# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Per-session **difficulty**: Peaceful, Easy, Hard, Nightmare or Custom. Sessions store the resolved settings: guards per wall by type, towers per chunk, money per kill, drop chance, wall-chest share, enemy fire rate, player lives and a derived score multiplier. Sessions created earlier play as Hard, which is unchanged.
- Wall chests: on Peaceful (25% of walls), and on Custom if enabled. Each holds 20–100 money, more the farther it is from the origin (up to ×3), plus rolled aid kits, goggles, ammo and a rare weapon.
- Custom difficulty above 100% can put two guards on one wall (on opposite sides) and two towers in one chunk.
- SQLite migration 2 adds `game_sessions.difficulty`. MongoDB stores it as an optional `difficulty` field.

### Changed

- **Breaking:** `POST /api/v1/sessions` requires `difficulty`. Session responses include the resolved `difficulty`.
- Kill score is the base reward times the session's score multiplier (Peaceful ×0.25, Easy ×0.45, Hard ×1, Nightmare ×1.2). Kill money is scaled separately by `money_pct`, which also covers kills of other players.

## [2.0.0] - 2026-11-20

### Added

- `InputMessage.aim_rotation` (field 8): desired facing in degrees, usually from the mouse cursor. The server turns the player toward it at up to 720°/s.

### Changed

- Aid kits picked up while below full health (6 lives) are used immediately, one at a time until full; only the rest go into the inventory. Previously every picked-up kit was stored. Shop purchases are still stored.
- **Breaking:** the server no longer sends per-tick bullet positions (`GameStateDeltaMessage.updated_bullets` stays empty). Clients must move bullets themselves from the `position` and `velocity` in `added_bullets`; `removed_bullets` still carries the full bullet. Cuts delta size about 3× in firefights.
- Deltas are only treated as delivered once queued for the client. A delta dropped because the client's send buffer was full is folded into the next one, instead of permanently desyncing the client.
- Joining and leaving a session no longer does DB I/O on the game loop, which used to stall every session. The last player leaving saves the session in the background; rejoining before that finishes reuses the in-memory world.
- Spent bullets are kept for 1 s instead of 5 s (enough for the rocket explosion effect), and per-player view snapshots share walls instead of copying them: roughly 60% less allocation per tick.
- Faster collision checks: segment tests reject far-away walls by bounding box first, and rocket explosions only check enemies in nearby chunks.
- **Breaking:** `left`/`right` now strafe sideways relative to the facing instead of rotating. `forward`/`backward` still move along the facing. Diagonal movement is normalised to the same speed.
- `PlayerRotationSpeed` 180°/s → 720°/s (max turn rate toward the aim).

### Removed

- Keyboard rotation: `left`/`right` no longer turn the player.

## [1.1.1] - 2025-12-26

### Changed

- Inventory drop expires after 5 minutes
- Inventory chest bonus is exposed to its owner

## [1.1.0] - 2025-12-26

### Added

- Shops have names

### Changed

- After respawn, the player appears on a random neighbor chunk

## [1.0.0] - 2025-12-25

### Added

- Shopping mechanics
- Enhanced player inventory system

### Changed

- Improved game balance and difficulty scaling
- Updated user interface design

## [0.0.1-alpha2] - 2025-12-19

### Added

- Different weapon types
- Player inventory

### Changed

- Optimized performance for larger dungeons

### Fixed

- Resolved collision detection issues

## [0.0.1-alpha1] - 2025-12-01

### Added

- Initial release of the dungeon game
- Basic player movement and controls
- Core dungeon generation system
- Combat mechanics with enemies
- Game over and restart functionality
