# wildspark — Backend

Lightweight open-world game server match module implemented for Nakama.

A focused backend that implements an open-world match named `game`, integrates a small physics layer, and exposes a Lua-based scripting bridge for runtime map/object manipulation.

---

## Table of contents
- [wildspark — Backend](#wildspark--backend)
  - [Table of contents](#table-of-contents)
  - [Overview](#overview)
  - [Repository structure](#repository-structure)
  - [Prerequisites](#prerequisites)
  - [Build](#build)
  - [Running locally](#running-locally)
  - [Development notes](#development-notes)
  - [Script API (Lua)](#script-api-lua)
  - [OpCodes / Messages](#opcodes--messages)
  - [Testing \& debugging](#testing--debugging)
  - [Contributing](#contributing)
  - [License](#license)

---

## Overview

This project contains the Nakama match implementation and supporting code for a persistent open-world match. Key responsibilities:
- Match lifecycle and networking (join/leave/messages) via Nakama runtime.
- Physics integration (Physix-go) for colliders and movement.
- Lua scripting (gopher-lua) to modify objects at runtime and trigger game effects.
- Map loading and automatic collider generation from tile templates.

## Repository structure

- `game.go` — Nakama match implementation (match lifecycle, message handling, broadcasting)
- `script_engine.go` — Lua script runner (gopher-lua) and script API
- `map_loader.go` — map loading and helpers to apply maps into game state
- `physics_engine.go` — wrapper/integration for Physix-go
- `input_processor.go` — player input processing and player object creation
- `database_manager.go` — persistence helpers for world and player data
- `build/` — prebuilt artifacts used for local development (e.g. `backend.so`)

## Prerequisites

- Go 1.20+ (match version in `go.mod`)
- Nakama server (for loading the compiled plugin/module)
- Optional: `make` (provided `Makefile`) and `zsh` (development convenience)

## Build

Build the plugin (example):

```bash
# build plugin into build/backend.so
go build -o build/backend.so -buildmode=plugin
```

Or use the provided Makefile:

```bash
make build
```

## Running locally

1. Build the plugin as above.
2. Configure Nakama to load `build/backend.so` (see Nakama docs for plugin loading).
3. Start Nakama and connect a client that joins the `game` match.

If you want a short developer guide for running Nakama with this plugin locally, tell me your OS and Nakama version and I can add specific steps.

## Development notes

- Coordinate convention: map editors often store tile/object positions using a tile-aligned anchor (top-left). The code centralises this with `TileSize`/`HalfTile` constants (defined in `game.go`). Adjust these constants if your tile size or anchor differs.

- Thread-safety: `GameMatchState` is protected by `gs.mu`. Mutations of `gs.objects` and `obj.Props` must be done while holding that lock.

- Avoid magic numbers: prefer named constants for tile sizes and offsets.

## Script API (Lua)

The `ScriptEngine` exposes helper functions to scripts executed at runtime.

- `effect_ack(msg)` — record an acknowledgement effect returned to the script caller
- `set_object_prop(objectId, key, value)` — set a prop on an object (supports strings, numbers, booleans, tables)
- `get_object_prop(objectId, key)` — returns the value or `nil`
- `has_object_prop(objectId, key)` — returns boolean
- `set_object_gid(objectId, gid[, offsetX, offsetY])` — set tile GID and auto-rebuild colliders from tile templates; optional offsets adjust the object world position
- `add_object_collider(objectId, colliderTable)` — add a collider for an object from Lua
- `remove_object_colliders(objectId)` — remove all colliders owned by the object

Scripts run under `gopher-lua` and errors are logged by `ScriptEngine`.

## OpCodes / Messages

- `OpCodeWorldState` (1) — initial world state for new players
- `OpCodeWorldUpdate` (2) — periodic world updates
- `OpCodeMapChange` (3) — map change notifications
- `OpCodeInputACK` (4) — input acknowledgements
- `OpCodeObjectUpdate` (5) — object delta / interaction notifications

## Testing & debugging

- Use the provided `logger` in match code to inspect lifecycle events, script errors, and state changes.
- Scripts missing or failing will be logged by `ScriptEngine` with the script path and error.
- Keep state mutations under `gs.mu` to avoid race conditions; consider running `go vet` and `go test` where applicable.

## Contributing

- Keep changes small and review thread-safety when touching shared state.
- Centralise repeated logic in helpers (e.g. safe conversions, locking helpers).
- Add tests for non-trivial logic.

## License

See repository root for license information.

---

If you want me to add a developer guide for running Nakama with this plugin on your Linux + zsh environment, tell me the Nakama version and I will append step-by-step instructions.
