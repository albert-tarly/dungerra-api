# Multiplayer Shooter Game Server

A real-time authoritative multiplayer shooter game server written in Go with WebSocket support, Google OAuth authentication, and pluggable persistence (embedded SQLite or MongoDB).

## Features

- **Authentication & Authorization**:
  - Pluggable sign-in providers behind feature switches: Google OAuth 2.0 and local username/password accounts
  - JWT (JSON Web Token) based session management
  - Persistent user accounts with automatic account creation
  - Secure WebSocket connections with token validation
- **Authoritative Server Architecture**: All game logic runs on the server to prevent cheating
- **Real-time Multiplayer**: WebSocket-based communication for low-latency gameplay
- **Binary Protocol Support**: Optional Protocol Buffers encoding for 60% bandwidth reduction (see [Binary Protocol section](docs/development/binary-protocol.md))
- **Database Persistence**:
  - Two interchangeable backends, selected with `DB_DRIVER`:
    - `sqlite` — embedded single-file database (pure Go, no CGO), ideal for local/desktop builds
    - `mongodb` — for hosted deployments
  - See [Database documentation](docs/development/database.md) and [Authentication documentation](docs/development/authentication.md)
  - User profiles with stats, progress tracking
  - Game session management and state persistence
- **Core Game Mechanics**:
  - Mouse-aim controls: facing turns toward the client's aim (mouse cursor); move forward/backward along the facing and strafe left/right
  - Lives system with invulnerability after taking damage
  - Bullet recharge system (6 bullets, recharge over time)
  - Shooting mechanics with fire rate limiting
  - Hit detection and collision system with sliding collision resolution
  - Health and scoring system with monetary rewards
  - Enemy AI with patrol and shooting behavior
  - Procedural wall generation in chunks
  - Power-ups: Aid kits (heal) and Night vision goggles
  - Map boundaries with chunk-based world generation
- **30 FPS Game Loop**: Smooth server-side physics and updates
- **Scalable Design**: Concurrent client handling with goroutines

## Project Structure

```
dungerra-api/
├── main.go                         # Server entry point, HTTP routes, CORS
├── go.mod / go.sum                 # Go module definition
├── package.json                    # Node tooling for protobuf TypeScript generation
├── .env.example                    # Environment variables template
├── CHANGELOG.md                    # Release notes
├── CLAUDE.md                       # Guidance for AI coding assistants
├── dist/
│   └── index.ts                    # TypeScript entry point re-exporting the generated protocol types
├── docs/
│   ├── development/                # Developer docs (API, auth, database, binary protocol, deployment, TS usage)
│   └── user/                       # Player guide (getting started, weapons, enemies, items, scoring, FAQ)
├── scripts/
│   ├── generate-proto.sh           # Regenerate Go + TypeScript protobuf code (bash)
│   └── generate-proto.ps1          # Same, for Windows PowerShell
└── internal/
    ├── auth/
    │   ├── jwt.go                  # JWT token generation and validation
    │   ├── google.go               # Google OAuth provider
    │   ├── local.go                # Local username + password provider
    │   ├── local_test.go
    │   └── user.go                 # Provider-independent endpoints (/auth/user, /auth/providers)
    ├── config/
    │   └── config.go               # Environment config + all gameplay tuning constants
    ├── db/
    │   ├── store.go                # Repository interfaces + backend selection
    │   ├── mongo.go                # MongoDB backend
    │   ├── sqlite.go               # Embedded SQLite backend (schema migrations included)
    │   ├── models.go               # Database models (User, GameSession, LeaderboardEntry)
    │   └── store_test.go           # Contract tests run against every backend
    ├── game/
    │   ├── engine.go               # Game loop, physics, AI and combat
    │   ├── session.go              # Engine ↔ database state conversion
    │   └── session_test.go
    ├── handlers/
    │   ├── sessions.go             # REST: create, list and join game sessions
    │   └── leaderboard.go          # REST: global leaderboard
    ├── server/
    │   ├── server.go               # Game server: sessions, client registry, broadcasting, saving
    │   └── websocket.go            # Per-client WebSocket read/write pumps, JSON + binary decoding
    ├── types/
    │   ├── types.go                # Game state, input payload, inventory types
    │   ├── player.go               # Player entity
    │   ├── enemy.go                # Enemy entity (soldier, lieutenant, sniper, tower)
    │   ├── bullet.go               # Bullets and projectiles
    │   ├── ray.go                  # Hitscan rays (railgun, sniper)
    │   ├── bonus.go                # Pick-ups (aid kits, goggles, chests, drops)
    │   ├── shop.go                 # Shops and their inventory
    │   ├── wall.go                 # Walls
    │   ├── screenobject.go         # Common base for positioned objects
    │   └── vector2.go              # 2D vector math
    ├── utils/
    │   ├── math.go                 # Collision and geometry helpers, chunk coordinates
    │   └── math_test.go
    └── protocol/
        ├── messages.proto          # Protocol Buffers schema (source of truth for the wire format)
        ├── messages.pb.go          # Generated Go code — do not edit
        ├── messages.ts             # Generated TypeScript code — do not edit
        └── converters.go           # Conversions between game types and protobuf messages
```

## Prerequisites

- Go 1.24 or higher
- Node.js + `protoc` — only to regenerate protobuf code (`npm run proto:generate`)
- MongoDB (Atlas or local instance) — only when `DB_DRIVER=mongodb`; otherwise the server uses an embedded SQLite file and needs no database setup
- Google OAuth credentials ([Get them here](https://console.cloud.google.com/)) — only when `GOOGLE_AUTH_ENABLED=true`; local username/password accounts need no external setup
- Git (for dependency management)

## Installation

1. Clone or navigate to the project directory:

```bash
cd ~/dungerra-api
```

2. Install dependencies:

```bash
go mod download
```

3. Set up environment variables:

```bash
cp .env.example .env
```

Edit `.env` with your configuration:

```bash
# Database backend: "sqlite" (embedded) or "mongodb" (hosted).
# If unset: mongodb when MONGODB_URL is set, otherwise sqlite.
DB_DRIVER=sqlite
SQLITE_PATH=data/dungerra.db          # used when DB_DRIVER=sqlite
MONGODB_URL=mongodb+srv://username:password@cluster.mongodb.net/   # used when DB_DRIVER=mongodb

# JWT Secret (generate with: openssl rand -base64 64)
SECRET_KEY=your-secret-key-here

# Authentication providers (at least one must be enabled)
# GOOGLE_AUTH_ENABLED defaults to true when both Google credentials are set.
# LOCAL_AUTH_ENABLED (username + password accounts) defaults to true only when Google is disabled.
GOOGLE_AUTH_ENABLED=true
LOCAL_AUTH_ENABLED=false

# Google OAuth Credentials (required when GOOGLE_AUTH_ENABLED=true)
GOOGLE_CLIENT_ID=your-client-id.apps.googleusercontent.com
GOOGLE_CLIENT_SECRET=your-client-secret

# API Configuration
API_BASE_URL=http://localhost:8080
FRONTEND_URL=http://localhost:3000

# JWT Token Expiration (in minutes, default 8 days)
ACCESS_TOKEN_EXPIRE_MINUTES=11520

# Server Configuration
PORT=8080
USE_TLS=false
```

### Setting up Google OAuth

1. Go to [Google Cloud Console](https://console.cloud.google.com/)
2. Create a new project or select existing
3. Enable Google+ API
4. Go to "Credentials" → "Create Credentials" → "OAuth 2.0 Client ID"
5. Set application type to "Web application"
6. Add authorized redirect URIs:
   - `http://localhost:8080/api/v1/auth/google/callback` (development)
   - `https://yourdomain.com/api/v1/auth/google/callback` (production)
7. Copy Client ID and Client Secret to your `.env` file

## Running the Server

Start the server:

```bash
go run main.go
```

The server will start on `http://localhost:8080`

## API Endpoints

### Authentication

Accounts belong to exactly one provider and are never merged across providers. Local accounts are shown to other players with an `@local` suffix (e.g. `alice@local`) so they can't be mistaken for Google users.

- **Enabled Providers**: `GET /api/v1/auth/providers`
  - Returns: `{ "google": true, "local": false }` — use it to decide which sign-in options to show

#### Google (when `GOOGLE_AUTH_ENABLED`)

- **Get Google Auth URL**: `GET /api/v1/auth/google/url`
  - Returns: `{ "url": "https://accounts.google.com/...", "state": "..." }`
  - Use this URL to redirect users to Google login
- **Google OAuth Callback**: `GET /api/v1/auth/google/callback?code=xxx&state=xxx`
  - Handles the OAuth callback from Google
  - Creates/finds user in database
  - Returns JWT token by redirecting to: `{FRONTEND_URL}?token={jwt}`

#### Local accounts (when `LOCAL_AUTH_ENABLED`)

- **Register**: `POST /api/v1/auth/local/register` with `{ "username": "alice", "password": "..." }`
  - Username: 3–24 characters, letters/digits/`_`/`-`/`.`, unique case-insensitively
  - Password: 8–72 characters (stored as a bcrypt hash)
  - Returns `201 { "token": "<jwt>", "user": { ... } }`; `409` if the username is taken, `400` if invalid
- **Login**: `POST /api/v1/auth/local/login` with `{ "username": "alice", "password": "..." }`
  - Returns `200 { "token": "<jwt>", "user": { ... } }`; `401` on wrong credentials

#### Any provider

- **Get Current User**: `GET /api/v1/auth/user`
  - Headers: `Authorization: Bearer {jwt}`
  - Returns current authenticated user information
  - Response:
    ```json
    {
      "id": "507f1f77bcf86cd799439011",
      "provider": "google",
      "email": "user@example.com",
      "google_id": "1234567890",
      "username": "user",
      "display_name": "user",
      "is_active": true,
      "created_at": "2025-01-01T00:00:00Z",
      "current_session": "507f1f77bcf86cd799439012"
    }
    ```

### WebSocket Connection

**Session-Based Multiplayer**: Each game session has its own isolated game state, allowing multiple independent games to run simultaneously.

- **WebSocket (JSON)**: `ws://localhost:8080/ws?token={jwt}&sessionId={sessionId}` - Game connection with JSON protocol
- **WebSocket (Binary)**: `ws://localhost:8080/ws?token={jwt}&sessionId={sessionId}&protocol=binary` - Game connection with Protocol Buffers

**Authentication**: Include JWT token in query parameter:

```javascript
const token = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...";
const sessionId = "507f1f77bcf86cd799439011"; // Optional - auto-created if not provided
const ws = new WebSocket(
  `ws://localhost:8080/ws?token=${token}&sessionId=${sessionId}`
);
```

**Session Behavior**:

- If no `sessionId` is provided, a new session is automatically created
- When the first player joins a session, game state is loaded from the database (if it exists)
- When the last player leaves a session, game state is saved to the database and cleared from memory
- Each session has its own independent chunk generation, enemies, bonuses, and game world
- Multiple sessions can run simultaneously without interfering with each other

### Health Check

- **Health Check**: `GET /health` - Server health status

For details on binary protocol usage, see [Binary Protocol Documentation](docs/development/binary-protocol.md).

## Client-Server Protocol

### Client → Server Messages

#### Connect

```json
{
  "type": "connect",
  "payload": {
    "username": "PlayerName"
  }
}
```

#### Player Input

```json
{
  "type": "input",
  "payload": {
    "forward": true,
    "backward": false,
    "left": false,
    "right": true,
    "aimRotation": 225.0
  }
}
```

- `forward` / `backward`: move along / against the current facing. `left` / `right`: strafe to the player's own left / right. Diagonals move at normal speed.
- `aimRotation` (optional): desired facing in degrees (0 = +Y, `atan2(-dx, dy)` for a target offset `dx, dy`). The server turns toward it at a capped rate. If omitted, the last aim is kept.

#### Shoot

```json
{
  "type": "shoot",
  "payload": {
    "direction": 0.785398
  }
}
```

### Server → Client Messages

#### Game State (Broadcast every frame)

```json
{
  "type": "gameState",
  "payload": {
    "players": {
      "player-id": {
        "id": "uuid",
        "username": "PlayerName",
        "position": { "x": 100, "y": 200 },
        "velocity": { "x": 0, "y": 0 },
        "lives": 5,
        "score": 3,
        "money": 150.0,
        "kills": 15,
        "rotation": 90.0,
        "bulletsLeft": 4,
        "nightVisionTimer": 0,
        "isAlive": true
      }
    },
    "bullets": {
      "bullet-id": {
        "id": "uuid",
        "position": { "x": 150, "y": 220 },
        "velocity": { "x": 420, "y": 0 },
        "ownerId": "player-id",
        "damage": 1
      }
    },
    "walls": {
      "wall-id": {
        "id": "uuid",
        "position": { "x": 500, "y": 500 },
        "width": 30,
        "height": 250,
        "orientation": "vertical"
      }
    },
    "enemies": {
      "enemy-id": {
        "id": "uuid",
        "position": { "x": 520, "y": 600 },
        "rotation": 90.0,
        "lives": 1,
        "wallId": "wall-id",
        "isDead": false
      }
    },
    "bonuses": {
      "bonus-id": {
        "id": "uuid",
        "position": { "x": 300, "y": 400 },
        "type": "aid_kit"
      }
    },
    "timestamp": 1234567890
  }
}
```

#### Player Join

```json
{
  "type": "playerJoin",
  "payload": {
    "player": {
      /* player object */
    }
  }
}
```

#### Player Leave

```json
{
  "type": "playerLeave",
  "payload": {
    "playerId": "uuid"
  }
}
```

## Game Configuration

All gameplay tuning constants are in the `const` block of `internal/config/config.go`, grouped by area:

- **Player**: lives, speed, size, rotation speed, invulnerability times, kill reward, inventory drop lifetime
- **Weapons**: blaster, shotgun, rocket launcher and railgun sections
- **Enemies**: shared enemy values, plus soldier, lieutenant, sniper and tower sections (lives, range, reward, drop chances)
- **Bonuses**: aid kit healing, night-vision duration, pick-up sizes
- **World and vision**: chunk size, sight radius, wall density, torch radius
- **Session**: save interval and game loop rate (`GameLoopInterval`, 30 updates per second)
- **Shop and tower loot**: stock probabilities and quantities

The values change with balancing, so check the file itself rather than copying them elsewhere. If you change a value that players can see, also update the matching page in [`docs/user/`](docs/user/README.md).

## Building for Production

Build an executable:

```bash
go build -o game-server main.go
```

Run the compiled binary:

```bash
./game-server
```

## Testing

```bash
go test ./...
```

The tests need no external services. The storage contract tests in `internal/db/store_test.go` always run against SQLite. To run them against MongoDB too, set `MONGODB_TEST_URL`.

To check the binary protocol by hand, connect a client with `protocol=binary` (see [Binary Protocol Documentation](docs/development/binary-protocol.md)).

## Development

### Adding New Features

1. **Game Logic**: Modify `internal/game/engine.go`
2. **Gameplay tuning**: Adjust constants in `internal/config/config.go`
3. **Entities**: Add or change behaviour in `internal/types/`
4. **Network Protocol**: Edit `internal/protocol/messages.proto`, run `npm run proto:generate` (`npm run proto:generate:win` on Windows), then update `internal/protocol/converters.go`
5. **Server Behavior**: Adjust `internal/server/server.go` (sessions, broadcasting) or `internal/server/websocket.go` (per-client I/O)
6. **REST endpoints**: Add handlers in `internal/handlers/` and register them in `main.go`
7. **Storage**: Extend the interfaces in `internal/db/store.go` and implement them in both `sqlite.go` and `mongo.go`

## Client Implementation Example

Here's a basic JavaScript client example:

```javascript
const ws = new WebSocket("ws://localhost:8080/ws");

ws.onopen = () => {
  // Connect with username
  ws.send(
    JSON.stringify({
      type: "connect",
      payload: { username: "MyPlayer" },
    })
  );
};

ws.onmessage = (event) => {
  const message = JSON.parse(event.data);

  if (message.type === "gameState") {
    // Update game rendering with message.payload
    updateGame(message.payload);
  }
};

// Send player input
// aimDegrees: angle from the player to the cursor, atan2(-dx, dy) in degrees
function sendInput(keys, aimDegrees) {
  ws.send(
    JSON.stringify({
      type: "input",
      payload: {
        forward: keys.w,
        backward: keys.s,
        left: keys.a,
        right: keys.d,
        aimRotation: aimDegrees,
      },
    })
  );
}

// Shoot
function shoot(angle) {
  ws.send(
    JSON.stringify({
      type: "shoot",
      payload: { direction: angle },
    })
  );
}
```

## Architecture Notes

### Authoritative Server

- All game state is maintained on the server
- Client inputs are processed server-side
- Prevents client-side cheating
- Server validates all actions (movement, shooting, collisions)

### Concurrency

- Each client connection runs in its own goroutine
- Game engine uses mutex locks for thread-safe state access
- Broadcast messages are queued and sent asynchronously

### Performance

- 30 FPS game loop (33ms tick rate)
- Efficient collision detection with spatial checks
- Delta-time based physics for consistent movement

## Future Enhancements

- [ ] Client-side prediction and interpolation
- [x] Lag compensation
- [x] Multiple game rooms/lobbies
- [ ] Spectator mode
- [x] Power-ups and different weapon types
- [x] Respawn system
- [x] Persistent player stats
- [ ] Anti-cheat measures
- [ ] Match-making system

## License

MIT

## Contributing

Feel free to submit issues and pull requests!
