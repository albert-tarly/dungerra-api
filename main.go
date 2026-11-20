package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/albert-tarly/dungerra-api/internal/auth"
	"github.com/albert-tarly/dungerra-api/internal/config"
	"github.com/albert-tarly/dungerra-api/internal/db"
	"github.com/albert-tarly/dungerra-api/internal/handlers"
	"github.com/albert-tarly/dungerra-api/internal/server"
	"github.com/albert-tarly/dungerra-api/internal/webapp"
)

// The listen flags override HOST, PORT, USE_TLS, TLS_CERT and TLS_KEY. Their
// defaults are applied after .env is loaded (see resolveListenSettings), so
// values from .env are honoured too.
var (
	host     = flag.String("host", "", `Host to listen on (env HOST, default "localhost")`)
	port     = flag.String("port", "", `Port to listen on, 0 for any free port (env PORT, default "8080")`)
	certFile = flag.String("cert", "", "TLS certificate file, required for HTTPS (env TLS_CERT)")
	keyFile  = flag.String("key", "", "TLS key file, required for HTTPS (env TLS_KEY)")
	useTLS   = flag.Bool("tls", false, "Enable TLS/HTTPS (env USE_TLS=true)")

	// Desktop mode: see docs/decisions/desktop-version.md (sidecar contract)
	readyLine            = flag.Bool("ready-line", false, "Print a DUNGERRA_READY line with the bound address to stdout once listening")
	shutdownOnStdinClose = flag.Bool("shutdown-on-stdin-close", false, "Shut down gracefully when stdin is closed (for a parent process)")
	noDotEnv             = flag.Bool("no-dotenv", false, "Do not read a .env file from the working directory")
)

// CORS middleware
func corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Parse frontend domain from config
		frontendDomain := config.AppConfig.FrontendURL
		if idx := strings.Index(frontendDomain, "://"); idx != -1 {
			if pathIdx := strings.Index(frontendDomain[idx+3:], "/"); pathIdx != -1 {
				frontendDomain = frontendDomain[:idx+3+pathIdx]
			}
		}
		w.Header().Set("Access-Control-Allow-Origin", frontendDomain)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Allow-Credentials", "true")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next(w, r)
	}
}

func main() {
	flag.Parse()

	// Load configuration
	cfg := config.LoadConfig(config.LoadOptions{SkipDotEnv: *noDotEnv})

	// Open the configured storage backend (MongoDB or embedded SQLite)
	if err := db.Open(db.Options{
		Driver:     cfg.DBDriver,
		MongoDBURL: cfg.MongoDBURL,
		SQLitePath: cfg.SQLitePath,
	}); err != nil {
		log.Fatalf("Failed to open %s database: %v", cfg.DBDriver, err)
	}
	defer db.Close()

	// Create game server
	gameServer := server.NewGameServer()

	// Start game loop in background
	go gameServer.Run()

	// Setup handlers
	userHandler := auth.NewUserHandler()
	sessionHandler := handlers.NewSessionHandler()
	leaderboardHandler := handlers.NewLeaderboardHandler()

	// Setup HTTP routes
	http.HandleFunc("/ws", gameServer.HandleWebSocket)

	// Auth endpoints. Provider routes are only registered when the provider is
	// enabled, so a disabled provider is unreachable (404) rather than half-working.
	http.HandleFunc("/api/v1/auth/providers", corsMiddleware(userHandler.HandleProviders))
	http.HandleFunc("/api/v1/auth/user", corsMiddleware(userHandler.HandleGetUser))
	if cfg.GoogleAuthEnabled {
		googleAuth := auth.NewGoogleAuthHandler()
		http.HandleFunc("/api/v1/auth/google/url", corsMiddleware(googleAuth.HandleGetAuthURL))
		http.HandleFunc("/api/v1/auth/google/callback", googleAuth.HandleCallback)
	}
	if cfg.LocalAuthEnabled {
		localAuth := auth.NewLocalAuthHandler()
		http.HandleFunc("/api/v1/auth/local/register", corsMiddleware(localAuth.HandleRegister))
		http.HandleFunc("/api/v1/auth/local/login", corsMiddleware(localAuth.HandleLogin))
	}
	log.Printf("Auth providers: google=%v local=%v", cfg.GoogleAuthEnabled, cfg.LocalAuthEnabled)

	// Session endpoints
	http.HandleFunc("/api/v1/sessions", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			sessionHandler.HandleCreateSession(w, r)
		case http.MethodGet:
			sessionHandler.HandleListSessions(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	}))
	http.HandleFunc("/api/v1/sessions/", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/join") {
			sessionHandler.HandleJoinSession(w, r)
		} else if r.Method == http.MethodDelete {
			sessionHandler.HandleDeleteSession(w, r)
		} else {
			http.Error(w, "Not found", http.StatusNotFound)
		}
	}))

	// Leaderboard endpoints
	http.HandleFunc("/api/v1/leaderboard/global", corsMiddleware(leaderboardHandler.HandleGetGlobalLeaderboard))

	// Version, probed by the desktop launcher before joining a LAN host
	http.HandleFunc("/api/v1/version", corsMiddleware(handlers.HandleVersion))

	// Web client, when this server also delivers it (desktop and LAN play).
	// Registered at "/", so every route above takes precedence.
	if cfg.StaticDir != "" {
		http.Handle("/", webapp.NewSPAHandler(cfg.StaticDir))
		log.Printf("Serving web client from %s", cfg.StaticDir)
	}

	// Health check
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Prepare address
	listen := resolveListenSettings(explicitFlags(), listenSettings{
		Host: *host, Port: *port, CertFile: *certFile, KeyFile: *keyFile, UseTLS: *useTLS,
	}, cfg)
	addr := net.JoinHostPort(listen.Host, listen.Port)

	// Create HTTP server with proper configuration
	httpServer := &http.Server{
		Addr:         addr,
		Handler:      nil, // Uses DefaultServeMux
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Bind before serving, so port 0 (any free port) can be reported
	withTLS := listen.UseTLS || listen.CertFile != ""
	if withTLS && (listen.CertFile == "" || listen.KeyFile == "") {
		log.Fatal("TLS enabled but certificate or key file not provided. Use -cert and -key flags or TLS_CERT and TLS_KEY environment variables.")
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("Cannot listen on %s: %v", addr, err)
	}
	boundAddr := listener.Addr().String()
	_, boundPort, _ := net.SplitHostPort(boundAddr)

	// Start HTTP/HTTPS server
	go func() {
		if withTLS {
			log.Printf("Starting game server with TLS on %s", boundAddr)
			if err := httpServer.ServeTLS(listener, listen.CertFile, listen.KeyFile); err != nil && err != http.ErrServerClosed {
				log.Fatal("ServeTLS error: ", err)
			}
		} else {
			log.Printf("Starting game server on %s", boundAddr)
			if err := httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
				log.Fatal("Serve error: ", err)
			}
		}
	}()

	log.Println("Server started successfully")
	if withTLS {
		log.Printf("WebSocket (JSON): wss://your-domain:%s/ws", boundPort)
		log.Printf("WebSocket (Binary): wss://your-domain:%s/ws?protocol=binary", boundPort)
	} else {
		log.Printf("WebSocket (JSON): ws://localhost:%s/ws", boundPort)
		log.Printf("WebSocket (Binary): ws://localhost:%s/ws?protocol=binary", boundPort)
	}
	if *readyLine {
		// The listener is bound, so connections queue even before Serve runs
		fmt.Fprint(os.Stdout, formatReadyLine(boundAddr))
	}

	// Wait for an interrupt signal or, for a parent process, stdin closing
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	var stdinClosed <-chan struct{} // nil blocks forever when not enabled
	if *shutdownOnStdinClose {
		stdinClosed = closedOnEOF(os.Stdin)
	}
	select {
	case <-sigChan:
		log.Println("Received shutdown signal, shutting down gracefully...")
	case <-stdinClosed:
		log.Println("Stdin closed by the parent process, shutting down gracefully...")
	}

	// Shutdown game server first (save sessions, close websockets)
	gameServer.Shutdown()

	// Shutdown HTTP server with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	} else {
		log.Println("HTTP server shut down successfully")
	}

	log.Println("Server stopped")
}
