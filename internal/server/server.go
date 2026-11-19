package server

import (
	"context"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/albert-tarly/dungerra-api/internal/auth"
	"github.com/albert-tarly/dungerra-api/internal/config"
	"github.com/albert-tarly/dungerra-api/internal/db"
	"github.com/albert-tarly/dungerra-api/internal/game"
	"github.com/albert-tarly/dungerra-api/internal/protocol"
	"github.com/albert-tarly/dungerra-api/internal/types"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins in development
	},
}

// Session represents a game session with its engine
type Session struct {
	ID                string
	Name              string
	Engine            *game.Engine
	PlayerCount       int
	mu                sync.Mutex
	saveMu            sync.Mutex // Serializes DB saves so a later snapshot is never overwritten by an earlier one
	lastSaveTime      time.Time
	deadPlayerTracked map[string]bool // Track which player deaths have been recorded
}

// GameServer manages the game and all clients
type GameServer struct {
	clients    map[string]*WebsocketClient
	sessions   map[string]*Session // sessionID -> Session
	register   chan *WebsocketClient
	unregister chan *WebsocketClient
	broadcast  chan []byte
	shutdown   chan struct{}
	mu         sync.RWMutex
	running    bool

	// Sessions whose last player left: saved and cleared in the background so
	// the game loop does not wait on the DB. A player who rejoins meanwhile
	// gets the in-memory world back. Guarded by mu.
	evicting map[string]*eviction
	// Completed eviction saves per session ID, to detect a DB copy read
	// before the latest save. Guarded by mu.
	saveSeq   map[string]uint64
	evictions sync.WaitGroup
}

// NewGameServer creates a new game server
func NewGameServer() *GameServer {
	return &GameServer{
		clients:    make(map[string]*WebsocketClient),
		sessions:   make(map[string]*Session),
		evicting:   make(map[string]*eviction),
		saveSeq:    make(map[string]uint64),
		register:   make(chan *WebsocketClient),
		unregister: make(chan *WebsocketClient),
		broadcast:  make(chan []byte, 256),
		shutdown:   make(chan struct{}),
		running:    false,
	}
}

// Run starts the game server loop
func (gs *GameServer) Run() {
	gs.running = true
	ticker := time.NewTicker(config.GameLoopInterval)
	defer ticker.Stop()

	for {
		select {
		case <-gs.shutdown:
			log.Println("Game server loop shutting down...")
			return

		case client := <-gs.register:
			gs.registerClient(client)

		case client := <-gs.unregister:
			gs.unregisterClient(client)

		case message := <-gs.broadcast:
			gs.broadcastMessage(message)

		case <-ticker.C:
			// Update all active sessions
			gs.mu.RLock()
			for _, session := range gs.sessions {
				session.Engine.Update()

				// Check if session needs saving (with mutex protection)
				session.mu.Lock()
				needsSave := (session.lastSaveTime.IsZero() || time.Since(session.lastSaveTime) > config.SessionSaveInterval) && session.PlayerCount > 0
				if needsSave {
					// Update lastSaveTime immediately to prevent duplicate saves
					session.lastSaveTime = time.Now()
				}
				session.mu.Unlock()

				if needsSave {
					// Save asynchronously to avoid blocking the game loop
					go gs.saveSessionToDatabase(session)
				}

				// Check for player deaths and update leaderboard
				for _, player := range session.Engine.GetAllPlayers() {
					session.mu.Lock()
					isTracked := session.deadPlayerTracked[player.ID]
					session.mu.Unlock()

					if !player.IsAlive && !isTracked {
						log.Printf("Player %s (ID: %s) died! Score: %d, Kills: %d", player.Username, player.ID, player.Score, player.Kills)

						// Mark this death as tracked to avoid duplicate entries
						session.mu.Lock()
						session.deadPlayerTracked[player.ID] = true
						session.mu.Unlock()

						// Update player score in leaderboard
						go func(p *types.Player, sessID, sessName string) {
							ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
							defer cancel()

							userID, err := primitive.ObjectIDFromHex(p.ID)
							if err != nil {
								log.Printf("Updating leaderboard: invalid player ID %s: %v", p.ID, err)
								return
							}

							leaderboardRepo := db.NewLeaderboardRepository()
							entry := &db.LeaderboardEntry{
								UserID:      userID,
								Username:    p.Username,
								SessionID:   sessID,
								SessionName: sessName,
								Score:       p.Score,
								Kills:       p.Kills,
							}
							if err := leaderboardRepo.UpsertEntry(ctx, entry); err != nil {
								log.Printf("Failed to update leaderboard entry for player %s: %v", p.Username, err)
							} else {
								log.Printf("Leaderboard updated for player %s: score=%d, kills=%d", p.Username, p.Score, p.Kills)
							}
						}(player, session.ID, session.Name)
					} else if player.IsAlive {
						// Reset tracking when player respawns
						session.mu.Lock()
						delete(session.deadPlayerTracked, player.ID)
						session.mu.Unlock()
					}
				}
			}
			gs.mu.RUnlock()

			// Broadcast game state for each session
			gs.broadcastAllSessionStates()
		}
	}
}

// Shutdown gracefully shuts down the server
func (gs *GameServer) Shutdown() {
	log.Println("Starting graceful shutdown...")

	// Signal the Run loop to stop
	close(gs.shutdown)

	// Give it a moment to process
	time.Sleep(100 * time.Millisecond)

	// Let background saves of sessions whose last player left finish
	gs.evictions.Wait()

	gs.mu.Lock()
	defer gs.mu.Unlock()

	// Close all client connections gracefully
	log.Printf("Closing %d client connections...", len(gs.clients))
	for id, client := range gs.clients {
		// Send close message to client
		client.Conn.WriteControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, "Server shutting down"),
			time.Now().Add(time.Second))
		client.Conn.Close()
		delete(gs.clients, id)
	}

	// Save all active sessions to database
	log.Printf("Saving %d active sessions to database...", len(gs.sessions))
	ctx := context.Background()
	sessionRepo := db.NewGameSessionRepository()

	for sessionID, session := range gs.sessions {
		if sessionObjID, err := primitive.ObjectIDFromHex(sessionID); err == nil {
			if dbSession, err := sessionRepo.FindByID(ctx, sessionObjID); err == nil {
				session.Engine.SaveToSession(dbSession)
				sessionRepo.Update(ctx, dbSession)
				log.Printf("Saved session %s", sessionID)
			}
		}
	}

	log.Println("Graceful shutdown complete")
}

func (gs *GameServer) registerClient(client *WebsocketClient) {
	gs.mu.Lock()

	gs.clients[client.ID] = client

	// Get or create session
	session, exists := gs.sessions[client.SessionID]
	if !exists {
		if ev, isEvicting := gs.evicting[client.SessionID]; isEvicting {
			// The last player left moments ago and the session is still being
			// saved: the in-memory world is newer than the DB, so keep it
			delete(gs.evicting, client.SessionID)
			session = ev.session
			gs.sessions[client.SessionID] = session
			log.Printf("Reviving session %s that was being unloaded", client.SessionID)
		} else {
			session = &Session{
				ID:                client.SessionID,
				Name:              client.SessionName,
				Engine:            game.NewEngine(client.SessionID),
				PlayerCount:       0,
				deadPlayerTracked: make(map[string]bool),
			}
			gs.sessions[client.SessionID] = session

			// The DB copy was read when the client connected, off the game loop.
			// Only if an unload save finished since then is it stale and read again.
			dbSession := client.dbSession
			if dbSession != nil && client.dbSessionSaveSeq != gs.saveSeq[client.SessionID] {
				dbSession = nil
				if sessionID, err := primitive.ObjectIDFromHex(client.SessionID); err == nil {
					if fresh, err := db.NewGameSessionRepository().FindByID(context.Background(), sessionID); err == nil {
						dbSession = fresh
					}
				}
			}

			if dbSession != nil {
				log.Printf("Loading existing session %s from database", client.SessionID)
				session.Engine.LoadFromSession(dbSession)
				session.lastSaveTime = time.Now()
			} else {
				log.Printf("Creating new session %s", client.SessionID)
			}
		}
	}

	session.mu.Lock()
	session.PlayerCount++
	playerCount := session.PlayerCount
	session.mu.Unlock()

	// Unlock before calling methods that need to acquire locks
	gs.mu.Unlock()

	// Add player to game engine
	player := session.Engine.ConnectPlayer(client.UserID.Hex(), client.Username)

	// The user's current session is recorded in HandleWebSocket, off the game loop

	gs.broadcastPlayerJoinedMessage(client.SessionID, player)

	log.Printf("Player %s (%s) joined session %s (players: %d)",
		client.Username, client.UserID.Hex(), client.SessionID, playerCount)
}

func (gs *GameServer) saveSessionToDatabase(session *Session) {
	// Periodic and unload saves run in the background and may overlap; the
	// snapshot is taken inside the lock, so saves land in snapshot order
	session.saveMu.Lock()
	defer session.saveMu.Unlock()

	ctx := context.Background()
	sessionRepo := db.NewGameSessionRepository()
	if sessionObjectID, err := primitive.ObjectIDFromHex(session.ID); err == nil {
		// Load or create database session
		dbSession, err := sessionRepo.FindByID(ctx, sessionObjectID)
		if err != nil {
			// Create new session
			dbSession = &db.GameSession{
				ID:         sessionObjectID,
				Name:       "Session " + session.ID[:8],
				MaxPlayers: 10,
				IsActive:   true,
			}
			sessionRepo.Create(ctx, dbSession)
		}

		// Save engine state to session
		session.Engine.SaveToSession(dbSession)
		sessionRepo.Update(ctx, dbSession)

		log.Printf("Session %s saved to database", session.ID)
	}
}

func (gs *GameServer) unregisterClient(client *WebsocketClient) {
	gs.mu.Lock()
	_, exists := gs.clients[client.ID]
	if exists {
		delete(gs.clients, client.ID)
	}

	session, sessionExists := gs.sessions[client.SessionID]
	gs.mu.Unlock()

	if !exists {
		return
	}

	if !sessionExists {
		return
	}

	// Remove player from game engine
	session.Engine.DisconnectPlayer(client.UserID.Hex())

	// Decrement player count
	session.mu.Lock()
	session.PlayerCount--
	playerCount := session.PlayerCount
	session.mu.Unlock()

	// Clear user's current session in database, off the game loop
	go func(userID primitive.ObjectID) {
		ctx := context.Background()
		userRepo := db.NewUserRepository()
		if user, err := userRepo.FindByID(ctx, userID); err == nil {
			user.CurrentSession = ""
			userRepo.Update(ctx, user)
		}
	}(client.UserID)

	// If this was the last player, save session to database and clear from memory
	if playerCount == 0 {
		log.Printf("Last player left session %s, saving to database", client.SessionID)

		// Unload in the background; registerClient revives it if a player
		// rejoins before the save finishes
		ev := &eviction{session: session}
		gs.mu.Lock()
		delete(gs.sessions, client.SessionID)
		gs.evicting[client.SessionID] = ev
		gs.evictions.Add(1)
		gs.mu.Unlock()

		go gs.evictSession(ev)
	} else {
		gs.broadcastPlayerLeftMessage(client.SessionID, client.UserID.Hex())
	}

	log.Printf("Player %s (%s) left session %s (remaining: %d)",
		client.Username, client.UserID.Hex(), client.SessionID, playerCount)
}

// eviction is one unload of a session. Each unload gets its own entry, so a
// slow save from an earlier unload of a revived session cannot free a later one.
type eviction struct {
	session *Session
}

// evictSession saves a session whose last player left and then frees it,
// unless a player rejoined in the meantime.
func (gs *GameServer) evictSession(ev *eviction) {
	defer gs.evictions.Done()

	session := ev.session
	gs.saveSessionToDatabase(session)

	gs.mu.Lock()
	defer gs.mu.Unlock()

	gs.saveSeq[session.ID]++
	if gs.evicting[session.ID] != ev {
		return // Revived by a rejoining player, or unloaded again since
	}
	delete(gs.evicting, session.ID)
	session.Engine.Clear()
}

func (gs *GameServer) broadcastMessage(message []byte) {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	for _, client := range gs.clients {
		select {
		case client.Send <- message:
		default:
			// Client buffer full, skip
		}
	}
}

func (gs *GameServer) broadcastPlayerJoinedMessage(sessionID string, player *types.Player) {
	msg := &protocol.GameMessage{
		Type: protocol.MessageType_PLAYER_JOIN,
		Payload: &protocol.GameMessage_PlayerJoin{
			PlayerJoin: &protocol.PlayerJoinMessage{
				Player: protocol.ToProtoPlayer(player),
			},
		},
	}

	gs.broadcastToSession(sessionID, msg, player.ID)
}

func (gs *GameServer) broadcastPlayerLeftMessage(sessionID string, playerID string) {
	msg := &protocol.GameMessage{
		Type: protocol.MessageType_PLAYER_LEAVE,
		Payload: &protocol.GameMessage_PlayerLeave{
			PlayerLeave: &protocol.PlayerLeaveMessage{
				PlayerId: playerID,
			},
		},
	}

	gs.broadcastToSession(sessionID, msg, playerID)
}

func (gs *GameServer) broadcastToSession(sessionID string, msg *protocol.GameMessage, excludeClientId string) {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	for _, client := range gs.clients {
		if client.SessionID == sessionID && client.UserID.Hex() != excludeClientId {
			if client.UseBinary {
				client.SendBinary(msg)
			} else {
				client.SendJSON(msg)
			}
		}
	}
}

func (gs *GameServer) broadcastAllSessionStates() {
	gs.mu.RLock()
	sessions := make(map[string]*Session)
	for id, session := range gs.sessions {
		sessions[id] = session
	}
	gs.mu.RUnlock()

	for sessionID, session := range sessions {
		// Send individualized delta to each player in the session
		gs.mu.RLock()
		for _, client := range gs.clients {
			if client.SessionID == sessionID {
				// Get player-specific delta (filtered to surrounding chunks)
				playerID := client.UserID.Hex()
				delta, view := session.Engine.GetGameStateDeltaForPlayer(playerID)

				// Only send if there are changes. If the client's buffer is full the
				// delta is dropped and not committed, so the next one includes it.
				if protocol.IsGameStateDeltaEmpty(delta) || client.SendGameStateDelta(delta) {
					session.Engine.CommitPlayerView(playerID, view)
				}
			}
		}
		gs.mu.RUnlock()
	}
}

// HandleWebSocket handles WebSocket connections
func (gs *GameServer) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Extract and validate JWT token from query parameters
	token := r.URL.Query().Get("token")
	if token == "" {
		// Check Authorization header as fallback
		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			token = strings.TrimPrefix(authHeader, "Bearer ")
		}
	}

	if token == "" {
		http.Error(w, "Unauthorized: missing token", http.StatusUnauthorized)
		return
	}

	// Validate JWT token
	userID, err := auth.ValidateToken(token)
	if err != nil {
		log.Printf("Token validation error: %v", err)
		http.Error(w, "Unauthorized: invalid token", http.StatusUnauthorized)
		return
	}

	// Fetch user from database
	ctx := context.Background()
	userRepo := db.NewUserRepository()
	user, err := userRepo.FindByID(ctx, userID)
	if err != nil {
		log.Printf("User lookup error: %v", err)
		http.Error(w, "Unauthorized: user not found", http.StatusUnauthorized)
		return
	}

	sessionID := r.URL.Query().Get("sessionId")
	sessionRepo := db.NewGameSessionRepository()
	sessionObjID, err := primitive.ObjectIDFromHex(sessionID)
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}

	// Read before the session itself so a save that lands in between is
	// detected in registerClient
	gs.mu.RLock()
	saveSeq := gs.saveSeq[sessionID]
	gs.mu.RUnlock()

	session, err := sessionRepo.FindByID(ctx, sessionObjID)
	if err != nil {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	// Upgrade to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	// Check if client wants binary protocol (via query parameter)
	useBinary := r.URL.Query().Get("protocol") == "binary"

	client := &WebsocketClient{
		ID:          uuid.New().String(),
		UserID:      user.ID,
		Username:    user.DisplayName(),
		SessionID:   sessionID,
		SessionName: session.Name,
		Conn:        conn,
		Send:        make(chan []byte, 256),
		Server:      gs,
		UseBinary:   useBinary,

		dbSession:        session,
		dbSessionSaveSeq: saveSeq,
	}

	// Record the user's current session here rather than on the game loop
	user.CurrentSession = sessionID
	if err := userRepo.Update(ctx, user); err != nil {
		log.Printf("Failed to record current session for user %s: %v", user.ID.Hex(), err)
	}

	log.Printf("New client connected (ID: %s, User: %s, Session: %s, Binary: %v)",
		client.UserID.Hex(), client.Username, client.SessionID, useBinary)

	// Start client goroutines
	go client.writePump()
	go client.readPump()

	// Register client
	gs.register <- client
}
