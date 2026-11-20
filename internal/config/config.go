package config

import (
	"log"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

const GameVersion = "1.4.0"

type Config struct {
	DBDriver                 string // "mongodb" or "sqlite"
	MongoDBURL               string
	SQLitePath               string
	SecretKey                string
	GoogleClientID           string
	GoogleClientSecret       string
	GoogleAuthEnabled        bool // GOOGLE_AUTH_ENABLED
	LocalAuthEnabled         bool // LOCAL_AUTH_ENABLED: username + password accounts
	APIBaseURL               string
	FrontendURL              string
	AccessTokenExpireMinutes int
	Host                     string // HOST: listen host (the -host flag overrides it)
	Port                     string
	UseTLS                   bool
	TLSCert                  string
	TLSKey                   string
	EngineDebugMode          bool
	DataDir                  string // DATA_DIR: per-user data directory (desktop mode); empty when unset
	StaticDir                string // STATIC_DIR: built web client to serve at "/"; empty disables
}

var AppConfig *Config

// LoadOptions tunes how LoadConfig reads its sources.
type LoadOptions struct {
	// SkipDotEnv ignores a .env file in the working directory. The desktop
	// app sets it, because its working directory is not under its control.
	SkipDotEnv bool
}

// LoadConfig loads configuration from environment variables
func LoadConfig(opts LoadOptions) *Config {
	// Load .env file if it exists
	if opts.SkipDotEnv {
		log.Println("Skipping .env file, using environment variables")
	} else if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using environment variables")
	}

	expireMinutes := 11520 // Default: 8 days
	if expireStr := os.Getenv("ACCESS_TOKEN_EXPIRE_MINUTES"); expireStr != "" {
		if val, err := strconv.Atoi(expireStr); err == nil {
			expireMinutes = val
		}
	}

	useTLS := false
	if tlsStr := os.Getenv("USE_TLS"); tlsStr == "true" {
		useTLS = true
	}

	engineDebugMode := false
	if debugStr := os.Getenv("ENGINE_DEBUG_MODE"); debugStr == "true" {
		engineDebugMode = true
	}

	config := &Config{
		DBDriver:                 getEnvOrDefault("DB_DRIVER", ""),
		MongoDBURL:               getEnvOrDefault("MONGODB_URL", ""),
		SQLitePath:               getEnvOrDefault("SQLITE_PATH", "data/dungerra.db"),
		SecretKey:                getEnvOrDefault("SECRET_KEY", ""),
		GoogleClientID:           getEnvOrDefault("GOOGLE_CLIENT_ID", ""),
		GoogleClientSecret:       getEnvOrDefault("GOOGLE_CLIENT_SECRET", ""),
		APIBaseURL:               getEnvOrDefault("API_BASE_URL", "http://localhost:8080"),
		FrontendURL:              getEnvOrDefault("FRONTEND_URL", "http://localhost:9000"),
		AccessTokenExpireMinutes: expireMinutes,
		Host:                     getEnvOrDefault("HOST", "localhost"),
		Port:                     getEnvOrDefault("PORT", "8080"),
		UseTLS:                   useTLS,
		TLSCert:                  getEnvOrDefault("TLS_CERT", ""),
		TLSKey:                   getEnvOrDefault("TLS_KEY", ""),
		EngineDebugMode:          engineDebugMode,
		DataDir:                  getEnvOrDefault("DATA_DIR", ""),
		StaticDir:                getEnvOrDefault("STATIC_DIR", ""),
	}

	// Desktop mode keeps everything in one per-user directory, because the
	// working directory of a bundled app is arbitrary. An explicit SQLITE_PATH
	// still wins.
	if config.DataDir != "" {
		abs, err := filepath.Abs(config.DataDir)
		if err != nil {
			log.Fatalf("Invalid DATA_DIR %q: %v", config.DataDir, err)
		}
		config.DataDir = abs
		if err := os.MkdirAll(config.DataDir, 0o700); err != nil {
			log.Fatalf("Cannot create DATA_DIR %q: %v", config.DataDir, err)
		}
		if os.Getenv("SQLITE_PATH") == "" {
			config.SQLitePath = filepath.Join(config.DataDir, "dungerra.db")
		}
	}
	if config.StaticDir != "" {
		if _, err := os.Stat(filepath.Join(config.StaticDir, "index.html")); err != nil {
			log.Fatalf("STATIC_DIR %q must contain index.html: %v", config.StaticDir, err)
		}
	}

	// Validate required fields
	// Storage backend. When DB_DRIVER is not set, existing deployments that
	// configure MONGODB_URL keep using MongoDB; otherwise the embedded SQLite
	// store is used so the server runs with zero external dependencies.
	if config.DBDriver == "" {
		if config.MongoDBURL != "" {
			config.DBDriver = "mongodb"
		} else {
			config.DBDriver = "sqlite"
		}
	}
	switch config.DBDriver {
	case "mongodb":
		if config.MongoDBURL == "" {
			log.Fatal("MONGODB_URL is required when DB_DRIVER=mongodb")
		}
	case "sqlite":
		if config.SQLitePath == "" {
			log.Fatal("SQLITE_PATH must not be empty when DB_DRIVER=sqlite")
		}
	default:
		log.Fatalf("Unsupported DB_DRIVER %q (expected \"mongodb\" or \"sqlite\")", config.DBDriver)
	}
	if config.SecretKey == "" && config.DataDir != "" {
		// A desktop install has nobody to set a secret, so it generates one
		// once and keeps it, which keeps issued tokens valid across restarts.
		key, err := LoadOrCreateSecretKey(filepath.Join(config.DataDir, SecretKeyFileName))
		if err != nil {
			log.Fatalf("SECRET_KEY is not set and the generated key is unusable: %v", err)
		}
		config.SecretKey = key
	}
	if config.SecretKey == "" {
		log.Fatal("SECRET_KEY is required (or set DATA_DIR to generate one)")
	}

	// Auth providers. Google defaults to on when its credentials are present
	// (so existing deployments are unchanged); local accounts default to on
	// only when Google is off, so a fresh/desktop setup is usable out of the box.
	hasGoogleCreds := config.GoogleClientID != "" && config.GoogleClientSecret != ""
	config.GoogleAuthEnabled = getEnvBool("GOOGLE_AUTH_ENABLED", hasGoogleCreds)
	config.LocalAuthEnabled = getEnvBool("LOCAL_AUTH_ENABLED", !config.GoogleAuthEnabled)
	if config.GoogleAuthEnabled && !hasGoogleCreds {
		log.Fatal("GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET are required when GOOGLE_AUTH_ENABLED=true")
	}
	if !config.GoogleAuthEnabled && !config.LocalAuthEnabled {
		log.Fatal("No authentication provider enabled: set GOOGLE_AUTH_ENABLED and/or LOCAL_AUTH_ENABLED")
	}

	AppConfig = config
	return config
}

// getEnvBool parses a boolean env var (true/false/1/0/...), falling back to
// defaultValue when unset. An unparsable value is a configuration error.
func getEnvBool(key string, defaultValue bool) bool {
	raw := os.Getenv(key)
	if raw == "" {
		return defaultValue
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		log.Fatalf("%s must be true or false, got %q", key, raw)
	}
	return v
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// Constants
const (
	// Player constants
	PlayerLives = 6.0
	PlayerSpeed = 300.0 // Units per second

	PlayerSize          = 24.0
	PlayerRadius        = PlayerSize / 2
	PlayerGunEndOffsetX = -10.0
	PlayerGunEndOffsetY = 20.0
	PlayerTorchOffsetX  = 10.0
	PlayerTorchOffsetY  = 18.0

	PlayerRotationSpeed            = 720.0 // Max degrees per second the player turns toward the aim direction
	PlayerInvulnerabilityTime      = 1.0   // Seconds
	PlayerSpawnInvulnerabilityTime = 3.0   // Seconds after spawn
	PlayerReward                   = 100.0 // Money for killing enemy
	PlayerDropInventoryLifetime    = 5 * time.Minute

	// Blaster constants
	BlasterBulletDamage       = 1
	BlasterBulletSize         = 8.0
	BlasterBulletRadius       = BlasterBulletSize / 2
	BlasterBulletSpeed        = 420.0 // Units per second
	BlasterBulletLifetime     = 3 * time.Second
	BlasterShootDelay         = 0.2 // Seconds
	BlasterMaxBullets         = 6
	BlasterBulletRechargeTime = 1.0 // Seconds per bullet

	// Shotgun constants
	ShotgunShootDelay         = 0.2 // Seconds
	ShotgunMaxBullets         = 2
	ShotgunBulletRechargeTime = 2    // Seconds
	ShotgunSpreadAngle        = 30.0 // Degrees
	ShotgunNumPellets         = 8
	ShotgunDamage             = 2.0
	ShotgunRange              = 200.0

	// Rocket Launcher constants
	RocketLauncherShootDelay     = 1.5   // Seconds
	RocketLauncherBulletSpeed    = 300.0 // Units per second
	RocketLauncherDamage         = 2
	RocketLauncherDamageRadius   = 150.0
	RocketLauncherBulletLifetime = 5 * time.Second

	// Railgun constants
	RailgunShootDelay = 1.0 // Seconds
	RailgunDamage     = 3.0
	RailgunRange      = SightRadius

	// Enemy constants
	EnemyDeathTraceTime      = 5.0  // Seconds
	EnemyTowerDeathTraceTime = 30.0 // Seconds
	EnemyLieutenantChance    = 0.15 // 15% chance to spawn lieutenant instead of soldier
	EnemySniperChance        = 0.05 // 5% chance to spawn sniper instead of soldier
	EnemySpawnChancePerWall  = 0.9  // 90% chance to spawn enemy for each wall

	// Enemy soldier constants
	EnemySoldierSpeed         = 120.0 // Units per second
	EnemySoldierSize          = 24.0
	EnemySoldierGunEndOffsetX = -1.0
	EnemySoldierGunEndOffsetY = 26.0

	EnemySoldierLives       = 1.0
	EnemySoldierShootDelay  = 1.0   // Seconds
	EnemySoldierBulletSpeed = 240.0 // Units per second

	EnemySoldierReward            = 20.0 // Money reward
	EnemySoldierDropChance        = 0.3  // 30% chance to drop bonus
	EnemySoldierDropChanceGoggles = 0.2  // 20% chance to drop goggles if dropping bonus

	// Enemy lieutenant constants
	EnemyLieutenantLives            = 2.0
	EnemyLieutenantShootDelay       = 0.5  // Seconds
	EnemyLieutenantReward           = 50.0 // Money reward
	EnemyLieutenantDropChance       = 0.5  // 50% chance to drop bonus
	EnemyLieutenantDropChanceWeapon = 0.3  // 30% chance to drop a shotgun if dropping bonus

	// Enemy sniper constants
	EnemySniperLives             = 2.0
	EnemySniperShootDelay        = 2.5   // Seconds
	EnemySniperRange             = 800.0 // Max distance to spot, aim at and hit a player
	EnemySniperReward            = 100.0 // Money reward
	EnemySniperDropChanceAmmo    = 0.5   // 50% chance to drop railgun ammo
	EnemySniperAmmoMinQuantity   = 5
	EnemySniperAmmoMaxQuantity   = 20
	EnemySniperDropChanceWeapon  = 0.5  // 50% chance to drop a railgun
	EnemySniperDropChanceGoggles = 0.75 // 75% chance to drop goggles
	EnemySniperGunEndOffsetX     = 0.0
	EnemySniperGunEndOffsetY     = 33.0

	// Enemy tower constants
	EnemyTowerLives       = 30.0
	EnemyTowerShootDelay  = 2.0   // Seconds
	EnemyTowerReward      = 200.0 // Money reward
	EnemyTowerSize        = 120.0
	EnemyTowerBulletSpeed = 300.0 // Units per second

	EnemyTowerGunEndOffsetX = 0.0
	EnemyTowerGunEndOffsetY = 80.0

	EnemyTowerRotationSpeed = 90.0 // Degrees per second

	// Bonus constants
	AidKitSize        = 32.0
	AidKitHealAmount  = 1.0
	GogglesSize       = 32.0
	GogglesActiveTime = 20.0 // Seconds
	ChestSize         = 32.0

	// World constants
	ChunkSize            = 2000.0
	SightRadius          = 1500.0
	WallWidth            = 30.0
	MinWallsPerKiloPixel = 5
	MaxWallsPerKiloPixel = 10
	ShopSize             = 64.0

	// Vision constants
	TorchRadius                = 200.0
	NightVisionDetectionRadius = 100.0

	// Session constants
	SessionSaveInterval      = 5 * time.Minute
	DeadEntitiesCacheTimeout = 5 * time.Second
	// How long a spent bullet stays in the world after hitting something, so a
	// player who comes into view right after still gets its after-effect. Must
	// cover the longest client-side bullet effect (rocket explosion, 0.8 s).
	DeadBulletCacheTimeout = 1 * time.Second
	GameLoopInterval       = time.Second / 30

	// Shop constants
	ShopAmmoProbability = 0.7
	ShopAmmoMinQuantity = 10
	ShopAmmoMaxQuantity = 20

	ShopWeaponProbability = 0.7
	ShopWeaponMinQuantity = 2
	ShopWeaponMaxQuantity = 5

	ShopAidKitProbability = 0.5
	ShopAidKitMinQuantity = 10
	ShopAidKitMaxQuantity = 20

	ShopGogglesProbability = 0.3
	ShopGogglesMinQuantity = 3
	ShopGogglesMaxQuantity = 6

	// Tower constants
	TowerAmmoProbability = 0.5
	TowerAmmoMinQuantity = 5
	TowerAmmoMaxQuantity = 20

	TowerAidKitProbability = 0.3
	TowerAidKitMinQuantity = 1
	TowerAidKitMaxQuantity = 3

	TowerGogglesProbability = 0.2
	TowerGogglesMinQuantity = 1
	TowerGogglesMaxQuantity = 2

	// Wall chest constants (loot generated next to walls; how many walls get
	// one is the session difficulty's WallChestPct)
	WallChestMoneyMin           = 20
	WallChestMoneyMax           = 100
	WallChestMoneyDistanceScale = 10000.0 // Units from the world origin per +100% money
	WallChestMoneyMaxMultiplier = 3.0     // Distance bonus cap (x3 at 20000 units)
	WallChestAidKitProbability  = 0.4
	WallChestAidKitMinQuantity  = 1
	WallChestAidKitMaxQuantity  = 2
	WallChestGogglesProbability = 0.15
	WallChestAmmoProbability    = 0.3 // One random ammo type
	WallChestAmmoMinQuantity    = 5
	WallChestAmmoMaxQuantity    = 15
	WallChestWeaponProbability  = 0.05 // One random non-blaster weapon
	WallChestGap                = 4.0  // Gap between the wall face and the chest edge
)
