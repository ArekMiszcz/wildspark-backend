# wildspark - Backend

Lightweight open-world game server match module built for Nakama.

This repository contains the Nakama match implementation, a small physics integration (Physix-go), a Lua-based scripting bridge and map loader used by the open-world `game` match.

Contents
- `game.go` — Nakama match implementation (match lifecycle, message handling, broadcasting)
- `script_engine.go` — Lua script runner (gopher-lua) and script API exposed to game scripts
- `map_loader.go` — map loading utilities and helpers to apply maps into game state
- `physics_engine.go` — thin wrapper over Physix-go integration
- `input_processor.go` — player input processing and player object creation
- `database_manager.go` — persistence helpers for world and player data
- `build/` — prebuilt artifacts used for local development (e.g. `backend.so`)

High level
- The server exposes a Nakama match module named `game`.
- Matches are open-world and persistent; they keep running even when players disconnect.
- Scripts (Lua) can run via `ScriptEngine` and call helper functions to manipulate objects.

Prerequisites
- Go 1.20+ (match to your go.mod)
- Nakama server (for loading the compiled plugin/module)
- Optional: Make and zsh for convenience

Build
- Build plugin (example):

  # build plugin into build/backend.so
  go build -o build/backend.so -buildmode=plugin

- Or use the provided Makefile:

  make build

Development notes
- Coordinate convention: map objects from the editor are stored using a tile-aligned anchor.
  The code defines `TileSize` and `HalfTile` constants in `game.go` and uses `HalfTile` to convert coordinates between map/editor origin and server/world origin. Adjust these constants if your tiles or object anchors differ.

- Thread-safety: game state (`GameMatchState`) is protected by `gs.mu`. Mutations of `gs.objects` and object `Props` should be done while holding that lock.

Script API (available inside Lua scripts executed by `ScriptEngine`)
- `effect_ack(msg)` — add an acknowledgment effect returned to caller
- `set_object_prop(objectId, key, value)` — set a prop on an object (value supports basic types and tables)
- `get_object_prop(objectId, key)` — get a prop value
- `has_object_prop(objectId, key)` — returns boolean
- `set_object_gid(objectId, gid[, offsetX, offsetY])` — set tile GID for object and auto-rebuild colliders from tile templates
- `add_object_collider(objectId, colliderTable)` — add a collider for an object from Lua
- `remove_object_colliders(objectId)` — remove all colliders owned by the object

OpCodes / Messages
- OpCodeWorldState (1) — initial world state for new players
- OpCodeWorldUpdate (2) — periodic world updates
- OpCodeMapChange (3) — map change notifications
- OpCodeInputACK (4) — input acknowledgements
- OpCodeObjectUpdate (5) — object delta/interaction notifications

Running locally
1. Build the plugin: `make build` or the `go build` command above.
2. Place or point Nakama to `build/backend.so` when launching the server (consult Nakama docs for loading Go modules).
3. Start Nakama and connect a client that joins the `game` match.

Testing & Debugging
- Use the match logging (logger) to debug life-cycle events and script execution errors.
- Scripts missing or failing will be logged by `ScriptEngine`.

Contributing
- Keep changes small and prefer helpers for repeated patterns (safe conversions, locking).
- Add tests for any non-trivial logic where feasible.

License
- See repository root for license information.

If anything is unclear or you want a short developer guide for running Nakama with this plugin locally, tell me which OS and Nakama version you use and I’ll add specific steps.
