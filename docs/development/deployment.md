# Production Deployment Guide

## Running on Port 443 (HTTPS)

### Requirements

1. **TLS Certificate** - You need a valid SSL/TLS certificate
2. **Root/Sudo Access** - Port 443 requires elevated privileges
3. **Domain Name** - For Let's Encrypt certificates

### Method 1: Direct with TLS (Recommended for Production)

#### Using Let's Encrypt Certificate

```bash
# Generate certificate with certbot (one-time setup)
sudo certbot certonly --standalone -d yourdomain.com

# Run server on port 443
sudo ./dungerra-api \
  -host 0.0.0.0 \
  -port 443 \
  -tls \
  -cert /etc/letsencrypt/live/yourdomain.com/fullchain.pem \
  -key /etc/letsencrypt/live/yourdomain.com/privkey.pem
```

#### Using Environment Variables

```bash
# Set environment variables
export HOST=0.0.0.0
export PORT=443
export USE_TLS=true
export TLS_CERT=/path/to/cert.pem
export TLS_KEY=/path/to/key.pem

# Run with sudo
sudo -E ./dungerra-api
```

### Method 2: Using Reverse Proxy (Recommended for Flexibility)

Run your game server on a non-privileged port and use a reverse proxy:

#### Nginx Configuration

```nginx
upstream gameserver {
    server localhost:8080;
}

server {
    listen 443 ssl http2;
    server_name yourdomain.com;

    ssl_certificate /etc/letsencrypt/live/yourdomain.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/yourdomain.com/privkey.pem;

    location /ws {
        proxy_pass http://gameserver;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # WebSocket timeouts
        proxy_read_timeout 86400;
        proxy_send_timeout 86400;
    }

    location /health {
        proxy_pass http://gameserver;
    }
}
```

Start your game server normally:

```bash
./dungerra-api -port 8080
```

#### Caddy Configuration (Automatic HTTPS)

```caddy
yourdomain.com {
    reverse_proxy /ws localhost:8080
    reverse_proxy /health localhost:8080
}
```

Caddy automatically handles HTTPS certificates!

### Method 3: Docker with Port Mapping

```dockerfile
FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o game-server

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/game-server .
COPY --from=builder /app/certs /certs

EXPOSE 443

CMD ["./game-server", "-port", "443", "-tls", "-cert", "/certs/cert.pem", "-key", "/certs/key.pem"]
```

Run with:

```bash
docker build -t game-server .
docker run -d -p 443:443 -v /etc/letsencrypt:/certs:ro game-server
```

### Method 4: Using systemd with Capabilities

Allow the binary to bind to privileged ports without root:

```bash
# Build the binary
go build -o /usr/local/bin/dungerra-api

# Set capabilities
sudo setcap 'cap_net_bind_service=+ep' /usr/local/bin/dungerra-api
```

Create systemd service `/etc/systemd/system/dungeon-game.service`:

```ini
[Unit]
Description=Dungeon Game Server
After=network.target

[Service]
Type=simple
User=gameserver
WorkingDirectory=/opt/dungeon-game
Environment="HOST=0.0.0.0"
Environment="PORT=443"
Environment="USE_TLS=true"
Environment="TLS_CERT=/etc/letsencrypt/live/yourdomain.com/fullchain.pem"
Environment="TLS_KEY=/etc/letsencrypt/live/yourdomain.com/privkey.pem"
ExecStart=/usr/local/bin/dungerra-api
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
```

Enable and start:

```bash
sudo systemctl enable dungeon-game
sudo systemctl start dungeon-game
sudo systemctl status dungeon-game
```

## Command Line Options

```bash
./dungerra-api [options]

Options:
  -port string
        Port to listen on (default "8080")
        Can also set via PORT environment variable

  -tls
        Enable TLS/HTTPS
        Can also set via USE_TLS=true environment variable

  -cert string
        TLS certificate file path
        Can also set via TLS_CERT environment variable
        Required when TLS is enabled

  -key string
        TLS private key file path
        Can also set via TLS_KEY environment variable
        Required when TLS is enabled

  -ready-line
        Print "DUNGERRA_READY {"addr":"<host>:<port>"}" to stdout once listening
        (desktop mode; with -port 0 it reports the port that was picked)

  -shutdown-on-stdin-close
        Save sessions and stop when stdin is closed (desktop mode; works on Windows,
        where a parent process cannot send SIGINT)

  -no-dotenv
        Do not read a .env file from the working directory
```

A flag given on the command line overrides the matching environment variable, including one set in `.env`. `-host` (env `HOST`, default `localhost`) sets the listen address. `-port 0` binds any free port. Logs go to stderr, so with `-ready-line` the ready line is the only thing written to stdout.

## Environment Variables

```bash
HOST=0.0.0.0               # Host to listen on
PORT=443                   # Port to listen on
USE_TLS=true               # Enable HTTPS
TLS_CERT=/path/cert.pem    # Certificate file
TLS_KEY=/path/key.pem      # Private key file

DB_DRIVER=mongodb          # "mongodb" (hosted) or "sqlite" (single server / desktop)
MONGODB_URL=mongodb+srv://...
SQLITE_PATH=/var/lib/dungerra/dungerra.db

GOOGLE_AUTH_ENABLED=true   # Google sign-in (needs GOOGLE_CLIENT_ID / GOOGLE_CLIENT_SECRET)
LOCAL_AUTH_ENABLED=false   # Username + password accounts

DATA_DIR=                  # Desktop mode: per-user data directory (see below)
STATIC_DIR=                # Serve the built web client from this folder at "/"
```

### Choosing a database and sign-in providers

- **Hosted / multiple instances:** `DB_DRIVER=mongodb`. An SQLite file can only be used by one server process.
- **Single server or desktop bundle:** `DB_DRIVER=sqlite` needs no database service. Put `SQLITE_PATH` on persistent storage (a volume in Docker) and back the file up.
- **Sign-in:** at least one of `GOOGLE_AUTH_ENABLED` / `LOCAL_AUTH_ENABLED` must be on. Local accounts have no built-in rate limiting; on a public server, rate-limit `/api/v1/auth/local/*` at the reverse proxy.

When upgrading an existing MongoDB deployment, the first start migrates users to the provider-based account model (see [database.md](database.md)). Back up the `users` collection first, and make sure the database user may drop indexes.

See [database.md](database.md) and [authentication.md](authentication.md) for details.

### Desktop mode (solo and LAN)

The desktop app runs this server as a child process. The design is in [docs/decisions/desktop-version.md](../decisions/desktop-version.md). These settings are opt-in, and the server behaves as before without them:

- **`DATA_DIR`** is the absolute per-user data directory. It is created with owner-only permissions if missing.
  - When `SQLITE_PATH` is unset, the database is `<DATA_DIR>/dungerra.db`.
  - When `SECRET_KEY` is unset, a random key is generated once and kept in `<DATA_DIR>/secret.key`, so sign-ins survive restarts. Deleting that file logs everyone out.
  - An empty `secret.key` is an error and is not replaced.
- **`STATIC_DIR`** serves the built web client at `/`, so the client and the API share one origin (no CORS).
  - Paths that are not files get `index.html`, so client routes such as `/<sessionId>` work.
  - `/api/*`, `/ws` and `/health` take precedence. An unknown `/api/*` path is a 404.
  - The folder must contain `index.html`.
- **`GET /api/v1/version`** returns `{"game_version": "…"}` without authentication. The launcher uses it to probe a LAN host.

A typical desktop start (solo; use `-host 0.0.0.0 -port 8080` to host for the LAN):

```bash
DATA_DIR="$HOME/Library/Application Support/Dungerra" \
STATIC_DIR=/path/to/bundled/web \
LOCAL_AUTH_ENABLED=true GOOGLE_AUTH_ENABLED=false \
./dungerra-api -host 127.0.0.1 -port 0 -ready-line -shutdown-on-stdin-close -no-dotenv
```

When hosting, the ready line reports `0.0.0.0:<port>`. The host's own window should use `127.0.0.1:<port>`.

## Examples

### Development (HTTP on 8080)

```bash
./dungerra-api
```

### Production (HTTPS on 443)

```bash
sudo ./dungerra-api -host 0.0.0.0 -port 443 -tls \
  -cert /etc/certs/cert.pem \
  -key /etc/certs/key.pem
```

### Behind Load Balancer (HTTP on 8080)

```bash
# Let the load balancer handle HTTPS
./dungerra-api -port 8080
```

### Custom Port with TLS

```bash
./dungerra-api -port 8443 -tls \
  -cert ./certs/cert.pem \
  -key ./certs/key.pem
```

## Security Best Practices

1. **Use TLS in Production** - Always encrypt WebSocket traffic in production
2. **Keep Certificates Updated** - Automate renewal with certbot
3. **Run as Non-Root** - Use reverse proxy or capabilities instead of running as root
4. **Firewall Rules** - Only expose necessary ports
5. **Rate Limiting** - Use nginx/caddy for rate limiting
6. **Health Checks** - Monitor `/health` endpoint

## Testing HTTPS Locally

Generate self-signed certificate for testing:

```bash
openssl req -x509 -newkey rsa:4096 -keyout key.pem -out cert.pem -days 365 -nodes -subj "/CN=localhost"

./dungerra-api -port 8443 -tls -cert cert.pem -key key.pem
```

Connect with (ignore self-signed warning in development):

```javascript
const ws = new WebSocket("wss://localhost:8443/ws");
```

## Load Balancing Multiple Instances

For high availability, run multiple instances:

```nginx
upstream gameservers {
    least_conn;  # Use least connections for game servers
    server 10.0.1.10:8080;
    server 10.0.1.11:8080;
    server 10.0.1.12:8080;
}

server {
    listen 443 ssl;
    server_name game.yourdomain.com;

    location /ws {
        proxy_pass http://gameservers;
        # ... WebSocket config ...
    }
}
```

**Note**: Each server instance maintains its own game state. For shared state, you'd need to implement state synchronization or session affinity. Multiple instances also require `DB_DRIVER=mongodb`, because an SQLite database cannot be shared between processes.

## Monitoring

```bash
# Check if server is running
curl https://yourdomain.com/health

# View logs with systemd
sudo journalctl -u dungeon-game -f

# Check connections
sudo netstat -tulpn | grep :443
```

## Troubleshooting

### "Permission denied" on port 443

- Use sudo, or
- Set capabilities, or
- Use reverse proxy on port 443

### "Address already in use"

```bash
# Check what's using the port
sudo lsof -i :443
```

### Certificate Issues

```bash
# Verify certificate
openssl x509 -in cert.pem -text -noout

# Test TLS connection
openssl s_client -connect yourdomain.com:443
```
