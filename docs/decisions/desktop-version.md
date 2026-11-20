# Desktop version: solo and LAN play

- **Status:** accepted, in progress
- **Date:** 2026-11-20
- **Repos involved:** `dungerra-api` (this repo), `dungerra` (web client), `dungerra-desktop` (new: shell and release pipeline)

## Context

Dungerra is deployed as a web game: the client is on GitHub Pages, and this server runs on Render with MongoDB. We also want installable apps for Windows and macOS that support:

- **solo play** with no internet connection and no account on the hosted service
- **LAN play**: one player hosts on a chosen IP and port, and others join directly

The web deployment must keep working unchanged.

What the codebase already provides:

- **Storage:** SQLite through `modernc.org/sqlite` is pure Go (no cgo). The server cross-compiles to `windows/amd64`, `darwin/arm64` and `darwin/amd64`. The two darwin binaries can be merged into one universal binary. No database service is needed.
- **Auth** is feature-switched. When Google credentials are absent, local accounts are enabled automatically. The client discovers the available providers through `/api/v1/auth/providers`.
- **Engine:** one server process hosts many sessions. Engines load lazily from the DB and are saved on shutdown. Solo play is a one-player session on a local server, and LAN play is the same server reachable on the network.
- **WebSocket upgrade** accepts any origin.

Gaps:

- **Client:** the server address is a build-time constant (`API_DOMAIN` in `dungerra/src/config.ts`), and `/dungerra` is hard-coded as the router basename and webpack `publicPath`.
- **Server:**
  - `SECRET_KEY` is mandatory.
  - `SQLITE_PATH` is relative to the working directory.
  - The server cannot pick a free port and report it.
  - It shuts down gracefully only on SIGINT/SIGTERM. Windows does not deliver these to a child process, so sessions would not be saved.
  - There is no version endpoint.

## Decision

### Architecture: the server serves the client

The desktop app is a thin **Tauri v2** shell. It runs this server as a **sidecar** process, and the game window loads the client **from that server**.

```
Dungerra.app / Dungerra.exe  (Tauri v2 shell, dungerra-desktop repo)
 ├─ launcher page: [Solo] [Host LAN :port] [Join IP:port]
 ├─ sidecar: dungerra-server(.exe)   ← this repo, desktop mode
 │     serves /api/v1, /ws and the client SPA from bundled resources/web/
 └─ game window navigates to:
       Solo → http://127.0.0.1:<random port>/
       Host → http://127.0.0.1:<port>/   (server bound to 0.0.0.0:<port>)
       Join → http://<host-ip>:<port>/   (no local server needed)
```

Reasons:

- **Same origin everywhere:** no CORS, no mixed-content problems between `tauri://` and `ws://192.168…`, and the router and asset paths work unchanged.
- **No version skew over LAN:** a joining player runs the client served by the host, so client and protocol always match the host's server.
- **No install needed to join:** a LAN player can open `http://<host-ip>:<port>` in a browser.
- **The client barely knows about desktop.** It only gains a build target in which the server is the page's own origin.
- **The shell is replaceable.** If the Tauri spike fails on macOS, we can switch to Electron and keep the server and client changes.

### Alternatives considered

- **Electron:** the same Chromium engine on every OS (the same as the web version), but bundles are about 150 MB. This is the fallback if the spike fails.
- **Wails (Go-native webview, server in-process):** a separate repo cannot import this module's `internal/` packages, so it would need a package-layout refactor. Rejected.
- **Go binary that opens the system browser:** cheapest, but not an app bundle. It stays possible anyway, because desktop mode works without the shell.

### Resolved choices

| Topic | Choice |
|---|---|
| Shell | Tauri v2, if the Phase 0 spike passes. Otherwise Electron. |
| Login on desktop | Existing local accounts on whichever server you play on. The token is cached (8 days by default). No auth-flow changes. |
| Signing | Unsigned builds for now. Gatekeeper and SmartScreen will warn, and the user docs must say how to proceed. |
| Leaderboards | Each install keeps its own local leaderboard. Scores are never submitted to the hosted leaderboard, because a user-controlled server cannot be trusted. |
| Bundles | Windows x64 NSIS installer, plus one universal macOS `.dmg` (arm64 + x86_64). A single bundle for both OSes is not possible. |
| Repo layout | A new `dungerra-desktop` repo holds the shell, launcher and CI. This repo and `dungerra` get small changes, gated by flags or build targets. |

Deferred: an auth-flow change (loopback-only owner auto-login and "guest by name" for LAN joiners). Revisit only if the one-time local sign-up proves annoying.

## Sidecar contract (dungerra-api desktop mode)

Everything below is opt-in. Without these settings, the server behaves exactly as before.

| Setting | Meaning |
|---|---|
| `-port 0` / `PORT=0` | Bind any free port. |
| `-host 0.0.0.0` | Host mode: reachable from the LAN. Solo mode uses `127.0.0.1`. |
| `DATA_DIR` | Absolute per-user data directory. When set, the default SQLite file is `<DATA_DIR>/dungerra.db`. When `SECRET_KEY` is empty, a key is generated once and stored in `<DATA_DIR>/secret.key`. |
| `STATIC_DIR` | Serve the built client from this folder at `/`. Unknown paths fall back to `index.html` (SPA deep links). API, WebSocket and health routes take precedence. |
| `-ready-line` | After the listener is bound, print a single stdout line: `DUNGERRA_READY {"addr":"127.0.0.1:53123"}`. |
| `-shutdown-on-stdin-close` | Shut down gracefully (sessions are saved) when stdin reaches EOF, so a closing or crashed shell never loses progress on Windows. |
| `-no-dotenv` | Do not read `.env` from the working directory. |
| `GET /api/v1/version` | `{"game_version": "…"}`. The launcher probes it before joining a host. |

The shell runs the sidecar with:

- `LOCAL_AUTH_ENABLED=true` and `GOOGLE_AUTH_ENABLED=false`
- an absolute `DATA_DIR`: `%APPDATA%\Dungerra` on Windows, `~/Library/Application Support/Dungerra` on macOS
- `STATIC_DIR` set to the bundled `web/` folder

It reads stderr to show a fatal configuration error in the launcher.

## Plan

### Phase 0: spike (no commits to main repos)

Load a hand-built client and server in a bare Tauri window on Windows and macOS. Go/no-go checks:

- `.ogg` decoding through `decodeAudioData` in WKWebView
- frame rate with the three stacked canvases
- navigation from the shell to `http://192.168.x.x`
- the macOS Local Network permission prompt and the Windows Firewall prompt when hosting

If WebKit fails, switch the shell to Electron. The other phases are unaffected.

### Phase 1: dungerra-api desktop mode (this repo) — done

Implemented as specified in the sidecar contract above. Usage is documented in [deployment.md](../development/deployment.md#desktop-mode-solo-and-lan).

1. Listener: `net.Listen` + `Serve` (so `PORT=0` works), plus the ready line.
2. Graceful shutdown on stdin EOF.
3. `STATIC_DIR` SPA handler.
4. `DATA_DIR` and a persisted, generated secret key.
5. `GET /api/v1/version`.
6. `-no-dotenv`.
7. Tests (`httptest` for the SPA handler and routing, unit tests for key generation), plus docs and `.env.example`.

The wire protocol, DB schema and auth flow are unchanged.

### Phase 2: dungerra desktop build target

- In `config.ts`: `API_DOMAIN = window.location.origin` when `TARGET=desktop`. The web build is unchanged.
- The router basename and `publicPath` come from the target: `/` for desktop, `/dungerra/` for web. Desktop builds do not emit `404.html`.
- Add `npm run build:desktop` (via `cross-env`). CI also builds it.

### Phase 3: dungerra-desktop shell

- **Rust commands:**
  - `start_server(mode, port)`: spawn the sidecar, wait for `DUNGERRA_READY`, then return the URL
  - `stop_server()`: close stdin and wait
  - `lan_addresses()`
  - `probe(host, port)`: calls `/api/v1/version`
- **Launcher page:** Solo, Host (port field, shows the LAN IPs to share) and Join (IP and port, probe, then navigate). A native "Back to launcher" menu item, because remote pages get no IPC access.
- **Capabilities:** navigation to `http://*` is allowed. Remote origins get no Tauri API access.
- **Platform specifics:** `NSLocalNetworkUsageDescription` on macOS. An optional NSIS hook on Windows adds a firewall rule.

### Phase 4: CI and release (dungerra-desktop)

- **Matrix:** `windows-latest` and `macos-latest`. On macOS, build the Go binary for arm64 and amd64 and merge them with `lipo` into `dungerra-server-universal-apple-darwin`, then run `tauri build --target universal-apple-darwin`.
- **Go build:** `CGO_ENABLED=0 -trimpath -ldflags "-s -w"`. Name binaries with the target-triple suffix that Tauri expects.
- **Client:** `npm ci && npm run build:desktop`, copied to `src-tauri/resources/web`.
- **Version pinning:** check out `dungerra` at a chosen commit. Take the `dungerra-api` commit from `dungerra/package-lock.json`, so protocol types and server match.
- **Triggers:** tags `v*` produce a draft GitHub Release. `workflow_dispatch` takes SHAs.
- **Signing:** none for now. Later: Apple Developer ID plus notarization, a Windows code-signing certificate, and the Tauri updater.

### Phase 5: documentation

- Developer docs: `docs/development/deployment.md` (desktop mode, the sidecar contract).
- Player docs: a `docs/user/` page on playing offline and on LAN (hosting, joining, firewall and Gatekeeper prompts, joining from a browser).
- `CHANGELOG.md` entries in both repos.

## Risks

- **WebKit on macOS:** sound format support and canvas speed. Phase 0 decides this.
- **Hosting exposure:** a hosting machine is open to the LAN. Anyone there can register, and WebSocket origins are not checked. This is acceptable for LAN play. Session passwords could come later.
- **First-run prompts:** Windows Firewall, the macOS Local Network permission and Gatekeeper/SmartScreen (unsigned builds) will confuse some players. The user docs must cover them.
- **Silent config failures:** a `log.Fatal` in config loading ends the sidecar. The shell must capture stderr and show it to the player.
- **Version pinning:** the web client's lock file pins an older `dungerra-api` commit than `main`. The desktop pipeline must take the server commit from that lock file, not from `main`.
