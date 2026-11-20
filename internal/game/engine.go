package game

import (
	"log"
	"math"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/albert-tarly/dungerra-api/internal/config"
	"github.com/albert-tarly/dungerra-api/internal/protocol"
	"github.com/albert-tarly/dungerra-api/internal/types"
	"github.com/albert-tarly/dungerra-api/internal/utils"
	"github.com/google/uuid"
)

// makeChunkKey builds the "x,y" key used by the per-chunk maps. It runs in the
// hottest loops of the tick, so it avoids fmt.Sprintf.
func makeChunkKey(chunkX, chunkY int) string {
	return strconv.Itoa(chunkX) + "," + strconv.Itoa(chunkY)
}

// Engine handles the game logic for a specific session
type EngineGameState struct {
	players        map[string]*types.Player
	bullets        map[string]*types.Bullet
	wallsByChunk   map[string]map[string]*types.Wall
	enemiesByChunk map[string]map[string]*types.Enemy
	bonuses        map[string]*types.Bonus
	shopsByChunk   map[string]map[string]*types.Shop
	rays           map[string]*types.Ray

	// markers is only used in per-player views: the other players' positions
	// the client holds for its markers (every detectable player, seen or not).
	markers map[string]types.Vector2
}

type UpdateTimeStats struct {
	enemies time.Duration
	bullets time.Duration
	players time.Duration
	bonuses time.Duration
}

func (s *UpdateTimeStats) Total() time.Duration {
	return s.enemies + s.bullets + s.players + s.bonuses
}

type DeltaCalcStats struct {
	delta          time.Duration
	updatePrevious time.Duration
}

func (s *DeltaCalcStats) Total() time.Duration {
	return s.delta + s.updatePrevious
}

type EngineStats struct {
	TotalUpdateTime                UpdateTimeStats
	TotalUpdateTimeSinceLastReport UpdateTimeStats
	UpdateCount                    int64
	UpdateCountSinceLastReport     int64

	TotalDeltaCalcTime                DeltaCalcStats
	TotalDeltaCalcTimeSinceLastReport DeltaCalcStats
	DeltaCalcCount                    int64
	DeltaCalcCountSinceLastReport     int64

	LastReportedAt time.Time
	Frequency      time.Duration
}
type Engine struct {
	mu           sync.RWMutex
	sessionID    string // Session identifier
	state        *EngineGameState
	chunkHash    map[string]bool // Track generated chunks
	respawnQueue map[string]bool // Players to respawn

	// Previous state for delta computation
	prevState               map[string]*EngineGameState
	lastUpdate              time.Time
	playerInputState        map[string]*types.InputPayload
	itemsToUseByPlayer      map[string][]types.InventoryItemID
	itemsToPurchaseByPlayer map[string][]types.InventoryItemID

	stats     *EngineStats
	debugMode bool

	// difficulty is fixed for the session's lifetime: it is set from the DB
	// session by LoadFromSession before any player connects.
	difficulty config.Difficulty
}

// NewEngine creates a new game engine for a session
func NewEngine(sessionID string) *Engine {
	return &Engine{
		sessionID: sessionID,
		state: &EngineGameState{
			players:        make(map[string]*types.Player),
			bullets:        make(map[string]*types.Bullet),
			wallsByChunk:   make(map[string]map[string]*types.Wall),
			enemiesByChunk: make(map[string]map[string]*types.Enemy),
			bonuses:        make(map[string]*types.Bonus),
			shopsByChunk:   make(map[string]map[string]*types.Shop),
			rays:           make(map[string]*types.Ray),
		},
		playerInputState:        make(map[string]*types.InputPayload),
		itemsToUseByPlayer:      make(map[string][]types.InventoryItemID),
		itemsToPurchaseByPlayer: make(map[string][]types.InventoryItemID),
		chunkHash:               make(map[string]bool),
		respawnQueue:            make(map[string]bool),
		prevState:               make(map[string]*EngineGameState),
		lastUpdate:              time.Now(),
		stats: &EngineStats{
			Frequency: time.Second * 1,
		},
		debugMode:  config.AppConfig.EngineDebugMode,
		difficulty: config.HardDifficulty(),
	}
}

// ConnectPlayer adds a new player to the game
func (e *Engine) ConnectPlayer(id, username string) *types.Player {
	e.mu.Lock()
	defer e.mu.Unlock()

	player, exists := e.state.players[id]
	if !exists {
		chunkKey := "0,0"
		chunksNumber := len(e.chunkHash)
		if chunksNumber > 0 {
			randomIndex := rand.Intn(len(e.chunkHash))
			i := 0
			for key := range e.chunkHash {
				if i == randomIndex {
					chunkKey = key
					break
				}
				i++
			}
		}

		chunkX, _ := strconv.Atoi(strings.Split(chunkKey, ",")[0])
		chunkY, _ := strconv.Atoi(strings.Split(chunkKey, ",")[1])
		chunkCenterX := float64(chunkX)*config.ChunkSize + config.ChunkSize/2
		chunkCenterY := float64(chunkY)*config.ChunkSize + config.ChunkSize/2

		spawnPoint := e.pickSpawnPoint(&types.Vector2{X: chunkCenterX, Y: chunkCenterY})

		player = &types.Player{
			ScreenObject: types.ScreenObject{
				ID:       id,
				Position: spawnPoint,
			},

			Username: username,
			Lives:    float32(e.difficulty.PlayerLives),
			MaxLives: float32(e.difficulty.PlayerLives),
			BulletsLeftByWeaponType: map[string]int32{
				types.WeaponTypeBlaster: config.BlasterMaxBullets,
			},
			InvulnerableTimer: config.PlayerSpawnInvulnerabilityTime,
			IsAlive:           true,
			IsConnected:       true,
			Inventory: []types.InventoryItem{
				{Type: types.InventoryItemBlaster, Quantity: 1},
			},
			SelectedGunType: types.WeaponTypeBlaster,
		}

		e.state.players[id] = player
	} else {
		if !player.IsAlive {
			e.addPlayerToRespawnQueue(id)
		}

		player.IsConnected = true
	}

	e.prevState[id] = &EngineGameState{}
	e.itemsToUseByPlayer[id] = []types.InventoryItemID{}
	e.itemsToPurchaseByPlayer[id] = []types.InventoryItemID{}
	// Generate initial walls and enemies around player
	e.generateInitialWorld(player.Position)

	return player
}

// generateInitialWorld creates walls and enemies in chunks around the starting position
func (e *Engine) generateInitialWorld(center *types.Vector2) {
	// Generate 3x3 grid of chunks around spawn
	chunkX, chunkY := utils.ChunkXYFromPosition(center.X, center.Y)

	for neighborChunkX := chunkX - 1; neighborChunkX <= chunkX+1; neighborChunkX++ {
		for neighborChunkY := chunkY - 1; neighborChunkY <= chunkY+1; neighborChunkY++ {
			e.generateChunk(neighborChunkX, neighborChunkY, center)
		}
	}
}

// generateChunk generates walls and enemies for a specific chunk
func (e *Engine) generateChunk(chunkX, chunkY int, playerPos *types.Vector2) {
	now := time.Now()
	defer func() {
		if e.debugMode {
			log.Printf("Generated chunk (%d,%d) in %v", chunkX, chunkY, time.Since(now))
		}
	}()

	// Check if chunk already exists
	chunkKey := makeChunkKey(chunkX, chunkY)
	if e.chunkHash[chunkKey] {
		return // Chunk already generated
	}
	e.chunkHash[chunkKey] = true
	e.state.wallsByChunk[chunkKey] = make(map[string]*types.Wall)
	e.state.enemiesByChunk[chunkKey] = make(map[string]*types.Enemy)
	e.state.shopsByChunk[chunkKey] = make(map[string]*types.Shop)

	chunkStartX := float64(chunkX) * config.ChunkSize
	chunkStartY := float64(chunkY) * config.ChunkSize

	// Randomly generate walls
	kiloPixelsPerChunk := math.Pow(config.ChunkSize/1000.0, 2)
	minNumWalls := config.MinWallsPerKiloPixel * kiloPixelsPerChunk
	maxNumWalls := config.MaxWallsPerKiloPixel * kiloPixelsPerChunk
	numWalls := rand.Intn(int(maxNumWalls-minNumWalls+1)) + int(minNumWalls)

	chunkCenter := &types.Vector2{
		X: chunkStartX + config.ChunkSize/2,
		Y: chunkStartY + config.ChunkSize/2,
	}
	shop := types.GenerateShop(chunkCenter)

	e.state.shopsByChunk[chunkKey][shop.ID] = shop

	// Create enemy towers. Hard has exactly one per chunk; other difficulties
	// scale that, with the fractional part rolled as a chance of one more.
	towerRadius := config.EnemyTowerSize / 2
	safeWallPadding := config.EnemySoldierSize
	towerPositions := make([]*types.Vector2, 0, 2)
	for range scaledCount(1, e.difficulty.TowersPct) {
		var towerPosition *types.Vector2
		for attempt := 0; attempt < maxTowerPlacementAttempts && towerPosition == nil; attempt++ {
			candidate := &types.Vector2{
				X: chunkStartX + towerRadius + rand.Float64()*(config.ChunkSize-towerRadius*2),
				Y: chunkStartY + towerRadius + rand.Float64()*(config.ChunkSize-towerRadius*2),
			}
			towerPosition = candidate
			for _, other := range towerPositions {
				if math.Hypot(candidate.X-other.X, candidate.Y-other.Y) < 2*(towerRadius+safeWallPadding) {
					towerPosition = nil
					break
				}
			}
		}
		if towerPosition == nil {
			continue // Chunk too crowded with towers; skip this one
		}
		towerPositions = append(towerPositions, towerPosition)

		towerID := uuid.New().String()
		e.state.enemiesByChunk[chunkKey][towerID] = &types.Enemy{
			ScreenObject: types.ScreenObject{
				ID:       towerID,
				Position: towerPosition,
			},
			Lives:      float32(config.EnemyTowerLives),
			Type:       types.EnemyTypeTower,
			ShootDelay: e.enemyShootDelay(config.EnemyTowerShootDelay),
			IsAlive:    true,
		}
	}

	for numWalls > 0 {
		// Random orientation
		orientation := "vertical"
		if rand.Float64() < 0.5 {
			orientation = "horizontal"
		}

		var x, y, width, height float64
		if orientation == "vertical" {
			x = chunkStartX + rand.Float64()*(config.ChunkSize-200) + 100
			y = chunkStartY + rand.Float64()*(config.ChunkSize-300) + 100
			width = config.WallWidth
			height = rand.Float64()*101 + 200 // 200-300
		} else {
			x = chunkStartX + rand.Float64()*(config.ChunkSize-300) + 100
			y = chunkStartY + rand.Float64()*(config.ChunkSize-200) + 100
			width = rand.Float64()*101 + 200 // 200-300
			height = config.WallWidth
		}

		// Don't spawn walls too close to player
		safePadding := config.TorchRadius + 40
		if math.Abs(x-playerPos.X) < safePadding && math.Abs(y-playerPos.Y) < safePadding {
			continue
		}

		wallID := uuid.New().String()
		wall := &types.Wall{
			ScreenObject: types.ScreenObject{
				ID:       wallID,
				Position: &types.Vector2{X: x, Y: y},
			},
			Width:       width,
			Height:      height,
			Orientation: orientation,
		}
		wallTopLeft := wall.GetTopLeft()

		overlaps := false
		for _, towerPosition := range towerPositions {
			if utils.CheckRectCollision(
				towerPosition.X-towerRadius-safeWallPadding,
				towerPosition.Y-towerRadius-safeWallPadding,
				towerRadius*2+2*safeWallPadding,
				towerRadius*2+2*safeWallPadding,
				wallTopLeft.X, wallTopLeft.Y,
				width, height,
			) {
				overlaps = true
				break
			}
		}
		if overlaps {
			continue
		}

		// Check overlap with existing walls
		for _, wall := range e.state.wallsByChunk[chunkKey] {
			checkedTopLeft := wall.GetTopLeft()

			if utils.CheckRectCollision(
				checkedTopLeft.X-safeWallPadding,
				checkedTopLeft.Y-safeWallPadding,
				wall.Width+2*safeWallPadding,
				wall.Height+2*safeWallPadding,
				wallTopLeft.X, wallTopLeft.Y, width, height,
			) {
				overlaps = true
				break
			}
		}

		if overlaps {
			continue
		}

		numWalls--
		e.state.wallsByChunk[chunkKey][wallID] = wall

		// Create enemies for this wall. A second one, when the difficulty asks
		// for more than one per wall, guards the opposite side.
		side := 1.0
		if rand.Float64() < 0.5 {
			side = -1.0
		}
		for _, enemyType := range e.rollWallEnemyTypes() {
			enemy := e.createEnemyForWall(wall, enemyType, side)
			e.state.enemiesByChunk[chunkKey][enemy.ID] = enemy
			side = -side
		}
	}

	e.generateWallChests(chunkKey, towerPositions)
}

// maxTowerPlacementAttempts bounds the search for a free spot for each extra
// tower in a chunk.
const maxTowerPlacementAttempts = 10

// scaledCount scales a Hard count by a difficulty percentage. The fractional
// part becomes the chance of one more, so the expected count is exact.
func scaledCount(hardCount float64, pct int) int {
	expected := hardCount * float64(pct) / 100
	count := int(expected)
	if rand.Float64() < expected-float64(count) {
		count++
	}
	return count
}

// rollWallEnemyTypes picks the enemies guarding one wall. On Hard this is the
// original roll: a 90% chance of an enemy, which is a sniper 5% of the time,
// else a lieutenant 15% of the time, else a soldier. Difficulty percentages
// scale each type's expected count per wall. While the total stays at or
// below one, a single categorical roll keeps Hard's distribution exact; above
// one, the extra share becomes the chance of a second enemy.
func (e *Engine) rollWallEnemyTypes() []string {
	hardSniper := config.EnemySpawnChancePerWall * config.EnemySniperChance
	hardLieutenant := config.EnemySpawnChancePerWall * (1 - config.EnemySniperChance) * config.EnemyLieutenantChance
	hardSoldier := config.EnemySpawnChancePerWall * (1 - config.EnemySniperChance) * (1 - config.EnemyLieutenantChance)

	sniper := hardSniper * float64(e.difficulty.SnipersPct) / 100
	lieutenant := hardLieutenant * float64(e.difficulty.LieutenantsPct) / 100
	soldier := hardSoldier * float64(e.difficulty.SoldiersPct) / 100
	total := sniper + lieutenant + soldier
	if total <= 0 {
		return nil
	}

	count := 1
	if total > 1 {
		count = scaledCount(total, 100)
	} else if rand.Float64() >= total {
		return nil
	}

	enemyTypes := make([]string, 0, count)
	for range count {
		r := rand.Float64() * total
		switch {
		case r < sniper:
			enemyTypes = append(enemyTypes, types.EnemyTypeSniper)
		case r < sniper+lieutenant:
			enemyTypes = append(enemyTypes, types.EnemyTypeLieutenant)
		default:
			enemyTypes = append(enemyTypes, types.EnemyTypeSoldier)
		}
	}
	return enemyTypes
}

// enemyShootDelay scales a Hard shoot delay by the difficulty's fire rate.
func (e *Engine) enemyShootDelay(hardDelay float64) float64 {
	return hardDelay * 100 / float64(e.difficulty.EnemyFireRatePct)
}

// generateWallChests places loot chests beside a share of a freshly generated
// chunk's walls. They have no DroppedBy, so they never expire and are saved
// with the world like any other bonus.
func (e *Engine) generateWallChests(chunkKey string, towerPositions []*types.Vector2) {
	if e.difficulty.WallChestPct <= 0 {
		return
	}
	chestRadius := config.ChestSize / 2
	towerRadius := config.EnemyTowerSize / 2
	walls := e.state.wallsByChunk[chunkKey]

	for _, wall := range walls {
		if rand.Float64()*100 >= float64(e.difficulty.WallChestPct) {
			continue
		}

		// A random spot along the wall, on a random side. Not the midpoint,
		// which is where guards stand.
		side := 1.0
		if rand.Float64() < 0.5 {
			side = -1.0
		}
		along := rand.Float64() - 0.5
		offset := config.WallWidth/2 + config.WallChestGap + chestRadius
		position := wall.GetCenter()
		if wall.Orientation == "vertical" {
			position.X += side * offset
			position.Y += along * (wall.Height - config.ChestSize)
		} else {
			position.X += along * (wall.Width - config.ChestSize)
			position.Y += side * offset
		}

		blocked := false
		for _, other := range walls {
			topLeft := other.GetTopLeft()
			if utils.CheckRectCollision(
				position.X-chestRadius, position.Y-chestRadius, config.ChestSize, config.ChestSize,
				topLeft.X, topLeft.Y, other.Width, other.Height,
			) {
				blocked = true
				break
			}
		}
		for _, towerPosition := range towerPositions {
			if math.Hypot(position.X-towerPosition.X, position.Y-towerPosition.Y) < towerRadius+chestRadius {
				blocked = true
				break
			}
		}
		if blocked {
			continue
		}

		bonus := &types.Bonus{
			ScreenObject: types.ScreenObject{
				ID:       uuid.New().String(),
				Position: position,
			},
			Type:      types.BonusTypeChest,
			Inventory: rollWallChestInventory(position),
		}
		e.state.bonuses[bonus.ID] = bonus
	}
}

// rollWallChestInventory always includes money, more of it the further the
// chest is from the world origin so exploring pays off, plus a few rolled
// supplies.
func rollWallChestInventory(position *types.Vector2) []types.InventoryItem {
	distance := math.Hypot(position.X, position.Y)
	moneyMultiplier := math.Min(1+distance/config.WallChestMoneyDistanceScale, config.WallChestMoneyMaxMultiplier)
	money := float64(config.WallChestMoneyMin+rand.Intn(config.WallChestMoneyMax-config.WallChestMoneyMin+1)) * moneyMultiplier

	inventory := []types.InventoryItem{{Type: types.InventoryItemMoney, Quantity: int32(math.Round(money))}}

	if rand.Float64() < config.WallChestAidKitProbability {
		inventory = append(inventory, types.InventoryItem{
			Type:     types.InventoryItemAidKit,
			Quantity: int32(config.WallChestAidKitMinQuantity + rand.Intn(config.WallChestAidKitMaxQuantity-config.WallChestAidKitMinQuantity+1)),
		})
	}
	if rand.Float64() < config.WallChestGogglesProbability {
		inventory = append(inventory, types.InventoryItem{Type: types.InventoryItemGoggles, Quantity: 1})
	}
	if rand.Float64() < config.WallChestAmmoProbability {
		ammoItems := []types.InventoryItemID{types.InventoryItemShotgunAmmo, types.InventoryItemRocket, types.InventoryItemRailgunAmmo}
		inventory = append(inventory, types.InventoryItem{
			Type:     ammoItems[rand.Intn(len(ammoItems))],
			Quantity: int32(config.WallChestAmmoMinQuantity + rand.Intn(config.WallChestAmmoMaxQuantity-config.WallChestAmmoMinQuantity+1)),
		})
	}
	if rand.Float64() < config.WallChestWeaponProbability {
		weaponItems := []types.InventoryItemID{types.InventoryItemShotgun, types.InventoryItemRocketLauncher, types.InventoryItemRailgun}
		inventory = append(inventory, types.InventoryItem{Type: weaponItems[rand.Intn(len(weaponItems))], Quantity: 1})
	}
	return inventory
}

func (e *Engine) pickSpawnPoint(playerPos *types.Vector2) *types.Vector2 {
	// Spawn position near center with some randomization
	chunkX, chunkY := utils.ChunkXYFromPosition(playerPos.X, playerPos.Y)

	// Move to the random neighboring chunk
	chunkIdToMove := rand.Intn(8)
	if chunkIdToMove < 3 {
		chunkY -= 1
	}
	if chunkIdToMove > 4 {
		chunkY += 1
	}

	if chunkIdToMove == 0 || chunkIdToMove == 3 || chunkIdToMove == 5 {
		chunkX -= 1
	}
	if chunkIdToMove == 2 || chunkIdToMove == 4 || chunkIdToMove == 7 {
		chunkX += 1
	}

	// Calculate spawn position in the chunk center
	spawnLeft := float64(chunkX)*config.ChunkSize + config.ChunkSize/2
	spawnTop := float64(chunkY)*config.ChunkSize + config.ChunkSize/2

	playerSize := config.PlayerRadius * 2

	// Check collision with walls, enemies, or players
	objectsToCheck := []*types.CollisionObject{}

	// Form collision boxes adding player radius as padding on top
	for _, walls := range e.state.wallsByChunk {
		for _, wall := range walls {
			wallTopLeft := wall.GetTopLeft()

			objectsToCheck = append(objectsToCheck, &types.CollisionObject{
				LeftTopPos: wallTopLeft,
				Width:      wall.Width,
				Height:     wall.Height,
			})
		}
	}

	for _, enemy := range e.state.enemiesByChunk {
		for _, enemy := range enemy {
			if !enemy.IsAlive {
				continue
			}

			objectsToCheck = append(objectsToCheck, &types.CollisionObject{
				LeftTopPos: types.Vector2{X: enemy.Position.X - enemy.Size()/2, Y: enemy.Position.Y - enemy.Size()/2},
				Width:      enemy.Size(),
				Height:     enemy.Size(),
			})
		}
	}

	for _, otherPlayer := range e.state.players {
		if !otherPlayer.IsConnected || !otherPlayer.IsAlive {
			continue
		}

		objectsToCheck = append(objectsToCheck, &types.CollisionObject{
			LeftTopPos: types.Vector2{X: otherPlayer.Position.X - config.PlayerRadius, Y: otherPlayer.Position.Y - config.PlayerRadius},
			Width:      config.PlayerRadius * 2,
			Height:     config.PlayerRadius * 2,
		})
	}

	hasCollision := true

	for hasCollision {
		hasCollision = false

		for _, object := range objectsToCheck {
			if utils.CheckRectCollision(
				spawnLeft-config.PlayerRadius,
				spawnTop-config.PlayerRadius,
				playerSize,
				playerSize,
				object.LeftTopPos.X,
				object.LeftTopPos.Y,
				object.Width,
				object.Height,
			) {
				hasCollision = true
				spawnLeft += playerSize
				spawnTop += playerSize
				break
			}
		}
	}

	return &types.Vector2{X: spawnLeft, Y: spawnTop}
}

// createEnemyForWall creates an enemy that patrols along a wall
// on the given side of it (1 or -1)
func (e *Engine) createEnemyForWall(wall *types.Wall, enemyType string, wallSide float64) *types.Enemy {
	enemyID := uuid.New().String()
	enemySize := config.EnemySoldierSize
	enemyLives := config.EnemySoldierLives
	switch enemyType {
	case types.EnemyTypeLieutenant:
		enemyLives = config.EnemyLieutenantLives
	case types.EnemyTypeSniper:
		enemyLives = config.EnemySniperLives
	}

	var x, y float64

	if wall.Orientation == "vertical" {
		x = wall.Position.X - wallSide*(wall.Width/2+enemySize/2)
		y = wall.Position.Y + wall.Height/2
	} else {
		x = wall.Position.X + wall.Width/2
		y = wall.Position.Y - wallSide*(wall.Height/2+enemySize/2)
	}

	rotation := 0.0
	if wall.Orientation == "vertical" {
		rotation = 90.0
	}

	return &types.Enemy{
		ScreenObject: types.ScreenObject{
			ID:       enemyID,
			Position: &types.Vector2{X: x, Y: y},
		},
		Rotation:   rotation,
		Lives:      float32(enemyLives),
		WallID:     wall.ID,
		Direction:  1.0,
		ShootDelay: 0,
		IsAlive:    true,
		DeadTimer:  0,
		Type:       enemyType,
	}
}

func (e *Engine) addPlayerToRespawnQueue(id string) {
	if _, exists := e.state.players[id]; exists {
		e.respawnQueue[id] = true
	}
}

func (e *Engine) RespawnPlayer(id string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.addPlayerToRespawnQueue(id)
}

// DisconnectPlayer removes a player from the game
func (e *Engine) DisconnectPlayer(id string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	player, exists := e.state.players[id]
	if exists {
		player.IsConnected = false
	}

	delete(e.prevState, id)
	delete(e.playerInputState, id)
	delete(e.respawnQueue, id)
	delete(e.itemsToUseByPlayer, id)
	delete(e.itemsToPurchaseByPlayer, id)
}

// UpdatePlayerInput updates player movement and rotation based on input
func (e *Engine) UpdatePlayerInput(playerID string, input types.InputPayload) {
	e.mu.Lock()
	defer e.mu.Unlock()

	prevInput, exists := e.playerInputState[playerID]
	if exists {
		for i := range prevInput.ItemKey {
			if !input.ItemKey[i] {
				e.itemsToUseByPlayer[playerID] = append(e.itemsToUseByPlayer[playerID], types.InventoryItemID(i))
			}
		}

		for i := range prevInput.PurchaseItemKey {
			if !input.PurchaseItemKey[i] {
				e.itemsToPurchaseByPlayer[playerID] = append(e.itemsToPurchaseByPlayer[playerID], types.InventoryItemID(i))
			}
		}

		// Keep turning toward the last aim if this message carries none
		if input.AimRotation == nil {
			input.AimRotation = prevInput.AimRotation
		}
	}

	e.playerInputState[playerID] = &input
}

// movementDirection returns the world-space unit vector the player wants to
// move in, relative to their facing (rotationDeg): forward/backward along the
// facing, left/right strafing perpendicular to it. Returns (0, 0) when idle.
// Diagonals are normalised so they are not faster than straight moves.
func movementDirection(input *types.InputPayload, rotationDeg float64) (float64, float64) {
	strafe, forward := 0.0, 0.0
	if input.Left {
		strafe--
	}
	if input.Right {
		strafe++
	}
	if input.Forward {
		forward++
	}
	if input.Backward {
		forward--
	}
	if strafe == 0 && forward == 0 {
		return 0, 0
	}
	if strafe != 0 && forward != 0 {
		strafe /= math.Sqrt2
		forward /= math.Sqrt2
	}

	rad := rotationDeg * math.Pi / 180
	sin, cos := math.Sin(rad), math.Cos(rad)
	// Facing vector is (-sin, cos); the player's right-hand side (screen Y points
	// down) is (-cos, -sin), e.g. facing up (180°) → right is +X.
	return -sin*forward - cos*strafe, cos*forward - sin*strafe
}

// buildPlayerView snapshots what the player's client will know once the delta
// computed in the same read-locked pass has been delivered. It only reads
// engine state, so it is safe under RLock.
func (e *Engine) buildPlayerView(playerID string) *EngineGameState {
	player, exists := e.state.players[playerID]
	if !exists {
		return nil
	}

	playerChunkX, playerChunkY := utils.ChunkXYFromPosition(player.Position.X, player.Position.Y)

	prevState := &EngineGameState{}

	playersAbleToSee := make(map[string]*types.Player)
	playersAbleToSee[playerID] = player

	// Same rule as GetGameStateDeltaForPlayer: a player without goggles also
	// sees what the detectable players they can see are seeing.
	if player.NightVisionTimer <= 0 {
		for id, p := range e.state.players {
			if p.IsConnected && id != playerID && p.IsPositionDetectable() && p.IsVisibleToPlayer(player) {
				playersAbleToSee[id] = p
			}
		}
	}

	// Save objects to previous state for delta computation. The view must hold
	// exactly the players the delta reported (added or updated): dead players
	// and goggled ones lit by a torch included. Leaving them out made every
	// delta re-add them and never send their updates, so a respawn or a move
	// never reached the client.
	prevState.players = make(map[string]*types.Player)
	for id, p := range e.state.players {
		if !p.IsConnected {
			continue
		}
		if id == playerID || isPlayerVisibleToAny(p, playersAbleToSee) {
			prevState.players[id] = p.Clone()
		}
	}

	prevState.markers = make(map[string]types.Vector2)
	for id, p := range e.state.players {
		if id != playerID && p.IsPositionDetectable() {
			prevState.markers[id] = *p.Position
		}
	}

	prevState.shopsByChunk = make(map[string]map[string]*types.Shop)
	prevState.wallsByChunk = make(map[string]map[string]*types.Wall)
	prevState.enemiesByChunk = make(map[string]map[string]*types.Enemy)
	for neighborChunkX := playerChunkX - 1; neighborChunkX <= playerChunkX+1; neighborChunkX++ {
		for neighborChunkY := playerChunkY - 1; neighborChunkY <= playerChunkY+1; neighborChunkY++ {
			chunkKey := makeChunkKey(neighborChunkX, neighborChunkY)
			if !e.chunkHash[chunkKey] {
				continue
			}
			prevState.wallsByChunk[chunkKey] = make(map[string]*types.Wall)
			prevState.enemiesByChunk[chunkKey] = make(map[string]*types.Enemy)
			prevState.shopsByChunk[chunkKey] = make(map[string]*types.Shop)

			for _, wall := range e.state.wallsByChunk[chunkKey] {
				// Walls are always visible to players so no need to check nearby players
				if wall.IsVisibleToPlayer(player) {
					// Walls never change after generation and the delta only checks
					// their presence, so the live object is shared instead of copied
					prevState.wallsByChunk[chunkKey][wall.ID] = wall
				}
			}

			for _, enemy := range e.state.enemiesByChunk[chunkKey] {
				for _, p := range playersAbleToSee {
					if enemy.IsVisibleToPlayer(p) {
						prevState.enemiesByChunk[chunkKey][enemy.ID] = enemy.Clone()
						break
					}
				}
			}

			for _, shop := range e.state.shopsByChunk[chunkKey] {
				for _, p := range playersAbleToSee {
					if shop.IsVisibleToPlayer(p) {
						prevState.shopsByChunk[chunkKey][shop.ID] = shop.Clone()
						break
					}
				}
			}
		}
	}

	prevState.bullets = make(map[string]*types.Bullet)
	for id, bullet := range e.state.bullets {
		for _, p := range playersAbleToSee {
			if bullet.IsVisibleToPlayer(p) {
				prevState.bullets[id] = bullet.Clone()
				break
			}
		}
	}

	prevState.bonuses = make(map[string]*types.Bonus)
	for id, bonus := range e.state.bonuses {
		for _, p := range playersAbleToSee {

			if bonus.IsVisibleToPlayer(p) {
				prevState.bonuses[id] = bonus.Clone()
				break
			}
		}
	}

	prevState.rays = make(map[string]*types.Ray)
	for _, ray := range e.state.rays {
		// Rays are always visible to players so no need to check nearby players
		if ray.IsVisibleToPlayer(player) {
			prevState.rays[ray.ID] = ray.Clone()
		}
	}

	return prevState
}

// PlayerView is the state a player's client will have after receiving a delta.
// The server commits it with CommitPlayerView only once the delta has been
// queued for sending, so a dropped delta is folded into the next one instead
// of being lost.
type PlayerView struct {
	state          *EngineGameState
	deltaTime      time.Duration
	updatePrevTime time.Duration
}

// CommitPlayerView makes view the base for the player's next delta.
func (e *Engine) CommitPlayerView(playerID string, view *PlayerView) {
	if view == nil || view.state == nil {
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	// The player disconnected after the delta was computed
	if _, exists := e.prevState[playerID]; !exists {
		return
	}
	e.prevState[playerID] = view.state

	if e.debugMode {
		e.stats.DeltaCalcCount++
		e.stats.DeltaCalcCountSinceLastReport++
		e.stats.TotalDeltaCalcTime.delta += view.deltaTime
		e.stats.TotalDeltaCalcTimeSinceLastReport.delta += view.deltaTime
		e.stats.TotalDeltaCalcTime.updatePrevious += view.updatePrevTime
		e.stats.TotalDeltaCalcTimeSinceLastReport.updatePrevious += view.updatePrevTime
	}
}

// Update runs one game tick
func (e *Engine) Update() {
	e.mu.Lock()
	defer e.mu.Unlock()

	now := time.Now()
	deltaTime := now.Sub(e.lastUpdate).Seconds()
	e.lastUpdate = now

	var updateDuration time.Duration

	playersChunks := make(map[string]bool)
	newBulletsIDs := make(map[string]bool)

	// Update players
	for _, player := range e.state.players {
		if !player.IsConnected {
			continue
		}

		if !player.IsAlive {
			if _, exists := e.respawnQueue[player.ID]; exists {
				// Respawn player
				spawnPoint := e.pickSpawnPoint(player.Position)
				player.Respawn(spawnPoint)
				delete(e.respawnQueue, player.ID)
			}

			continue
		}

		playerChunkX, playerChunkY := utils.ChunkXYFromPosition(player.Position.X, player.Position.Y)

		// Update timers
		if player.InvulnerableTimer > 0 {
			player.InvulnerableTimer = math.Max(0, player.InvulnerableTimer-deltaTime)
		}

		if player.NightVisionTimer > 0 {
			player.NightVisionTimer = math.Max(0, player.NightVisionTimer-deltaTime)
		}

		player.Recharge(deltaTime)

		itemsToUse := e.itemsToUseByPlayer[player.ID]
		for _, itemID := range itemsToUse {
			_, exists := types.WeaponTypeByInventoryItem[itemID]
			if exists {
				player.SelectGunType(itemID)
			}

			if itemID == types.InventoryItemAidKit {
				player.UseAidKit()
			}

			if itemID == types.InventoryItemGoggles {
				player.UseGoggles()
			}
		}
		e.itemsToUseByPlayer[player.ID] = []types.InventoryItemID{}

		var playersShop *types.Shop
		// Check if player is in shop
		chunkKey := makeChunkKey(playerChunkX, playerChunkY)
		for _, shop := range e.state.shopsByChunk[chunkKey] {
			if shop.IsPlayerInShop(player) {
				playersShop = shop
				break
			}
		}

		itemsToPurchase := e.itemsToPurchaseByPlayer[player.ID]
		for _, itemID := range itemsToPurchase {
			if playersShop != nil {
				playersShop.PurchaseInventoryItem(player, itemID)
			}
		}
		e.itemsToPurchaseByPlayer[player.ID] = []types.InventoryItemID{}

		input, inputExists := e.playerInputState[player.ID]
		if inputExists {

			// Turn toward the aim direction at a capped rate
			if input.AimRotation != nil {
				player.Rotation = utils.RotateTowards(player.Rotation, *input.AimRotation, config.PlayerRotationSpeed*deltaTime)
			}

			if input.Shoot {
				for id := range e.handlePlayerShooting(player) {
					newBulletsIDs[id] = true
				}
			}

			// Movement is relative to facing: forward/backward and strafe
			moveX, moveY := movementDirection(input, player.Rotation)
			if moveX != 0 || moveY != 0 {
				// Calculate movement
				intendedDx := moveX * config.PlayerSpeed * deltaTime
				intendedDy := moveY * config.PlayerSpeed * deltaTime

				dx := intendedDx
				dy := intendedDy
				dx0 := dx
				dy0 := dy

				objectsToCheck := []*types.CollisionObject{}

				// Form collision boxes adding player radius as padding on top

				for neighborChunkX := playerChunkX - 1; neighborChunkX <= playerChunkX+1; neighborChunkX++ {
					for neighborChunkY := playerChunkY - 1; neighborChunkY <= playerChunkY+1; neighborChunkY++ {
						neighborChunkKey := makeChunkKey(neighborChunkX, neighborChunkY)
						if !e.chunkHash[neighborChunkKey] {
							continue
						}

						for _, wall := range e.state.wallsByChunk[neighborChunkKey] {
							wallTopLeft := wall.GetTopLeft()

							objectsToCheck = append(objectsToCheck, &types.CollisionObject{
								LeftTopPos: types.Vector2{X: wallTopLeft.X - config.PlayerRadius, Y: wallTopLeft.Y - config.PlayerRadius},
								Width:      wall.Width + config.PlayerRadius*2,
								Height:     wall.Height + config.PlayerRadius*2,
							})
						}

						for _, enemy := range e.state.enemiesByChunk[neighborChunkKey] {
							if enemy.IsAlive {
								objectsToCheck = append(objectsToCheck, &types.CollisionObject{
									LeftTopPos: types.Vector2{X: enemy.Position.X - enemy.Size()/2 - config.PlayerRadius, Y: enemy.Position.Y - enemy.Size()/2 - config.PlayerRadius},
									Width:      enemy.Size() + config.PlayerRadius*2,
									Height:     enemy.Size() + config.PlayerRadius*2,
								})
							}
						}
					}
				}

				for _, otherPlayer := range e.state.players {
					if !otherPlayer.IsConnected {
						continue
					}

					if otherPlayer.ID != player.ID && otherPlayer.IsAlive {
						objectsToCheck = append(objectsToCheck, &types.CollisionObject{
							LeftTopPos: types.Vector2{X: otherPlayer.Position.X - config.PlayerRadius*2, Y: otherPlayer.Position.Y - config.PlayerRadius*2},
							Width:      config.PlayerRadius * 4,
							Height:     config.PlayerRadius * 4,
						})
					}
				}

				for _, obj := range objectsToCheck {
					if dx != 0 || dy != 0 {
						ix, iy := utils.CutLineSegmentBeforeRect(
							player.Position.X,
							player.Position.Y,
							player.Position.X+dx,
							player.Position.Y+dy,
							obj.LeftTopPos.X,
							obj.LeftTopPos.Y,
							obj.Width, obj.Height,
						)

						dx = ix - player.Position.X
						dy = iy - player.Position.Y
					}

					if dx0 != 0 {
						ix, _ := utils.CutLineSegmentBeforeRect(
							player.Position.X,
							player.Position.Y,
							player.Position.X+dx0,
							player.Position.Y,
							obj.LeftTopPos.X,
							obj.LeftTopPos.Y,
							obj.Width, obj.Height,
						)

						dx0 = ix - player.Position.X
					}

					if dy0 != 0 {
						_, iy := utils.CutLineSegmentBeforeRect(
							player.Position.X,
							player.Position.Y,
							player.Position.X,
							player.Position.Y+dy0,
							obj.LeftTopPos.X,
							obj.LeftTopPos.Y,
							obj.Width, obj.Height,
						)

						dy0 = iy - player.Position.Y
					}
				}

				// Apply movement with sliding collision
				if dx == 0 && dy == 0 {
					if dx0 != 0 {
						dx = dx0
					}
					if dy0 != 0 {
						dy = dy0
					}
				}

				player.Position.X += dx
				player.Position.Y += dy
			}
		}

		// Track chunks where players are located
		playerChunkX, playerChunkY = utils.ChunkXYFromPosition(player.Position.X, player.Position.Y)
		for neighborChunkX := playerChunkX - 1; neighborChunkX <= playerChunkX+1; neighborChunkX++ {
			for neighborChunkY := playerChunkY - 1; neighborChunkY <= playerChunkY+1; neighborChunkY++ {
				neighborChunkKey := makeChunkKey(neighborChunkX, neighborChunkY)
				if !e.chunkHash[neighborChunkKey] {
					e.generateChunk(neighborChunkX, neighborChunkY, player.Position)
				}
				playersChunks[neighborChunkKey] = true
			}
		}
	}

	if e.debugMode {
		updateDuration = time.Since(now)
		e.stats.TotalUpdateTime.players += updateDuration
		e.stats.TotalUpdateTimeSinceLastReport.players += updateDuration
		now = time.Now()
	}

	checkedEnemies := 0
	// Snipers that kept aiming this tick; any other laser ray is stale
	activeRayOwners := make(map[string]bool)

	// Update enemies
	for enemyChunkKey := range playersChunks {
		for _, enemy := range e.state.enemiesByChunk[enemyChunkKey] {
			enemyChunkX, enemyChunkY := utils.ChunkXYFromPosition(enemy.Position.X, enemy.Position.Y)

			checkedEnemies++

			if !enemy.IsAlive {
				// Delete enemy rays
				delete(e.state.rays, enemy.ID)

				// Handle dead enemy timer
				enemy.DeadTimer -= deltaTime
				if enemy.DeadTimer <= 0 {
					// Remove completely dead enemies
					delete(e.state.enemiesByChunk[enemyChunkKey], enemy.ID)
				}
				continue
			}

			// Find closest player to track
			var closestVisiblePlayer *types.Player
			hasPlayersInSight := false
			canSee := false
			minDist := math.MaxFloat64

			for _, player := range e.state.players {
				if !player.IsConnected || !player.IsAlive {
					continue
				}

				detectionPoint, detectionDistance := player.DetectionParams()
				if enemy.Type == types.EnemyTypeSniper {
					// Snipers see in the dark, but only up to their max range
					detectionDistance = config.EnemySniperRange
				}

				dist := enemy.DistanceToPoint(detectionPoint)
				if dist < config.SightRadius {
					hasPlayersInSight = true
				}
				if dist < detectionDistance+enemy.Size()/2 {
					// Add line-of-sight check with walls
					lineClear := true

					for neighborChunkX := enemyChunkX - 1; neighborChunkX <= enemyChunkX+1; neighborChunkX++ {
						for neighborChunkY := enemyChunkY - 1; neighborChunkY <= enemyChunkY+1; neighborChunkY++ {
							neighborChunkKey := makeChunkKey(neighborChunkX, neighborChunkY)
							if !e.chunkHash[neighborChunkKey] {
								continue
							}
							for _, wall := range e.state.wallsByChunk[neighborChunkKey] {
								distanceToWall := enemy.DistanceToPoint(wall.GetCenter())
								if distanceToWall > 2*wall.GetRadius()+detectionDistance {
									continue // Wall is beyond player
								}

								wallTopLeft := wall.GetTopLeft()
								if utils.CheckLineRectCollision(
									enemy.Position.X, enemy.Position.Y,
									detectionPoint.X, detectionPoint.Y,
									wallTopLeft.X, wallTopLeft.Y,
									wall.Width, wall.Height) {
									lineClear = false
									break
								}
							}
							for _, otherEnemy := range e.state.enemiesByChunk[neighborChunkKey] {
								if otherEnemy.ID == enemy.ID || !otherEnemy.IsAlive {
									continue
								}

								distanceToEnemy := enemy.DistanceToPoint(otherEnemy.Position)
								if distanceToEnemy > 2*otherEnemy.Size()+detectionDistance {
									continue // Enemy is beyond player
								}

								size := otherEnemy.Size()

								if utils.CheckLineRectCollision(
									enemy.Position.X, enemy.Position.Y,
									detectionPoint.X, detectionPoint.Y,
									otherEnemy.Position.X-size/2,
									otherEnemy.Position.Y-size/2,
									size, size) {
									lineClear = false
									break
								}
							}
						}
					}
					if lineClear {
						canSee = true
						if dist < minDist {
							minDist = dist
							closestVisiblePlayer = player
						}
					}
				}
			}

			if !canSee && enemy.Type == types.EnemyTypeSniper {
				enemy.LastAimedAtPlayerID = ""
				enemy.LastShotAtPlayerID = ""
				delete(e.state.rays, enemy.ID)
			}

			if !hasPlayersInSight {
				continue // No players nearby
			}

			// Update shoot delay
			if enemy.ShootDelay > 0 {
				enemy.ShootDelay -= deltaTime
			}

			if canSee {
				// Aim at player
				dx := closestVisiblePlayer.Position.X - enemy.Position.X
				dy := closestVisiblePlayer.Position.Y - enemy.Position.Y
				desiredRotation := math.Atan2(-dx, dy) * 180 / math.Pi
				if enemy.Type == types.EnemyTypeTower {
					// Smooth rotation for tower
					rotationDiff := desiredRotation - enemy.Rotation
					for rotationDiff < -180 {
						rotationDiff += 360
					}
					for rotationDiff > 180 {
						rotationDiff -= 360
					}

					maxRotationChange := config.EnemyTowerRotationSpeed * deltaTime
					if math.Abs(rotationDiff) < maxRotationChange {
						enemy.Rotation = desiredRotation
					} else {
						if rotationDiff > 0 {
							enemy.Rotation += maxRotationChange
						} else {
							enemy.Rotation -= maxRotationChange
						}

						// Normalize rotation to 0-360 range
						for enemy.Rotation < 0 {
							enemy.Rotation += 360
						}
						for enemy.Rotation >= 360 {
							enemy.Rotation -= 360
						}
					}
				} else {
					enemy.Rotation = desiredRotation
				}

				if enemy.Type == types.EnemyTypeSniper {
					if closestVisiblePlayer.ID != enemy.LastAimedAtPlayerID {
						// Reset shoot delay when switching targets
						enemy.ShootDelay = e.enemyShootDelay(config.EnemySniperShootDelay)
						enemy.LastShotAtPlayerID = ""
						enemy.LastAimedAtPlayerID = closestVisiblePlayer.ID
					}

					ray, exists := e.state.rays[enemy.ID]
					if exists {
						// Update existing laser pointer
						ray.EndPosition = closestVisiblePlayer.Position.Clone()
						ray.ScreenObject.Position = enemy.GetGunPoint()
					} else {
						// Add a laser pointer effect towards the player
						ray := &types.Ray{
							ScreenObject: types.ScreenObject{
								ID:       uuid.New().String(),
								Position: enemy.GetGunPoint(),
							},
							EndPosition: closestVisiblePlayer.Position.Clone(),
							OwnerID:     enemy.ID,
							Type:        types.RayTypeLaser,
						}

						e.state.rays[enemy.ID] = ray
					}
					activeRayOwners[enemy.ID] = true
				}

				// Shoot at player
				if enemy.ShootDelay <= 0 && enemy.Rotation == desiredRotation {
					bullet := enemy.ShootAt(closestVisiblePlayer.Position)

					e.state.bullets[bullet.ID] = bullet
					newBulletsIDs[bullet.ID] = true

					if enemy.Type == types.EnemyTypeSniper {
						e.applyBulletDamage(bullet, &types.Vector2{X: bullet.Position.X + bullet.Velocity.X, Y: bullet.Position.Y + bullet.Velocity.Y})
					}

					enemy.ShootDelay = e.enemyShootDelay(types.EnemyShootDelayByType[enemy.Type])
				}
			}

			shouldPatrol := false
			if enemy.Type == types.EnemyTypeSoldier && !canSee {
				shouldPatrol = true
			}
			if enemy.Type == types.EnemyTypeLieutenant {
				shouldPatrol = true
			}

			if shouldPatrol {
				// Patrol logic
				var wall *types.Wall
				var wallExists bool
				enemyChunkX, enemyChunkY := utils.ChunkXYFromPosition(enemy.Position.X, enemy.Position.Y)
				for neighborChunkX := enemyChunkX - 1; neighborChunkX <= enemyChunkX+1; neighborChunkX++ {
					for neighborChunkY := enemyChunkY - 1; neighborChunkY <= enemyChunkY+1; neighborChunkY++ {
						neighborChunkKey := makeChunkKey(neighborChunkX, neighborChunkY)
						if !e.chunkHash[neighborChunkKey] {
							continue
						}
						wall, wallExists = e.state.wallsByChunk[neighborChunkKey][enemy.WallID]
						if wallExists {
							break
						}
					}
					if wallExists {
						break
					}
				}
				if wallExists {
					var dx, dy float64
					if wall.Orientation == "vertical" {
						dy = config.EnemySoldierSpeed * float64(enemy.Direction) * deltaTime
						if !canSee {
							enemy.Rotation = 90 - 90*float64(enemy.Direction)
						}
					} else {
						dx = config.EnemySoldierSpeed * float64(enemy.Direction) * deltaTime
						if !canSee {
							enemy.Rotation = -90 * float64(enemy.Direction)
						}
					}

					// Skip collision checks if no movement
					if dx == 0 && dy == 0 {
						continue
					}

					// Check collisions with walls
					collision := false
					for neighborChunkX := enemyChunkX - 1; neighborChunkX <= enemyChunkX+1; neighborChunkX++ {
						for neighborChunkY := enemyChunkY - 1; neighborChunkY <= enemyChunkY+1; neighborChunkY++ {
							neighborChunkKey := makeChunkKey(neighborChunkX, neighborChunkY)
							if !e.chunkHash[neighborChunkKey] {
								continue
							}

							for _, w := range e.state.wallsByChunk[neighborChunkKey] {
								wallTopLeft := w.GetTopLeft()
								if utils.CheckCircleRectCollision(
									enemy.Position.X+dx, enemy.Position.Y+dy, enemy.Size()/2,
									wallTopLeft.X, wallTopLeft.Y, w.Width, w.Height) {
									collision = true
									break
								}
							}
							if collision {
								break
							}

							// Check collisions with other enemies
							for _, other := range e.state.enemiesByChunk[neighborChunkKey] {
								if other.ID != enemy.ID && other.IsAlive {
									if utils.CheckCircleCollision(
										enemy.Position.X+dx, enemy.Position.Y+dy, enemy.Size()/2,
										other.Position.X, other.Position.Y, other.Size()/2) {
										collision = true
										break
									}
								}
							}
							if collision {
								break
							}
						}
						if collision {
							break
						}
					}

					// Check collisions with players (only if no collision detected yet)
					if !collision {
						for _, player := range e.state.players {
							if !player.IsAlive || !player.IsConnected {
								continue
							}

							if utils.CheckCircleCollision(
								enemy.Position.X+dx, enemy.Position.Y+dy, enemy.Size()/2,
								player.Position.X, player.Position.Y, config.PlayerRadius) {
								collision = true
								break
							}
						}
					}

					if collision {
						enemy.Direction *= -1
					} else {
						enemy.Position.X += dx
						enemy.Position.Y += dy

						// Check patrol boundaries
						if wall.Orientation == "vertical" {
							if enemy.Position.Y < wall.Position.Y || enemy.Position.Y > wall.Position.Y+wall.Height {
								enemy.Direction *= -1
								enemy.Position.Y = math.Max(wall.Position.Y, math.Min(wall.Position.Y+wall.Height, enemy.Position.Y))
							}
						} else {
							if enemy.Position.X < wall.Position.X || enemy.Position.X > wall.Position.X+wall.Width {
								enemy.Direction *= -1
								enemy.Position.X = math.Max(wall.Position.X, math.Min(wall.Position.X+wall.Width, enemy.Position.X))
							}
						}
					}
				}
			}
		}
	}

	// Drop laser rays whose sniper didn't aim this tick: target died or left,
	// sniper died, or no living player is near enough for its chunk to be updated
	for ownerID := range e.state.rays {
		if !activeRayOwners[ownerID] {
			delete(e.state.rays, ownerID)
		}
	}

	if e.debugMode {
		updateDuration = time.Since(now)
		e.stats.TotalUpdateTime.enemies += updateDuration
		e.stats.TotalUpdateTimeSinceLastReport.enemies += updateDuration
		now = time.Now()
	}

	// Update bullets
	for _, bullet := range e.state.bullets {
		if _, isNew := newBulletsIDs[bullet.ID]; isNew {
			continue // Skip newly created bullets this tick
		}

		bullet.IsJustSpawned = false

		// Clean up deactivated bullets once the cache timeout has passed
		if !bullet.DeletedAt.IsZero() {
			if time.Since(bullet.DeletedAt) > config.DeadBulletCacheTimeout {
				delete(e.state.bullets, bullet.ID)
			}
			continue
		}

		// Hitscan shots (shotgun pellets, railgun beams) live for a single tick
		if bullet.WeaponType == types.WeaponTypeShotgun || bullet.WeaponType == types.WeaponTypeRailgun {
			bullet.IsActive = false
			bullet.DeletedAt = time.Now()
			continue
		}

		// Check lifetime
		maxLifetime, exists := types.BulletLifetimeByWeaponType[bullet.WeaponType]
		if exists && time.Since(bullet.SpawnTime) > maxLifetime {
			bullet.IsActive = false
			bullet.DeletedAt = time.Now()
			continue
		}

		// Update position
		dx := bullet.Velocity.X * deltaTime
		dy := bullet.Velocity.Y * deltaTime

		hitFound := false

		bulletNextChunkX, bulletNextChunkY := utils.ChunkXYFromPosition(bullet.Position.X+dx, bullet.Position.Y+dy)

		for neighborChunkX := bulletNextChunkX - 1; neighborChunkX <= bulletNextChunkX+1; neighborChunkX++ {
			for neighborChunkY := bulletNextChunkY - 1; neighborChunkY <= bulletNextChunkY+1; neighborChunkY++ {
				neighborChunkKey := makeChunkKey(neighborChunkX, neighborChunkY)
				if !e.chunkHash[neighborChunkKey] {
					continue
				}

				// Check collision with walls
				for _, wall := range e.state.wallsByChunk[neighborChunkKey] {
					topLeft := wall.GetTopLeft()
					ix, iy := utils.CutLineSegmentBeforeRect(
						bullet.Position.X, bullet.Position.Y, bullet.Position.X+dx, bullet.Position.Y+dy,
						topLeft.X, topLeft.Y,
						wall.Width, wall.Height)

					if !(ix == bullet.Position.X+dx && iy == bullet.Position.Y+dy) {
						hitFound = true
						dx = ix - bullet.Position.X
						dy = iy - bullet.Position.Y
					}
				}
			}
		}

		newPosition := &types.Vector2{X: bullet.Position.X + dx, Y: bullet.Position.Y + dy}

		hitCharacter, hitObjectIds := e.applyBulletDamage(bullet, newPosition)
		hitFound = hitFound || hitCharacter

		if bullet.WeaponType == types.WeaponTypeRocketLauncher && hitFound {
			// Rocket explosion - apply area damage
			e.applyRocketExplosionDamage(newPosition, hitObjectIds, bullet.OwnerID)
		}

		bullet.Position.X += dx
		bullet.Position.Y += dy

		if hitFound {
			bullet.IsActive = false
			bullet.DeletedAt = time.Now()
		}
	}

	if e.debugMode {
		updateDuration = time.Since(now)
		e.stats.TotalUpdateTime.bullets += updateDuration
		e.stats.TotalUpdateTimeSinceLastReport.bullets += updateDuration
		now = time.Now()
	}

	// Update bonuses - check pickup
	for _, bonus := range e.state.bonuses {
		// Check if bonus was picked up and needs cleanup
		if !bonus.PickedUpAt.IsZero() {
			if time.Since(bonus.PickedUpAt) > config.DeadEntitiesCacheTimeout {
				delete(e.state.bonuses, bonus.ID)
			}
			continue
		}

		// Check for dropped timeout
		if bonus.DroppedBy != "" && !bonus.DroppedAt.IsZero() && time.Since(bonus.DroppedAt) > config.PlayerDropInventoryLifetime {
			delete(e.state.bonuses, bonus.ID)
			continue
		}

		// Check pickup by players
		for _, player := range e.state.players {
			if !player.IsAlive || !player.IsConnected {
				continue
			}

			bonusRadius := config.AidKitSize / 2
			if bonus.Type == types.BonusTypeGoggles {
				bonusRadius = config.GogglesSize / 2
			}

			distance := player.DistanceToPoint(bonus.Position)

			if distance < config.PlayerRadius+bonusRadius {
				// Pickup!
				player.PickupBonus(bonus)
				break
			}
		}
	}

	if e.debugMode {
		// Update stats
		e.stats.UpdateCount++
		e.stats.UpdateCountSinceLastReport++

		updateDuration = time.Since(now)
		e.stats.TotalUpdateTime.bonuses += updateDuration
		e.stats.TotalUpdateTimeSinceLastReport.bonuses += updateDuration

		if e.stats.LastReportedAt.IsZero() || time.Since(e.stats.LastReportedAt) >= e.stats.Frequency {
			var avgUpdateTime time.Duration
			var avgUpdateTimeSinceLastReport time.Duration
			var avgDeltaCalcTime time.Duration
			var avgDeltaCalcTimeSinceLastReport time.Duration
			var avgUpdatePrevStateTime time.Duration
			var avgUpdatePrevStateTimeSinceLastReport time.Duration
			var avgUpdateTimeByType UpdateTimeStats
			var avgUpdateTimeByTypeSinceLastReport UpdateTimeStats

			if e.stats.UpdateCount > 0 {
				avgUpdateTime = e.stats.TotalUpdateTime.Total() / time.Duration(e.stats.UpdateCount)
				avgUpdateTimeByType = UpdateTimeStats{
					players: e.stats.TotalUpdateTime.players / time.Duration(e.stats.UpdateCount),
					enemies: e.stats.TotalUpdateTime.enemies / time.Duration(e.stats.UpdateCount),
					bullets: e.stats.TotalUpdateTime.bullets / time.Duration(e.stats.UpdateCount),
					bonuses: e.stats.TotalUpdateTime.bonuses / time.Duration(e.stats.UpdateCount),
				}
			}
			if e.stats.UpdateCountSinceLastReport > 0 {
				avgUpdateTimeSinceLastReport = e.stats.TotalUpdateTimeSinceLastReport.Total() / time.Duration(e.stats.UpdateCountSinceLastReport)
				avgUpdateTimeByTypeSinceLastReport = UpdateTimeStats{
					players: e.stats.TotalUpdateTimeSinceLastReport.players / time.Duration(e.stats.UpdateCountSinceLastReport),
					enemies: e.stats.TotalUpdateTimeSinceLastReport.enemies / time.Duration(e.stats.UpdateCountSinceLastReport),
					bullets: e.stats.TotalUpdateTimeSinceLastReport.bullets / time.Duration(e.stats.UpdateCountSinceLastReport),
					bonuses: e.stats.TotalUpdateTimeSinceLastReport.bonuses / time.Duration(e.stats.UpdateCountSinceLastReport),
				}
			}
			if e.stats.DeltaCalcCount > 0 {
				avgDeltaCalcTime = e.stats.TotalDeltaCalcTime.Total() / time.Duration(e.stats.DeltaCalcCount)
				avgUpdatePrevStateTime = e.stats.TotalDeltaCalcTime.updatePrevious / time.Duration(e.stats.DeltaCalcCount)
			}
			if e.stats.DeltaCalcCountSinceLastReport > 0 {
				avgDeltaCalcTimeSinceLastReport = e.stats.TotalDeltaCalcTimeSinceLastReport.Total() / time.Duration(e.stats.DeltaCalcCountSinceLastReport)
				avgUpdatePrevStateTimeSinceLastReport = e.stats.TotalDeltaCalcTimeSinceLastReport.updatePrevious / time.Duration(e.stats.DeltaCalcCountSinceLastReport)
			}

			// Print stats
			log.Printf(
				"Engine Stats - Session %s:\n"+
					"Total Updates: %d\n"+
					"Avg Update Time: %s\n"+
					"Players: %s, Enemies: %s, Bullets: %s, Bonuses: %s\n"+
					"Avg Update Time (last period): %s (%d rounds)\n"+
					"Players: %s (%d elements), Enemies: %s (%d checked), Bullets: %s (%d elements), Bonuses: %s (%d elements)\n"+
					"Avg Delta Calc Time: %s (of which %s for updating previous state)\n"+
					"Avg Delta Calc Time (last period): %s (of which %s for updating previous state, %d rounds)\n\n\n",
				e.sessionID,
				e.stats.UpdateCount,
				avgUpdateTime.String(),
				avgUpdateTimeByType.players.String(),
				avgUpdateTimeByType.enemies.String(),
				avgUpdateTimeByType.bullets.String(),
				avgUpdateTimeByType.bonuses.String(),
				avgUpdateTimeSinceLastReport.String(),
				e.stats.UpdateCountSinceLastReport,
				avgUpdateTimeByTypeSinceLastReport.players.String(),
				len(e.state.players),
				avgUpdateTimeByTypeSinceLastReport.enemies.String(),
				checkedEnemies,
				avgUpdateTimeByTypeSinceLastReport.bullets.String(),
				len(e.state.bullets),
				avgUpdateTimeByTypeSinceLastReport.bonuses.String(),
				len(e.state.bonuses),
				avgDeltaCalcTime.String(),
				avgUpdatePrevStateTime.String(),
				avgDeltaCalcTimeSinceLastReport.String(),
				avgUpdatePrevStateTimeSinceLastReport.String(),
				e.stats.DeltaCalcCountSinceLastReport,
			)

			e.stats.LastReportedAt = time.Now()
			e.stats.UpdateCountSinceLastReport = 0
			e.stats.TotalUpdateTimeSinceLastReport = UpdateTimeStats{}
			e.stats.DeltaCalcCountSinceLastReport = 0
			e.stats.TotalDeltaCalcTimeSinceLastReport = DeltaCalcStats{}
		}
	}
}

func (e *Engine) applyBulletDamage(bullet *types.Bullet, newPosition *types.Vector2) (hitFound bool, hitObjectIDs map[string]bool) {
	hitObjectIDs = make(map[string]bool)
	hitFound = false
	// Check collision with players
	for _, player := range e.state.players {
		if !player.IsConnected || !player.IsAlive || player.ID == bullet.OwnerID || player.InvulnerableTimer > 0 {
			continue
		}

		closestPointX, closestPointY := utils.ClosestPointOnLineSegment(bullet.Position.X, bullet.Position.Y, newPosition.X, newPosition.Y, player.Position.X, player.Position.Y)
		distance := player.DistanceToPoint(&types.Vector2{X: closestPointX, Y: closestPointY})

		if distance < config.PlayerRadius+config.BlasterBulletRadius {
			// Hit!
			player.Lives -= bullet.Damage
			if player.Lives <= 0 {
				chest := player.DropInventory()
				if chest != nil {
					e.state.bonuses[chest.ID] = chest
				}
				player.Die()

				// Award money to shooter
				if shooter, exists := e.state.players[bullet.OwnerID]; exists {
					e.awardKill(shooter, config.PlayerReward)
				}
			} else {
				player.InvulnerableTimer = config.PlayerInvulnerabilityTime
			}

			hitObjectIDs[player.ID] = true
			hitFound = true
		}
	}

	bulletChunkX, bulletChunkY := utils.ChunkXYFromPosition(newPosition.X, newPosition.Y)
	for neighborChunkX := bulletChunkX - 1; neighborChunkX <= bulletChunkX+1; neighborChunkX++ {
		for neighborChunkY := bulletChunkY - 1; neighborChunkY <= bulletChunkY+1; neighborChunkY++ {
			neighborChunkKey := makeChunkKey(neighborChunkX, neighborChunkY)
			if !e.chunkHash[neighborChunkKey] {
				continue
			}

			// Check collision with enemies
			for _, enemy := range e.state.enemiesByChunk[neighborChunkKey] {
				if !enemy.IsAlive || (bullet.IsEnemy && enemy.ID == bullet.OwnerID) {
					continue
				}

				closestPointX, closestPointY := utils.ClosestPointOnLineSegment(bullet.Position.X, bullet.Position.Y, newPosition.X, newPosition.Y, enemy.Position.X, enemy.Position.Y)
				distance := enemy.DistanceToPoint(&types.Vector2{X: closestPointX, Y: closestPointY})

				if distance < enemy.Size()/2+config.BlasterBulletRadius {
					// Hit!
					enemy.Lives -= bullet.Damage
					if enemy.Lives <= 0 {
						enemy.IsAlive = false
						enemy.DeadTimer = config.EnemyDeathTraceTime
						if enemy.Type == types.EnemyTypeTower {
							enemy.DeadTimer = config.EnemyTowerDeathTraceTime
						}
						// Award money to shooter
						if !bullet.IsEnemy {
							if shooter, exists := e.state.players[bullet.OwnerID]; exists {
								e.awardKill(shooter, enemy.Reward())
							}
						}

						e.spawnBonus(enemy)
					}
					hitFound = true
					hitObjectIDs[enemy.ID] = true
				}
			}
		}
	}

	return hitFound, hitObjectIDs
}

func (e *Engine) handlePlayerShooting(player *types.Player) (newBulletsIDs map[string]bool) {
	newBulletsIDs = make(map[string]bool)
	rotationRad := player.Rotation * math.Pi / 180.0
	bulletsLeft := player.BulletsLeftByWeaponType[player.SelectedGunType]
	usingBulletsFromInventory := false
	_, exists := types.MaxBulletsByWeaponType[player.SelectedGunType]
	if !exists {
		bulletsLeft = player.GetInventoryItemQuantity(types.InventoryAmmoIDByWeaponType[player.SelectedGunType])
		usingBulletsFromInventory = true
	}
	shootDelay := types.ShootDelayByWeaponType[player.SelectedGunType]

	if bulletsLeft > 0 && time.Since(player.LastShotAt).Seconds() >= shootDelay {
		player.LastShotAt = time.Now()
		if usingBulletsFromInventory {
			player.UseInventoryItem(types.InventoryAmmoIDByWeaponType[player.SelectedGunType], 1)
		} else {
			player.BulletsLeftByWeaponType[player.SelectedGunType]--
		}
		playerGunPoint := &types.Vector2{X: player.Position.X + config.PlayerGunEndOffsetX, Y: player.Position.Y + config.PlayerGunEndOffsetY}
		playerGunPoint.RotateAroundPoint(player.Position, player.Rotation)

		playerChunkX, playerChunkY := utils.ChunkXYFromPosition(player.Position.X, player.Position.Y)

		velocities := []*types.Vector2{}

		switch player.SelectedGunType {
		case types.WeaponTypeBlaster:
			velocities = append(velocities, &types.Vector2{
				X: -math.Sin(rotationRad) * config.BlasterBulletSpeed,
				Y: math.Cos(rotationRad) * config.BlasterBulletSpeed,
			})
		case types.WeaponTypeRocketLauncher:
			velocities = append(velocities, &types.Vector2{
				X: -math.Sin(rotationRad) * config.RocketLauncherBulletSpeed,
				Y: math.Cos(rotationRad) * config.RocketLauncherBulletSpeed,
			})
		case types.WeaponTypeShotgun:
			numPellets := config.ShotgunNumPellets
			spreadAngle := config.ShotgunSpreadAngle
			radius := config.ShotgunRange

			for i := 0; i < numPellets; i++ {
				angleOffset := (float64(i) - float64(numPellets-1)/2) * (spreadAngle / float64(numPellets-1))
				angleRad := rotationRad + angleOffset*math.Pi/180.0

				ix := playerGunPoint.X + -math.Sin(angleRad)*radius
				iy := playerGunPoint.Y + math.Cos(angleRad)*radius

				for neighborChunkX := playerChunkX - 1; neighborChunkX <= playerChunkX+1; neighborChunkX++ {
					for neighborChunkY := playerChunkY - 1; neighborChunkY <= playerChunkY+1; neighborChunkY++ {
						neighborChunkKey := makeChunkKey(neighborChunkX, neighborChunkY)
						if !e.chunkHash[neighborChunkKey] {
							continue
						}

						for _, wall := range e.state.wallsByChunk[neighborChunkKey] {
							wallTopLeft := wall.GetTopLeft()

							ix, iy = utils.CutLineSegmentBeforeRect(
								playerGunPoint.X,
								playerGunPoint.Y,
								ix,
								iy,
								wallTopLeft.X,
								wallTopLeft.Y,
								wall.Width,
								wall.Height,
							)
						}
					}
				}

				velocities = append(velocities, &types.Vector2{
					X: ix - playerGunPoint.X,
					Y: iy - playerGunPoint.Y,
				})
			}
		case types.WeaponTypeRailgun:
			ix := playerGunPoint.X + -math.Sin(rotationRad)*config.SightRadius
			iy := playerGunPoint.Y + math.Cos(rotationRad)*config.SightRadius

			for neighborChunkX := playerChunkX - 1; neighborChunkX <= playerChunkX+1; neighborChunkX++ {
				for neighborChunkY := playerChunkY - 1; neighborChunkY <= playerChunkY+1; neighborChunkY++ {
					neighborChunkKey := makeChunkKey(neighborChunkX, neighborChunkY)
					if !e.chunkHash[neighborChunkKey] {
						continue
					}

					for _, wall := range e.state.wallsByChunk[neighborChunkKey] {
						wallTopLeft := wall.GetTopLeft()

						ix, iy = utils.CutLineSegmentBeforeRect(
							playerGunPoint.X,
							playerGunPoint.Y,
							ix,
							iy,
							wallTopLeft.X,
							wallTopLeft.Y,
							wall.Width,
							wall.Height,
						)
					}
				}
			}

			velocities = append(velocities, &types.Vector2{
				X: ix - playerGunPoint.X,
				Y: iy - playerGunPoint.Y,
			})
		}

		damage := types.DamageByWeaponType[player.SelectedGunType] / float32(len(velocities))
		var batchID string
		if len(velocities) > 1 {
			batchID = uuid.New().String()
		}

		for _, velocity := range velocities {
			// Create bullet
			bullet := &types.Bullet{
				ScreenObject: types.ScreenObject{
					ID:       uuid.New().String(),
					Position: playerGunPoint,
				},
				BatchID:       batchID,
				Velocity:      velocity,
				OwnerID:       player.ID,
				IsActive:      true,
				SpawnTime:     time.Now(),
				IsJustSpawned: true,
				Damage:        damage,
				WeaponType:    player.SelectedGunType,
			}

			if player.SelectedGunType == types.WeaponTypeRailgun || player.SelectedGunType == types.WeaponTypeShotgun {
				e.applyBulletDamage(bullet, &types.Vector2{X: bullet.Position.X + velocity.X, Y: bullet.Position.Y + velocity.Y})
			}

			e.state.bullets[bullet.ID] = bullet
			newBulletsIDs[bullet.ID] = true
		}
	}

	return newBulletsIDs
}

func (e *Engine) applyRocketExplosionDamage(explosionCenter *types.Vector2, hitObjectIDs map[string]bool, ownerID string) {
	shooter, shooterExists := e.state.players[ownerID]

	// The blast radius is far smaller than a chunk, so only the explosion's
	// chunk and its neighbours can hold enemies within reach
	centerChunkX, centerChunkY := utils.ChunkXYFromPosition(explosionCenter.X, explosionCenter.Y)
	for neighborChunkX := centerChunkX - 1; neighborChunkX <= centerChunkX+1; neighborChunkX++ {
		for neighborChunkY := centerChunkY - 1; neighborChunkY <= centerChunkY+1; neighborChunkY++ {
			e.applyRocketExplosionDamageToEnemies(e.state.enemiesByChunk[makeChunkKey(neighborChunkX, neighborChunkY)], explosionCenter, hitObjectIDs, shooter, shooterExists)
		}
	}

	for _, player := range e.state.players {
		if !player.IsConnected || !player.IsAlive || hitObjectIDs[player.ID] || player.InvulnerableTimer > 0 {
			continue
		}

		distance := player.DistanceToPoint(explosionCenter)
		if distance < config.RocketLauncherDamageRadius {
			// Apply damage falloff
			damage := config.RocketLauncherDamage * (1 - distance/config.RocketLauncherDamageRadius)
			player.Lives -= float32(damage)
			if player.Lives <= 0 {
				chest := player.DropInventory()
				if chest != nil {
					e.state.bonuses[chest.ID] = chest
				}
				player.Die()

				if shooterExists && shooter.ID != player.ID {
					e.awardKill(shooter, config.PlayerReward)
				}
			} else {
				player.InvulnerableTimer = config.PlayerInvulnerabilityTime
			}
		}
	}
}

func (e *Engine) applyRocketExplosionDamageToEnemies(enemies map[string]*types.Enemy, explosionCenter *types.Vector2, hitObjectIDs map[string]bool, shooter *types.Player, shooterExists bool) {
	for _, enemy := range enemies {
		if !enemy.IsAlive || hitObjectIDs[enemy.ID] {
			continue
		}

		distance := enemy.DistanceToPoint(explosionCenter)
		if distance < config.RocketLauncherDamageRadius {
			// Apply damage falloff
			damage := config.RocketLauncherDamage * (1 - distance/config.RocketLauncherDamageRadius)
			enemy.Lives -= float32(damage)
			if enemy.Lives <= 0 {
				enemy.IsAlive = false
				enemy.DeadTimer = config.EnemyDeathTraceTime
				if enemy.Type == types.EnemyTypeTower {
					enemy.DeadTimer = config.EnemyTowerDeathTraceTime
				}

				if shooterExists {
					e.awardKill(shooter, enemy.Reward())
				}

				// Maybe spawn bonus
				e.spawnBonus(enemy)
			}
		}
	}
}

// awardKill pays a kill's reward (an enemy's or another player's). Money and
// score scale separately: money by the difficulty's money setting, score by
// its danger rating, so generous difficulties don't inflate the leaderboard.
func (e *Engine) awardKill(shooter *types.Player, hardReward float64) {
	shooter.Money += int(math.Round(hardReward * float64(e.difficulty.MoneyPct) / 100))
	shooter.Score += int(math.Round(hardReward * e.difficulty.ScoreMultiplier))
	shooter.Kills++
}

// spawnBonus creates a bonus at the given position
func (e *Engine) spawnBonus(enemy *types.Enemy) {
	// Maybe spawn bonus. The difficulty scales the soldier and lieutenant
	// drop chances; above 100% they always drop.
	dropScale := float64(e.difficulty.DropChancePct) / 100
	switch enemy.Type {
	case types.EnemyTypeSoldier:
		if rand.Float64() >= config.EnemySoldierDropChance*dropScale {
			return
		}
	case types.EnemyTypeLieutenant:
		if rand.Float64() >= config.EnemyLieutenantDropChance*dropScale {
			return
		}
	}

	var bonusType string
	inventory := []types.InventoryItem{}

	switch {
	case enemy.Type == types.EnemyTypeSniper:
		bonusType = types.BonusTypeChest

		if rand.Float64() < config.EnemySniperDropChanceAmmo {
			inventory = append(inventory, types.InventoryItem{
				Type:     types.InventoryItemRailgunAmmo,
				Quantity: int32(config.EnemySniperAmmoMinQuantity + rand.Intn(config.EnemySniperAmmoMaxQuantity-config.EnemySniperAmmoMinQuantity+1)),
			})
		}

		if rand.Float64() < config.EnemySniperDropChanceWeapon {
			inventory = append(inventory, types.InventoryItem{Type: types.InventoryItemRailgun, Quantity: 1})
		}

		if rand.Float64() < config.EnemySniperDropChanceGoggles {
			inventory = append(inventory, types.InventoryItem{Type: types.InventoryItemGoggles, Quantity: 1})
		}

		if len(inventory) == 0 {
			return // Nothing rolled, no chest
		}

	case enemy.Type == types.EnemyTypeLieutenant && rand.Float64() < config.EnemyLieutenantDropChanceWeapon:
		bonusType = types.BonusTypeChest
		inventory = []types.InventoryItem{{Type: types.InventoryItemShotgun, Quantity: 1}}

	case enemy.Type == types.EnemyTypeTower:
		bonusType = types.BonusTypeChest

		ammoItems := []types.InventoryItemID{
			types.InventoryItemShotgunAmmo,
			types.InventoryItemRocket,
			types.InventoryItemRailgunAmmo,
		}

		for _, itemID := range ammoItems {
			if rand.Float64() < config.TowerAmmoProbability {
				inventory = append(inventory, types.InventoryItem{
					Type:     itemID,
					Quantity: int32(config.TowerAmmoMinQuantity + rand.Intn(config.TowerAmmoMaxQuantity-config.TowerAmmoMinQuantity+1)),
				})
			}
		}

		if rand.Float64() < config.TowerAidKitProbability {
			inventory = append(inventory, types.InventoryItem{
				Type:     types.InventoryItemAidKit,
				Quantity: int32(config.TowerAidKitMinQuantity + rand.Intn(config.TowerAidKitMaxQuantity-config.TowerAidKitMinQuantity+1)),
			})
		}

		if rand.Float64() < config.TowerGogglesProbability {
			inventory = append(inventory, types.InventoryItem{
				Type:     types.InventoryItemGoggles,
				Quantity: int32(config.TowerGogglesMinQuantity + rand.Intn(config.TowerGogglesMaxQuantity-config.TowerGogglesMinQuantity+1)),
			})
		}

	default:
		bonusType = types.BonusTypeAidKit
		inventoryItemID := types.InventoryItemAidKit
		if rand.Float64() < config.EnemySoldierDropChanceGoggles {
			bonusType = types.BonusTypeGoggles
			inventoryItemID = types.InventoryItemGoggles
		}
		inventory = []types.InventoryItem{{Type: inventoryItemID, Quantity: 1}}
	}

	bonus := &types.Bonus{
		ScreenObject: types.ScreenObject{
			ID:       uuid.New().String(),
			Position: &types.Vector2{X: enemy.Position.X, Y: enemy.Position.Y},
		},
		Type:      bonusType,
		Inventory: inventory,
	}

	e.state.bonuses[bonus.ID] = bonus
}

func (e *Engine) GetAllPlayers() []*types.Player {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// Deep copy to avoid race conditions
	playersCopy := make([]*types.Player, 0, len(e.state.players))
	for _, v := range e.state.players {
		playersCopy = append(playersCopy, v.Clone())
	}

	return playersCopy
}

// GetGameStateDeltaForPlayer computes the delta filtered to player's surrounding
// chunks (-1 to 1) against the last committed view. It does not modify engine
// state: pass the returned view to CommitPlayerView once the delta has been
// handed to the client (or was empty), otherwise the next delta repeats it.
func (e *Engine) GetGameStateDeltaForPlayer(playerID string) (*protocol.GameStateDeltaMessage, *PlayerView) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	now := time.Now()
	prevState := e.prevState[playerID]
	if prevState == nil {
		prevState = &EngineGameState{}
	}

	player, exists := e.state.players[playerID]
	if !exists || !player.IsConnected {
		// Return empty delta if player doesn't exist
		return &protocol.GameStateDeltaMessage{}, nil
	}

	// Previous-view entries already handled in the per-chunk passes; the rest
	// are reported as removed at the end
	seenEnemies := make(map[string]bool)
	seenWalls := make(map[string]bool)
	seenShops := make(map[string]bool)
	seenBonuses := make(map[string]bool)
	seenRays := make(map[string]bool)

	playerChunkX, playerChunkY := utils.ChunkXYFromPosition(player.Position.X, player.Position.Y)

	delta := &protocol.GameStateDeltaMessage{
		AddedPlayers:   make(map[string]*protocol.Player),
		UpdatedPlayers: make(map[string]*protocol.PlayerUpdate),

		AddedBullets:   make(map[string]*protocol.Bullet),
		UpdatedBullets: make(map[string]*protocol.PositionUpdate),
		RemovedBullets: make(map[string]*protocol.Bullet),

		AddedWalls: make(map[string]*protocol.Wall),

		AddedEnemies:   make(map[string]*protocol.Enemy),
		UpdatedEnemies: make(map[string]*protocol.EnemyUpdate),

		AddedBonuses:   make(map[string]*protocol.Bonus),
		UpdatedBonuses: make(map[string]*protocol.BonusUpdate),

		AddedShops:   make(map[string]*protocol.Shop),
		UpdatedShops: make(map[string]*protocol.ShopUpdate),

		AddedRays:   make(map[string]*protocol.Ray),
		UpdatedRays: make(map[string]*protocol.RayUpdate),

		UpdatedOtherPlayerPositions: make(map[string]*protocol.Vector2),

		Timestamp: time.Now().UnixMilli(),
	}

	playersAbleToSee := make(map[string]*types.Player)
	playersAbleToSee[playerID] = player

	if player.NightVisionTimer <= 0 {
		for id, playerFromState := range e.state.players {
			if playerFromState.IsConnected && id != playerID && playerFromState.IsPositionDetectable() && playerFromState.IsVisibleToPlayer(player) {
				playersAbleToSee[id] = playerFromState
			}
		}
	}

	// Check for added/updated players in visible chunks
	for id, playerFromState := range e.state.players {
		if !playerFromState.IsConnected {
			continue
		}

		prev, prevExists := prevState.players[id]
		if playerFromState.ID == playerID || isPlayerVisibleToAny(playerFromState, playersAbleToSee) {
			if !prevExists {
				delta.AddedPlayers[id] = protocol.ToProtoPlayer(playerFromState)
			} else {
				updatedPlayer := protocol.ToProtoPlayerUpdate(prev, playerFromState, playerFromState.ID == playerID)
				if updatedPlayer != nil {
					delta.UpdatedPlayers[id] = updatedPlayer
				}
			}
		}

		// Markers cover every detectable player, seen or not, so they are
		// compared with the markers the client has, not with the seen players.
		if playerFromState.ID != playerID && playerFromState.IsPositionDetectable() {
			marker, hasMarker := prevState.markers[id]
			if !hasMarker || marker != *playerFromState.Position {
				delta.UpdatedOtherPlayerPositions[id] = &protocol.Vector2{
					X: playerFromState.Position.X,
					Y: playerFromState.Position.Y,
				}
			}
		}
	}

	// A marker goes when its player leaves, puts goggles on or dies, whether or
	// not the client can see them.
	for id := range prevState.markers {
		current, exists := e.state.players[id]
		if !exists || !current.IsPositionDetectable() {
			delta.RemovedOtherPlayerPositions = append(delta.RemovedOtherPlayerPositions, id)
		}
	}

	// Check for removed players that were in visible chunks
	// The view holds exactly the players the client knows, so any of them not
	// visible any more is removed.
	for id := range prevState.players {
		current, currentExists := e.state.players[id]
		if !currentExists || !current.IsConnected ||
			(id != playerID && !isPlayerVisibleToAny(current, playersAbleToSee)) {
			delta.RemovedPlayers = append(delta.RemovedPlayers, id)
		}
	}

	// Check for added bullets in visible chunks
	for id, bullet := range e.state.bullets {
		prev, prevExists := prevState.bullets[id]
		isBulletVisible := false
		for _, playerAbleToSee := range playersAbleToSee {
			if bullet.IsVisibleToPlayer(playerAbleToSee) {
				isBulletVisible = true
				break
			}
		}

		if isBulletVisible {
			if !prevExists {
				delta.AddedBullets[id] = protocol.ToProtoBullet(bullet)
				continue
			}

			// Bullets fly in a straight line at constant speed, so clients move
			// them from the position and velocity sent when they were added.
			// Only the end of the flight is reported, as a removal.
			if bullet.IsActive {
				continue
			}
		}

		if prevExists && prev.IsActive {
			// The full bullet is sent: clients need its final position, weapon
			// type and deactivation time for impact effects and sounds
			delta.RemovedBullets[id] = protocol.ToProtoBullet(bullet)
		}
	}

	for neighborChunkX := playerChunkX - 1; neighborChunkX <= playerChunkX+1; neighborChunkX++ {
		for neighborChunkY := playerChunkY - 1; neighborChunkY <= playerChunkY+1; neighborChunkY++ {
			neighborChunkKey := makeChunkKey(neighborChunkX, neighborChunkY)
			if !e.chunkHash[neighborChunkKey] {
				continue
			}

			enemyIDsInUpdatedState := []string{}

			// Check for added/updated enemies in visible chunks
			for id, enemy := range e.state.enemiesByChunk[neighborChunkKey] {
				currentVisible := false
				for _, playerAbleToSee := range playersAbleToSee {
					if enemy.IsVisibleToPlayer(playerAbleToSee) {
						currentVisible = true
						break
					}
				}

				prev, prevExists := prevState.enemiesByChunk[neighborChunkKey][id]

				if currentVisible {
					enemyIDsInUpdatedState = append(enemyIDsInUpdatedState, id)
					if !prevExists {
						delta.AddedEnemies[id] = protocol.ToProtoEnemy(enemy)
					} else {
						enemyUpdate := protocol.ToProtoEnemyUpdate(prev, enemy)
						if enemyUpdate != nil {
							delta.UpdatedEnemies[id] = enemyUpdate
						}
					}
				}

				if prevExists {
					if !currentVisible {
						delta.RemovedEnemies = append(delta.RemovedEnemies, id)
					}
					seenEnemies[id] = true
				}
			}

			for id, wall := range e.state.wallsByChunk[neighborChunkKey] {
				// Walls are always visible to players so no need to check nearby players
				currentVisible := wall.IsVisibleToPlayer(player) || e.enemiesHaveWall(enemyIDsInUpdatedState, wall.ID)
				_, prevExists := prevState.wallsByChunk[neighborChunkKey][id]
				if currentVisible && !prevExists {
					delta.AddedWalls[id] = protocol.ToProtoWall(wall)
				}

				if prevExists {
					if !currentVisible {
						delta.RemovedWalls = append(delta.RemovedWalls, id)
					}
					seenWalls[id] = true
				}
			}

			for id, shop := range e.state.shopsByChunk[neighborChunkKey] {
				currentVisible := false
				prevVisible := false
				for _, playerAbleToSee := range playersAbleToSee {
					if shop.IsVisibleToPlayer(playerAbleToSee) {
						currentVisible = true
					}

					prevPlayerAbleToSee, existsInPrev := prevState.players[playerAbleToSee.ID]
					if existsInPrev && shop.IsVisibleToPlayer(prevPlayerAbleToSee) {
						prevVisible = true
					}
				}

				if !currentVisible && !prevVisible {
					continue
				}

				prev, prevExists := prevState.shopsByChunk[neighborChunkKey][id]
				prevPlayer := prevState.players[playerID]

				if currentVisible && shop.IsPlayerInShop(player) && (prevPlayer == nil || !shop.IsPlayerInShop(prevPlayer)) {
					delta.AddedPlayersShops = append(delta.AddedPlayersShops, id)
				}

				if prev != nil && prevPlayer != nil &&
					prevVisible &&
					shop.IsPlayerInShop(prevPlayer) &&
					(!currentVisible || !shop.IsPlayerInShop(player)) {
					delta.RemovedPlayersShops = append(delta.RemovedPlayersShops, id)
				}

				if currentVisible {
					if !prevExists {
						delta.AddedShops[id] = protocol.ToProtoShop(shop)
					} else {
						shopUpdate := protocol.ToProtoShopUpdate(prev, shop)
						if shopUpdate != nil {
							delta.UpdatedShops[id] = shopUpdate
						}
					}
				}

				if prevExists {
					if !currentVisible {
						delta.RemovedShops = append(delta.RemovedShops, id)
					}
					seenShops[id] = true
				}
			}
		}
	}

	// Check for removed enemies that were in visible chunks
	for _, enemies := range prevState.enemiesByChunk {
		for id := range enemies {
			if !seenEnemies[id] {
				delta.RemovedEnemies = append(delta.RemovedEnemies, id)
			}
		}
	}

	// Check for removed walls that were in visible chunks
	for _, walls := range prevState.wallsByChunk {
		for id := range walls {
			if !seenWalls[id] {
				delta.RemovedWalls = append(delta.RemovedWalls, id)
			}
		}
	}

	// Check for removed shops that were in visible chunks
	for _, shops := range prevState.shopsByChunk {
		for id := range shops {
			if !seenShops[id] {
				delta.RemovedShops = append(delta.RemovedShops, id)
			}
		}
	}

	// Check for added bonuses in visible chunks
	for id, bonus := range e.state.bonuses {
		currentVisible := false
		for _, playerAbleToSee := range playersAbleToSee {
			if bonus.IsVisibleToPlayer(playerAbleToSee) {
				currentVisible = true
				break
			}
		}

		if currentVisible {
			prevBonus, prevExists := prevState.bonuses[id]

			if !prevExists {
				delta.AddedBonuses[id] = protocol.ToProtoBonus(bonus)
			} else {
				bonusUpdate := protocol.ToProtoBonusUpdate(prevBonus, bonus)
				if bonusUpdate != nil {
					delta.UpdatedBonuses[id] = bonusUpdate
				}
				seenBonuses[id] = true
			}
		}
	}

	for id := range prevState.bonuses {
		if !seenBonuses[id] {
			delta.RemovedBonuses = append(delta.RemovedBonuses, id)
		}
	}

	for _, ray := range e.state.rays {
		prev, prevExists := prevState.rays[ray.ID]
		isVisible := ray.IsVisibleToPlayer(player)

		if !prevExists {
			// Only announce rays the player can see: prevState tracks visible rays only,
			// so an invisible ray sent here would never get a matching removal.
			if isVisible {
				delta.AddedRays[ray.ID] = protocol.ToProtoRay(ray)
			}
			continue
		}

		if isVisible {
			rayUpdate := protocol.ToProtoRayUpdate(prev, ray)
			if rayUpdate != nil {
				delta.UpdatedRays[ray.ID] = rayUpdate
			}
		} else {
			delta.RemovedRays = append(delta.RemovedRays, ray.ID)
		}
		seenRays[ray.ID] = true
	}

	for id := range prevState.rays {
		if !seenRays[id] {
			delta.RemovedRays = append(delta.RemovedRays, id)
		}
	}

	view := &PlayerView{deltaTime: time.Since(now)}
	now = time.Now()
	view.state = e.buildPlayerView(playerID)
	view.updatePrevTime = time.Since(now)

	return delta, view
}

func (e *Engine) enemiesHaveWall(enemyIDs []string, wallID string) bool {
	for _, enemyID := range enemyIDs {
		for _, enemies := range e.state.enemiesByChunk {
			enemy, exists := enemies[enemyID]
			if exists && enemy.WallID == wallID {
				return true
			}
		}
	}
	return false
}

// isPlayerVisibleToAny reports whether any of the viewers can see the player.
func isPlayerVisibleToAny(p *types.Player, viewers map[string]*types.Player) bool {
	for _, viewer := range viewers {
		if p.IsVisibleToPlayer(viewer) {
			return true
		}
	}
	return false
}
