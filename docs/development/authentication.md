# Authentication System

This document describes the authentication and authorization system for the multiplayer game server.

## Overview

The server supports two sign-in **providers**, each behind a feature switch:

| Provider | Switch | Default | Identity |
|----------|--------|---------|----------|
| Google OAuth 2.0 | `GOOGLE_AUTH_ENABLED` | on when `GOOGLE_CLIENT_ID` and `GOOGLE_CLIENT_SECRET` are set | Google account ID |
| Local username + password | `LOCAL_AUTH_ENABLED` | on only when Google is disabled | lower-cased username |

At least one provider must be enabled, or the server refuses to start. Routes of a disabled provider are not registered (they return `404`). Clients discover what to offer with `GET /api/v1/auth/providers` → `{ "google": bool, "local": bool }`.

Whatever the provider, a successful sign-in yields a **JWT**, which authenticates every other request and the WebSocket connection. User data is persisted in the configured database (embedded SQLite or MongoDB, see [database.md](database.md)).

### Accounts are separated by provider

Every account belongs to exactly one provider and is identified by the pair `(provider, provider_id)`, which is unique. Accounts are **never merged** across providers: a Google user `alice` and a local user `alice` are two different accounts with separate progress and leaderboard entries.

To keep them distinguishable for other players, local accounts are displayed with an `@local` suffix (`alice@local`) everywhere names are shown: in-game, on the leaderboard, and as session host. The stored `username` stays `alice`; the API exposes the shown name as `display_name`.

### Local accounts

- `POST /api/v1/auth/local/register` — `{ "username", "password" }` → `201 { token, user }`
- `POST /api/v1/auth/local/login` — `{ "username", "password" }` → `200 { token, user }`

Rules and behaviour:

- Usernames: 3–24 characters, letters, digits, `_`, `-`, `.`; unique case-insensitively (`Alice` and `alice` are the same account). Login is case-insensitive on the username.
- Passwords: 8–72 characters, stored only as a bcrypt hash (never returned by the API).
- Uniqueness is enforced by the database, so concurrent registrations of the same name cannot both succeed (`409 Conflict`).
- Wrong password and unknown username both return `401` with the same message and similar timing, so usernames cannot be probed through login.
- There is no rate limiting yet; put the server behind a proxy with rate limits before enabling local accounts on a public deployment.

## Google Authentication Flow

```
┌──────────┐                                    ┌──────────┐
│  Client  │                                    │  Server  │
└────┬─────┘                                    └────┬─────┘
     │                                               │
     │ 1. GET /api/v1/auth/google/url               │
     │──────────────────────────────────────────────>│
     │                                               │
     │ 2. { url: "https://accounts.google.com/..." }│
     │<──────────────────────────────────────────────│
     │                                               │
     │ 3. Redirect to Google OAuth                   │
     ├──────────────────────────┐                    │
     │                          │                    │
     │ 4. User authenticates    │                    │
     │<─────────────────────────┘                    │
     │                                               │
     │ 5. Google redirects with code                 │
     │   /api/v1/auth/google/callback?code=xxx       │
     │──────────────────────────────────────────────>│
     │                                               │
     │                          6. Exchange code for token
     │                             Verify with Google
     │                             Find/Create user (provider=google)
     │                             Generate JWT      │
     │                                               │
     │ 7. Redirect: {FRONTEND_URL}?token={jwt}       │
     │<──────────────────────────────────────────────│
     │                                               │
     │ 8. Store JWT token                            │
     │                                               │
     │ 9. Connect WebSocket with token               │
     │   ws://server/ws?token={jwt}                  │
     │──────────────────────────────────────────────>│
     │                                               │
     │                          10. Validate JWT     │
     │                              Fetch user from DB
     │                              Accept connection│
     │                                               │
     │ 11. Connected                                 │
     │<──────────────────────────────────────────────│
     │                                               │
```

## Step-by-Step Guide

### 1. Frontend: Get Google Auth URL

Make a request to get the Google OAuth URL:

```javascript
const response = await fetch('http://localhost:8080/api/v1/auth/google/url');
const { url, state } = await response.json();

// Store state for CSRF validation (optional for client)
sessionStorage.setItem('oauth_state', state);

// Redirect user to Google
window.location.href = url;
```

**Response:**
```json
{
  "url": "https://accounts.google.com/o/oauth2/v2/auth?response_type=code&client_id=...&redirect_uri=...&scope=openid+email+profile&state=xyz",
  "state": "random_csrf_token"
}
```

### 2. User Authenticates with Google

The user is redirected to Google's OAuth consent screen where they:
- Sign in to their Google account (if not already signed in)
- Grant permissions (email, profile) to your application
- Google redirects back to your callback URL

### 3. Server: Handle OAuth Callback

Google redirects to: `http://localhost:8080/api/v1/auth/google/callback?code=xxx&state=xxx`

The server:
1. Exchanges the authorization code for an access token
2. Verifies the token with Google
3. Retrieves user information (Google ID, email, name)
4. Finds or creates user in MongoDB
5. Generates a JWT token containing the user ID
6. Redirects to frontend with the JWT token

**Redirect:** `http://localhost:3000?token=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...`

### 4. Frontend: Store JWT Token

Extract and store the JWT token:

```javascript
// Parse token from URL
const urlParams = new URLSearchParams(window.location.search);
const token = urlParams.get('token');

if (token) {
  // Store token
  localStorage.setItem('jwt_token', token);
  
  // Clean URL
  window.history.replaceState({}, document.title, window.location.pathname);
  
  // Now ready to connect to WebSocket
  connectToGame(token);
}
```

### 5. Connect to WebSocket with Authentication

Include the JWT token in the WebSocket connection:

```javascript
function connectToGame(token) {
  const ws = new WebSocket(`ws://localhost:8080/ws?token=${token}`);
  
  ws.onopen = () => {
    console.log('Connected to game server');
    
    // Send connect message (username not needed, comes from JWT)
    ws.send(JSON.stringify({
      type: 'connect',
      payload: {}
    }));
  };
  
  ws.onmessage = (event) => {
    const message = JSON.parse(event.data);
    console.log('Received:', message);
    
    // Handle game state updates, etc.
  };
  
  ws.onerror = (error) => {
    console.error('WebSocket error:', error);
  };
  
  ws.onclose = () => {
    console.log('Disconnected from game server');
  };
}
```

### 6. Server: Validate JWT on WebSocket Connection

When a client connects via WebSocket, the server:

1. Extracts the JWT token from the query parameter
2. Validates the token signature and expiration
3. Extracts the user ID from the token
4. Fetches the user from MongoDB
5. If valid, accepts the WebSocket connection
6. If invalid, rejects with HTTP 401 Unauthorized

**Server code excerpt:**
```go
func (gs *GameServer) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
    // Extract token
    token := r.URL.Query().Get("token")
    
    // Validate JWT
    userID, err := auth.ValidateToken(token)
    if err != nil {
        http.Error(w, "Unauthorized", 401)
        return
    }
    
    // Fetch user from database
    user, err := userRepo.FindByID(ctx, userID)
    if err != nil {
        http.Error(w, "User not found", 401)
        return
    }
    
    // Upgrade to WebSocket
    conn, _ := upgrader.Upgrade(w, r, nil)
    
    // Create authenticated client
    client := &Client{
        UserID:   user.ID,
        Username: user.Username,
        Conn:     conn,
    }
    
    // Register client and start game
    gs.register <- client
}
```

## JWT Token Details

### Token Structure

The JWT token contains:

```json
{
  "sub": "507f1f77bcf86cd799439011",  // MongoDB User ID
  "exp": 1735430400,                   // Expiration timestamp
  "iat": 1734739200                    // Issued at timestamp
}
```

### Token Lifetime

Default: **8 days** (11,520 minutes)

Configure via environment variable:
```bash
ACCESS_TOKEN_EXPIRE_MINUTES=11520
```

### Token Generation

```go
func GenerateToken(userID primitive.ObjectID) (string, error) {
    expirationTime := time.Now().Add(
        time.Duration(config.AppConfig.AccessTokenExpireMinutes) * time.Minute,
    )
    
    claims := &Claims{
        UserID: userID.Hex(),
        RegisteredClaims: jwt.RegisteredClaims{
            ExpiresAt: jwt.NewNumericDate(expirationTime),
            IssuedAt:  jwt.NewNumericDate(time.Now()),
        },
    }

    token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
    return token.SignedString([]byte(config.AppConfig.SecretKey))
}
```

### Token Validation

```go
func ValidateToken(tokenString string) (primitive.ObjectID, error) {
    claims := &Claims{}
    
    token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
        return []byte(config.AppConfig.SecretKey), nil
    })
    
    if err != nil || !token.Valid {
        return primitive.NilObjectID, errors.New("invalid token")
    }
    
    return primitive.ObjectIDFromHex(claims.UserID)
}
```

## Database Models

### User Model

```go
type User struct {
    ID             primitive.ObjectID  // Generated ID (Mongo _id / SQLite hex string)
    Provider       string              // "google" | "local"
    ProviderID     string              // Google account ID, or lower-cased local username
    Email          string              // Google accounts only
    GoogleID       string              // Google accounts only (kept for backward compatibility)
    Username       string              // Name as chosen/derived; see DisplayName()
    PasswordHash   string              // bcrypt, local accounts only; never serialized
    IsActive       bool                // Account status (inactive local accounts cannot log in)
    CreatedAt      time.Time           // Account creation time
    CurrentSession string              // Current game session ID (optional)
}

func (u *User) DisplayName() string // Username, plus "@local" for local accounts
```

Stored in the `users` collection/table. Constraints:
- `(provider, provider_id)` unique — the account identity
- `google_id` unique, sparse

Email is not unique: local accounts have none, and accounts of different providers are independent.

**MongoDB migration:** on startup the server gives pre-existing users `provider: "google"` and `provider_id = google_id`, and drops the legacy unique `email_1` index. The migration is idempotent; if it fails, the server does not start.

### Game Session Model

```go
type GameSession struct {
    ID            primitive.ObjectID      // MongoDB _id
    Name          string                  // Session name
    HostID        primitive.ObjectID      // Session host (User ID)
    Players       map[string]PlayerState  // Connected players
    MaxPlayers    int                     // Max player limit
    IsPrivate     bool                    // Private session flag
    Password      string                  // Session password (optional)
    WorldMap      map[string]Chunk        // World chunks
    SharedObjects map[string]WorldObject  // Game objects
    GameState     map[string]interface{}  // Custom game state
    CreatedAt     time.Time               // Session creation time
    LastUpdated   time.Time               // Last update time
    IsActive      bool                    // Active session flag
}
```

Stored in the `game_sessions` collection/table.

Indexes:
- `host_id`
- `is_active`

## Security Considerations

### 1. JWT Secret Key

**CRITICAL**: Use a strong, randomly generated secret key:

```bash
# Generate a secure secret key
openssl rand -base64 64
```

Add to `.env`:
```bash
SECRET_KEY=your-generated-key-here
```

**Never commit this to version control!**

### 2. HTTPS in Production

Always use HTTPS/TLS in production:

```bash
USE_TLS=true
TLS_CERT=/path/to/cert.pem
TLS_KEY=/path/to/key.pem
```

This encrypts:
- OAuth callbacks
- JWT tokens in transit
- WebSocket communication (becomes WSS)

### 3. Token Storage

**Frontend best practices:**

✅ **Recommended:**
- `sessionStorage` - Token cleared when tab closes (more secure)
- HTTP-only cookies (if using cookie-based auth)

⚠️ **Use with caution:**
- `localStorage` - Token persists across sessions
- Keep token expiration short if using localStorage

❌ **Never:**
- Store in URL parameters (except during OAuth callback)
- Store in plain text files
- Log tokens to console in production

### 4. CORS Configuration

For production, configure CORS properly:

```go
var upgrader = websocket.Upgrader{
    CheckOrigin: func(r *http.Request) bool {
        origin := r.Header.Get("Origin")
        // Only allow your frontend domain
        return origin == "https://yourdomain.com"
    },
}
```

### 5. Rate Limiting

There is no built-in rate limiting yet. Add it (e.g. at the reverse proxy) for:
- Auth endpoints, especially `/api/v1/auth/local/login` when local accounts are enabled publicly (prevent password brute force)
- WebSocket connections (prevent DoS)

## Error Handling

### Common Authentication Errors

| Error | HTTP Status | Cause | Solution |
|-------|-------------|-------|----------|
| Missing token | 401 | No token in request | Include `?token={jwt}` in WebSocket URL |
| Invalid token | 401 | Malformed or tampered token | Sign in again |
| Expired token | 401 | Token past expiration time | Sign in again |
| Invalid username or password | 401 | Local login with wrong credentials | Check credentials |
| Username is already taken | 409 | Local registration with an existing name (case-insensitive) | Choose another name |
| Validation message | 400 | Local registration with an invalid username/password | Follow the rules above |
| User not found | 401 | User deleted from database | Re-authenticate to create new account |
| Invalid signature | 401 | Wrong SECRET_KEY or tampered token | Check SECRET_KEY matches server |

### Client-Side Error Handling

```javascript
ws.onerror = (error) => {
  console.error('WebSocket error:', error);
};

ws.onclose = (event) => {
  if (event.code === 1008) { // Policy violation
    // Token invalid or expired
    console.log('Authentication failed, redirecting to login...');
    // Clear stored token
    localStorage.removeItem('jwt_token');
    // Redirect to login
    window.location.href = '/login';
  }
};
```

## Testing Authentication

### Manual Testing

1. **Test OAuth flow:**
```bash
# Get auth URL
curl http://localhost:8080/api/v1/auth/google/url

# Open the URL in a browser and complete Google login
# You'll be redirected with a token
```

   **Test local accounts** (with `LOCAL_AUTH_ENABLED=true`):
```bash
curl http://localhost:8080/api/v1/auth/providers
curl -X POST http://localhost:8080/api/v1/auth/local/register \
  -H 'Content-Type: application/json' -d '{"username":"alice","password":"correct horse"}'
curl -X POST http://localhost:8080/api/v1/auth/local/login \
  -H 'Content-Type: application/json' -d '{"username":"alice","password":"correct horse"}'
```

2. **Test WebSocket with token:**
```bash
# Use wscat or similar tool
wscat -c "ws://localhost:8080/ws?token=YOUR_JWT_TOKEN"
```

3. **Test token validation:**
```javascript
// In browser console
const token = localStorage.getItem('jwt_token');
const ws = new WebSocket(`ws://localhost:8080/ws?token=${token}`);
ws.onopen = () => console.log('Authenticated successfully!');
```

### Automated Testing

```bash
go test ./internal/auth ./internal/db
```

`internal/auth/local_test.go` covers registration, conflicts, case-insensitive login, wrong/unknown credentials and validation; `internal/db/store_test.go` covers provider identity and uniqueness on every storage backend.

## Environment Variables Reference

```bash
# Database (see database.md)
DB_DRIVER=sqlite                 # or mongodb
SQLITE_PATH=data/dungerra.db
MONGODB_URL=mongodb+srv://user:pass@cluster.mongodb.net/

# JWT Configuration
SECRET_KEY=<64-character-random-string>
ACCESS_TOKEN_EXPIRE_MINUTES=11520

# Auth providers (at least one enabled)
GOOGLE_AUTH_ENABLED=true         # default: true when Google credentials are set
LOCAL_AUTH_ENABLED=false         # default: true only when Google is disabled

# Google OAuth (required when GOOGLE_AUTH_ENABLED=true)
GOOGLE_CLIENT_ID=xxx.apps.googleusercontent.com
GOOGLE_CLIENT_SECRET=xxx

# Server URLs
API_BASE_URL=http://localhost:8080
FRONTEND_URL=http://localhost:3000

# Server Configuration
PORT=8080
HOST=localhost
USE_TLS=false
TLS_CERT=
TLS_KEY=
```

## Troubleshooting

### "OAuth redirect URI mismatch"

**Problem:** Google OAuth shows an error about redirect URI mismatch.

**Solution:** 
1. Go to [Google Cloud Console](https://console.cloud.google.com/)
2. Select your project → Credentials
3. Edit OAuth 2.0 Client ID
4. Add `http://localhost:8080/api/v1/auth/google/callback` to Authorized redirect URIs
5. Ensure `API_BASE_URL` in `.env` matches exactly

### "No authentication provider enabled" / "GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET are required"

**Problem:** The server exits at startup.

**Solution:** Enable at least one provider. `GOOGLE_AUTH_ENABLED=true` requires both Google credentials; for a setup without Google, set `GOOGLE_AUTH_ENABLED=false` (local accounts are then enabled by default).

### Google sign-in or local register/login returns 404

**Problem:** The provider is disabled, so its routes are not registered. Check `GET /api/v1/auth/providers` and the `Auth providers: google=… local=…` line in the server log.

### "Failed to open mongodb database"

**Problem:** Server can't connect to MongoDB (or the startup user migration failed).

**Solution:**
1. Verify `MONGODB_URL` is correct
2. Check MongoDB Atlas IP whitelist (allow your IP or use 0.0.0.0/0 for development)
3. Verify database user has read/write permissions (and index-management rights for the migration)
4. Test connection with MongoDB Compass

### "Invalid token" on WebSocket connection

**Problem:** WebSocket connection rejected with 401.

**Solution:**
1. Verify token is included: `ws://localhost:8080/ws?token=YOUR_TOKEN`
2. Check token hasn't expired (default: 8 days)
3. Verify `SECRET_KEY` is the same that generated the token
4. Re-authenticate to get a fresh token

### "User not found" after authentication

**Problem:** OAuth succeeds but user lookup fails.

**Solution:**
1. Check the database connection is working
2. Verify `users` collection/table exists
3. Check database indexes are created
4. Look for errors in server logs

## Next Steps

- [ ] Implement refresh tokens for extended sessions
- [ ] Rate limiting for local login/register
- [ ] Reject existing tokens of a provider after it is disabled (currently valid until expiry)
- [ ] Password change / reset for local accounts
- [ ] Support additional OAuth providers (GitHub, Discord) — add a `Provider*` constant and handler; identity model already supports it
- [ ] Implement role-based access control (admin, moderator)
- [ ] Add two-factor authentication
- [ ] Session management UI (view/revoke active sessions)
