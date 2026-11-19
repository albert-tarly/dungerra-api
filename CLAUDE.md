# CLAUDE.md

Authoritative multiplayer top-down shooter server in Go (module `github.com/albert-tarly/dungerra-api`, Go 1.24). It uses WebSocket for game traffic (JSON or protobuf) and REST for auth, sessions and the leaderboard, with pluggable storage (SQLite or MongoDB). The README has the feature overview. This file covers how to work in the repo.

## Commands

```sh
go build ./...                 # compile everything
go vet ./...                   # must stay clean
gofmt -l . | grep -v node_modules   # must print nothing
go test ./...                  # unit tests (no external services needed)
go test ./internal/db -run TestStoreContract -v   # storage contract, SQLite always; Mongo only if configured
go run main.go                 # start server on :8080 (reads .env)
npm run proto:generate:win     # regenerate protobuf (Go + TS) on Windows; proto:generate on bash
```

Dev machine is Windows: the shell may be PowerShell or Git Bash. Use the syntax for whichever tool you are calling.

## Layout (what lives where)

- `main.go`: wiring, HTTP routes, CORS, feature-switched auth registration.
- `internal/config/config.go`: env loading (`LoadConfig` → global `config.AppConfig`) **and all gameplay tuning constants** (speeds, ranges, rewards, drop chances).
- `internal/game/engine.go`: the game loop, physics, AI, combat (~2.2k lines, one `sync.RWMutex` `e.mu`). `session.go` handles engine ↔ DB conversion.
- `internal/types/`: entities (player, enemy, bullet, shop, wall, bonus…) and their per-entity behaviour.
- `internal/server/`: WebSocket handling and per-client state broadcasting.
- `internal/db/`: `store.go` holds repository interfaces + backend selection. `sqlite.go` (pure Go, `modernc.org/sqlite`, schema migrations inline) and `mongo.go` implement them.
- `internal/auth/`: JWT, Google OAuth, local username/password, provider-independent endpoints.
- `internal/protocol/`: `messages.proto` is the source of truth. `messages.pb.go` and `messages.ts` are **generated**. `converters.go` is hand-written.
- `docs/development/`: developer docs (API, auth, DB, binary protocol, deployment).
- `docs/user/`: player-facing guide.
- `dist/`, `node_modules/`, `data/`: build output, deps, local SQLite DB. Don't edit `dist/` or `node_modules/`, and don't commit `data/`.

## Rules and invariants

1. **Server is authoritative.** Never trust client-sent positions, damage, money or inventory. Clients send inputs only; all outcomes are computed in the engine.
2. **Engine concurrency.** All engine state is guarded by `e.mu`. Read paths take `RLock`, mutations take `Lock`. Never call DB/network I/O or another lock-taking method while holding `e.mu`. Persistence copies state under `RLock` (`SaveToSession`) and writes to the DB after the lock is released.
3. **Gameplay numbers go in `config.go` constants**, not inline literals, with a unit/meaning comment (see existing `EnemySniper*` block).
4. **Keep player docs in sync.** Any change to a player-visible number or behaviour (ranges, damage, prices, drop rates, cooldowns, enemy behaviour) must update the matching page in `docs/user/` in the same change. Grep `docs/user` for the old value.
5. **Storage parity.** Any repository change must be implemented in *both* `sqlite.go` and `mongo.go`, return `db.ErrNotFound` / `db.ErrDuplicate` (not driver errors), and be covered by `TestStoreContract`. SQLite stores world objects as JSON (numbers decode as `float64`; Mongo/BSON gives `int32`), so conversion code must accept both.
6. **SQLite schema changes** go through the migration list in `sqlite.go`. Never edit an already-shipped migration; append a new one.
7. **Protocol changes:** edit `messages.proto`, regenerate, update `converters.go`, and keep the JSON protocol (message structs in `internal/types/types.go`, README protocol section) consistent. Only add fields with new field numbers; never renumber or reuse them. Never hand-edit generated files.
8. **Auth is feature-switched** (`GOOGLE_AUTH_ENABLED`, `LOCAL_AUTH_ENABLED`; at least one on). New auth code must not assume a particular provider is enabled.
9. **Secrets:** `.env` is local and gitignored. Never read it into output, commit it, or paste values from it. Add new variables to `.env.example` and to the config docs.
10. **Tests that build an `Engine`** must set `config.AppConfig` first and restore it with `t.Cleanup` (see `internal/game/session_test.go`). There is global state.

## How I (Claude) should work here

- **Read before writing.** `engine.go` is large: grep for the relevant function and read the surrounding block plus its lock usage before editing. Don't rewrite or reformat regions you aren't changing.
- **Small, reviewable diffs.** One concern per change. No drive-by refactors, renames, or dependency bumps unless asked. If I spot an unrelated bug, report it instead of fixing it silently.
- **Verify, don't assert.** After a Go change, run `go build ./... && go vet ./... && go test ./...` and report the actual result. If something can't be verified (e.g. Mongo-only path, gameplay feel), say so explicitly.
- **Add a test when fixing a bug** whenever the logic is reachable without a live socket (math in `internal/utils`, storage contract, session round-trips, auth handlers via `httptest`).
- **Gameplay/balance changes:** state the before → after values and the player-visible effect in the summary, and update `docs/user/` (rule 4). Consider whether `CHANGELOG.md` needs an entry (Keep a Changelog format, semver). Ask the user if unsure.
- **Ask before** changing the wire protocol, DB schema, auth flow, or anything that breaks existing clients or saved worlds. Those are hard to reverse.
- **Git:** work on a feature branch, never commit to `main` directly, and commit or push only when asked. Commit messages are short and imperative; the history mixes plain and `feat(scope):` styles, so either is fine.
- **Docs:** user docs are written for players (actions, not keys; plain numbers in game units/seconds). Developer docs go in `docs/development/`. Don't create new markdown files unless asked.
- **When uncertain about intent** (e.g. is a number a bug or a design choice?), check git log/blame and `docs/user/` first, then ask instead of guessing.
