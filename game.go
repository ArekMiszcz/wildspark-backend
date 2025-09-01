package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/heroiclabs/nakama-common/runtime"
	"github.com/rudransh61/Physix-go/pkg/rigidbody"
	"github.com/rudransh61/Physix-go/pkg/vector"
)

// OpCode constants for different message types
const (
	OpCodeWorldState   = 1 // Initial world state for new players
	OpCodeWorldUpdate  = 2 // Regular world state updates
	OpCodeMapChange    = 3 // Map change notifications
	OpCodeInputACK     = 4 // Input acknowledgments
	OpCodeObjectUpdate = 5 // Interaction notifications (e.g., item pickups)
)

type GameMatch struct{}

type GameMatchState struct {
	presences          map[string]runtime.Presence
	objects            map[int]*ObjectData
	gameObjects        []*rigidbody.RigidBody
	playerObjects      map[string]*rigidbody.RigidBody
	currentTick        int64
	inputProcessor     *InputProcessor
	physicsEngine      *PhysicsEngine
	databaseManager    *DatabaseManager
	mapLoader          *MapLoader
	currentMap         *LoadedMap
	scriptEngine       *ScriptEngine
	mu                 sync.Mutex
	gameObjectsByOwner map[int][]*rigidbody.RigidBody // map from object ID -> colliders owned by that object (authoritative owner index)
	rbOwner            map[*rigidbody.RigidBody]int   // reverse lookup from rigid body pointer -> owner object id (helps cleanup)
}

type GameMessage struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

type PlayerInput struct {
	PlayerID      string  `json:"playerId"`
	ObjectID      int     `json:"objectId,omitempty"`
	Action        string  `json:"action"`
	InputSequence uint64  `json:"inputSequence"`       // Added
	X             float64 `json:"x,omitempty"`         // For direct position (spawn/teleport)
	Y             float64 `json:"y,omitempty"`         // For direct position (spawn/teleport)
	VelocityX     float64 `json:"velocityX,omitempty"` // For movement vector
	VelocityY     float64 `json:"velocityY,omitempty"` // For movement vector
	DeltaTime     float64 `json:"deltaTime,omitempty"` // Time delta for movement calculation
}

// ACK response structure
type InputACK struct {
	PlayerID      string  `json:"playerId"`
	Action        string  `json:"action"`
	InputSequence uint64  `json:"inputSequence"` // Added
	Approved      bool    `json:"approved"`
	Reason        string  `json:"reason,omitempty"`
	Timestamp     int64   `json:"timestamp"`
	X             float64 `json:"x,omitempty"` // Server authoritative position
	Y             float64 `json:"y,omitempty"` // Server authoritative position
}

type GameState struct {
	Tick        int64                  `json:"tick"`
	GameObjects []*rigidbody.RigidBody `json:"gameObjects"`
	Players     map[string]PlayerData  `json:"players"`
}

type ObjectData struct {
	ID    int
	Name  string
	Type  string
	GID   uint32
	Props map[string]interface{}
}

type PlayerData struct {
	SessionID string   `json:"sessionId"`
	UserID    string   `json:"userId"`
	Username  string   `json:"username"`
	Position  Position `json:"position"`
}

// Position represents a 2D position with lowercase JSON field names for client compatibility
type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// ToPosition converts a vector.Vector to Position for JSON serialization
func ToPosition(v vector.Vector) Position {
	return Position{
		X: v.X,
		Y: v.Y,
	}
}

// ToVector converts a Position back to vector.Vector for physics calculations
func (p Position) ToVector() vector.Vector {
	return vector.Vector{
		X: p.X,
		Y: p.Y,
	}
}

func (m *GameMatch) MatchInit(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, params map[string]interface{}) (interface{}, int, string) {
	// Create all required components
	physicsEngine := NewPhysicsEngine()
	mapLoader := NewMapLoader(logger, "/nakama/data/maps")

	// Connect the physics engine to the map loader
	mapLoader.SetPhysicsEngine(physicsEngine)

	state := &GameMatchState{
		presences:       make(map[string]runtime.Presence),
		objects:         make(map[int]*ObjectData),
		gameObjects:     make([]*rigidbody.RigidBody, 0),
		playerObjects:   make(map[string]*rigidbody.RigidBody),
		currentTick:     0,
		inputProcessor:  NewInputProcessor(),
		physicsEngine:   physicsEngine,
		databaseManager: NewDatabaseManager(logger, nk),
		mapLoader:       mapLoader,
		currentMap:      nil,
		scriptEngine:    NewScriptEngine(logger, "/nakama/data/scripts"),
		// map from object ID -> colliders owned by that object (authoritative owner index)
		gameObjectsByOwner: make(map[int][]*rigidbody.RigidBody),
		// reverse lookup from rigid body pointer -> owner object id (helps cleanup)
		rbOwner: make(map[*rigidbody.RigidBody]int),
	}

	// Try to load default map
	defaultMap := "elderford/world.json" // Default map file
	if mapName, exists := params["map"]; exists {
		if mapStr, ok := mapName.(string); ok {
			defaultMap = mapStr
		}
	}

	loadedMap, err := state.mapLoader.LoadMap(defaultMap)
	if err != nil {
		panic(fmt.Sprintf("Failed to load default map %s: %v", defaultMap, err))
	} else {
		state.currentMap = loadedMap
		state.mapLoader.ApplyMapToGameState(loadedMap, state)
		logger.Info("Loaded map: %s", defaultMap)
	}

	logger.Debug("Debug state after initialization: %d game objects, %d player objects", len(state.gameObjects), len(state.playerObjects))

	// Try to restore world state from persistent storage
	if err := state.databaseManager.RestoreWorldFromPersistence(ctx, state); err != nil {
		logger.Error("Failed to restore world from persistence: %v", err)
		// Continue with default initialization
	}

	tickRate := 60 // 60 ticks per second for game simulation
	label := "open_world_game"

	logger.Info("Open world game match initialized - always active with persistent storage")

	return state, tickRate, label
}

func (m *GameMatch) MatchJoin(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, tick int64, state interface{}, presences []runtime.Presence) interface{} {
	gameState, ok := state.(*GameMatchState)
	if !ok {
		logger.Error("state not a valid game state object")
		return nil
	}

	for _, presence := range presences {
		gameState.presences[presence.GetUserId()] = presence
		logger.Info("Player joined open world: %s", presence.GetUsername())

		// Try to load player's saved position and data
		playerData, err := gameState.databaseManager.LoadPlayerData(ctx, presence.GetUserId())
		if err != nil {
			logger.Error("Failed to load player data for %s: %v", presence.GetUsername(), err)
		}

		// Use saved position if available, otherwise use map spawn point
		spawnPosition := vector.Vector{X: 100, Y: 100} // Default fallback
		if playerData != nil {
			spawnPosition = playerData.Position
			logger.Info("Restored player %s to saved position (%f, %f)", presence.GetUsername(), spawnPosition.X, spawnPosition.Y)
		} else if gameState.currentMap != nil {
			// Use map spawn point for new players
			spawnPosition = gameState.mapLoader.GetRandomSpawnPoint(gameState.currentMap)
			logger.Info("Spawning new player %s at map spawn point (%f, %f)", presence.GetUsername(), spawnPosition.X, spawnPosition.Y)
		}

		// Create player object for new player
		gameState.inputProcessor.CreatePlayerObject(gameState, presence.GetUserId(), spawnPosition)
	}

	// Send current world state to new players
	worldData := map[string]interface{}{
		"playerCount": len(gameState.presences),
		"gameObjects": gameState.gameObjects,
	}

	// Include map information if available
	if gameState.currentMap != nil {
		worldData["mapInfo"] = gameState.mapLoader.GetMapInfo(gameState.currentMap)
	}

	message := GameMessage{
		Type: "world_state",
		Data: worldData,
	}

	data, _ := json.Marshal(message)
	dispatcher.BroadcastMessage(OpCodeWorldState, data, nil, nil, true)

	return gameState
}

func (m *GameMatch) MatchJoinAttempt(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, tick int64, state interface{}, presence runtime.Presence, metadata map[string]string) (interface{}, bool, string) {
	gameState, ok := state.(*GameMatchState)
	if !ok {
		logger.Error("state not a valid game state object")
		return nil, false, "Internal server error"
	}

	// Open world - allow all players to join
	return gameState, true, ""
}

func (m *GameMatch) MatchLeave(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, tick int64, state interface{}, presences []runtime.Presence) interface{} {
	gameState, ok := state.(*GameMatchState)
	if !ok {
		logger.Error("state not a valid game state object")
		return nil
	}

	for _, presence := range presences {
		// Save player data before they leave
		if playerObj := gameState.inputProcessor.FindPlayerObject(gameState, presence.GetUserId()); playerObj != nil {
			if err := gameState.databaseManager.SavePlayerData(ctx, presence, playerObj.Position, playerObj.Velocity); err != nil {
				logger.Error("Failed to save player data for %s: %v", presence.GetUsername(), err)
			} else {
				logger.Info("Saved player data for %s at position (%f, %f)", presence.GetUsername(), playerObj.Position.X, playerObj.Position.Y)
			}
		}

		delete(gameState.presences, presence.GetUserId())
		logger.Info("Player left open world: %s", presence.GetUsername())

		// Remove player object when they leave
		gameState.inputProcessor.RemovePlayerObject(gameState, presence.GetUserId())
	}

	// Open world continues running regardless of player count
	return gameState
}

func (m *GameMatch) MatchTerminate(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, tick int64, state interface{}, graceSeconds int) interface{} {
	gameState, ok := state.(*GameMatchState)

	if !ok {
		logger.Error("state not a valid game state object")
		return nil
	}

	if err := gameState.databaseManager.PeriodicSave(ctx, gameState); err != nil {
		logger.Error("Failed to perform final save during termination: %v", err)
	} else {
		logger.Info("Final world state and player data saved successfully during termination")
	}

	logger.Info("Open world match terminating - all data saved")

	return gameState
}

func (m *GameMatch) MatchSignal(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, tick int64, state interface{}, data string) (interface{}, string) {
	gameState, ok := state.(*GameMatchState)

	if !ok {
		logger.Error("state not a valid game state object")
		return nil, "Internal server error"
	}

	logger.Info("Open world match signal received: %s", data)

	// Handle map change signals
	var signal map[string]interface{}
	_ = json.Unmarshal([]byte(data), &signal)
	// No signals supported yet.
	return gameState, ""
}

func (m *GameMatch) MatchLoop(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, tick int64, state interface{}, messages []runtime.MatchData) interface{} {
	gameState, ok := state.(*GameMatchState)
	if !ok {
		logger.Error("state not a valid game state object")
		return nil
	}

	gameState.currentTick = tick

	// Process incoming messages (player inputs)
	for _, message := range messages {
		var input PlayerInput
		if err := json.Unmarshal(message.GetData(), &input); err != nil {
			logger.Error("Failed to unmarshal player input: %v", err)
			continue
		}

		// Ensure PlayerID is set from the presence if not in payload (should be in payload)
		if input.PlayerID == "" {
			input.PlayerID = message.GetUserId()
		}

		// logger.Debug("Received input from %s (OpCode: %d): Action: %s, Seq: %d, VelX: %f, VelY: %f",
		// 	input.PlayerID, message.GetOpCode(), input.Action, input.InputSequence, input.VelocityX, input.VelocityY)

		// Process the input (e.g., update velocity)
		gameState.inputProcessor.ProcessPlayerInput(gameState, &input, dispatcher, logger)

		// After processing input, especially movement, prepare an ACK
		// The actual position update will happen in the physics step.
		// We will send the ACK *after* the physics step for the most up-to-date position.
		// For now, let's just log that we'd prepare an ACK.
		// The ACK needs to be associated with this specific input and player.
	}

	// Update game world using physics engine
	// fixedDeltaTime := 1.0 / 60.0 // Assuming 60 ticks per second // This is handled by the physics engine internally
	gameState.physicsEngine.UpdatePhysics(gameState, logger) // Corrected method name and parameters

	// After physics update, send ACKs for processed inputs and broadcast world state
	// This needs to be more robust to link specific inputs to their resulting state.
	// For simplicity in this step, we iterate presences and if their input was processed this tick, send ACK.
	// A better way would be to queue ACKs when inputs are processed.

	for _, message := range messages { // Iterate again to send ACKs based on inputs processed in *this* tick
		var input PlayerInput
		if err := json.Unmarshal(message.GetData(), &input); err != nil {
			// Already logged, skip
			continue
		}
		if input.PlayerID == "" {
			input.PlayerID = message.GetUserId()
		}

		playerObject := gameState.inputProcessor.FindPlayerObject(gameState, input.PlayerID)
		if playerObject != nil {
			ack := InputACK{
				PlayerID:      input.PlayerID,
				Action:        input.Action,
				InputSequence: input.InputSequence,
				Approved:      true, // Assuming input is always approved for now
				Timestamp:     tick, // Or a more precise server timestamp
				X:             playerObject.Position.X,
				Y:             playerObject.Position.Y,
			}
			ackMessage := GameMessage{
				Type: "input_ack",
				Data: ack,
			}
			ackData, err := json.Marshal(ackMessage)
			if err != nil {
				logger.Error("Failed to marshal InputACK: %v", err)
				continue
			}

			// Send the ACK to the specific player who sent the input
			if presence, ok := gameState.presences[input.PlayerID]; ok {
				dispatcher.BroadcastMessage(OpCodeInputACK, ackData, []runtime.Presence{presence}, nil, true)
				// logger.Debug("Sent ACK for seq %d to player %s, Pos: (%.2f, %.2f)", input.InputSequence, input.PlayerID, ack.X, ack.Y)
			}
		}
	}

	// Broadcast world state periodically (e.g., every few ticks or if changed significantly)
	// For now, let's broadcast every tick for testing
	if tick%2 == 0 { // Broadcast every other tick
		m.broadcastWorldState(gameState, dispatcher, logger)
	}

	// Persist world state periodically
	if tick%300 == 0 { // Every 5 seconds (300 ticks / 60hz)
		if err := gameState.databaseManager.PeriodicSave(ctx, gameState); err != nil {
			logger.Error("Failed to persist world state: %v", err)
		}
	}

	return gameState
}

func (m *GameMatch) broadcastWorldState(gameState *GameMatchState, dispatcher runtime.MatchDispatcher, logger runtime.Logger) {
	// Construct player data for all current presences
	playersData := make(map[string]PlayerData)
	for userID, presence := range gameState.presences {
		playerObj := gameState.inputProcessor.FindPlayerObject(gameState, userID)
		if playerObj != nil {
			playersData[userID] = PlayerData{
				SessionID: presence.GetSessionId(),
				UserID:    userID,
				Username:  presence.GetUsername(),
				Position:  ToPosition(playerObj.Position),
			}
		} else {
			// Player might have just joined and object not fully synced, or an error occurred
			logger.Warn("Player object not found for broadcasting state for UserID: %s", userID)
			// Optionally, send a default/last known state or skip
		}
	}

	// Prepare game state for broadcasting
	worldState := GameState{
		Tick:        gameState.currentTick,
		GameObjects: gameState.gameObjects, // Consider if all game objects need to be sent every time
		Players:     playersData,
	}

	message := GameMessage{
		Type: "world_update",
		Data: worldState,
	}

	data, err := json.Marshal(message)
	if err != nil {
		logger.Error("Failed to marshal world state: %v", err)
		return
	}

	dispatcher.BroadcastMessage(OpCodeWorldUpdate, data, nil, nil, true) // Broadcast to all
	// logger.Debug("Broadcasted world update at tick %d. Player count: %d", gameState.currentTick, len(playersData))
}

func initializeGameObjects() []*rigidbody.RigidBody {
	return []*rigidbody.RigidBody{}
}

// CreateDefaultMatch creates a default open world match that's always available
func CreateDefaultMatch(ctx context.Context, nk runtime.NakamaModule, logger runtime.Logger) (string, error) {
	logger.Info("Creating default open world match")

	// Create match parameters
	params := map[string]interface{}{
		"map": "elderford/world.json", // Default map
	}

	// Create the match using the "game" module
	matchId, err := nk.MatchCreate(ctx, "game", params)
	if err != nil {
		return "", fmt.Errorf("failed to create default match: %v", err)
	}

	logger.Info("Default open world match created: %s", matchId)
	return matchId, nil
}

// EnsureDefaultMatch ensures there's always at least one open world match available
func EnsureDefaultMatch(ctx context.Context, nk runtime.NakamaModule, logger runtime.Logger) error {
	// List existing matches
	matches, err := nk.MatchList(ctx, 10, true, "open_world_game", nil, nil, "")
	if err != nil {
		logger.Error("Failed to list matches: %v", err)
		return err
	}

	// If no matches exist, create one
	if len(matches) == 0 {
		_, err := CreateDefaultMatch(ctx, nk, logger)
		return err
	}

	logger.Info("Found %d existing open world matches", len(matches))
	return nil
}

// AddOwnerCollider adds a collider to the physics slice and records ownership.
// If polygonPoints is non-nil and non-empty, the polygon will be registered with the physics engine.
func (gs *GameMatchState) AddOwnerCollider(owner int, rb *rigidbody.RigidBody, polygonPoints []vector.Vector) {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	gs.gameObjects = append(gs.gameObjects, rb)
	gs.gameObjectsByOwner[owner] = append(gs.gameObjectsByOwner[owner], rb)
	gs.rbOwner[rb] = owner

	if gs.physicsEngine != nil && len(polygonPoints) > 0 {
		AddPolygonToPhysicsEngine(gs.physicsEngine, rb, polygonPoints)
	}
}

// RemoveOwnerColliders removes all colliders owned by the given object and cleans up physics registry.
func (gs *GameMatchState) RemoveOwnerColliders(owner int) {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	toRemove := make(map[*rigidbody.RigidBody]bool)
	for _, rb := range gs.gameObjectsByOwner[owner] {
		toRemove[rb] = true
		if gs.physicsEngine != nil {
			delete(gs.physicsEngine.polygonRegistry, rb)
		}
		delete(gs.rbOwner, rb)
	}

	// filter gameObjects
	newList := make([]*rigidbody.RigidBody, 0, len(gs.gameObjects))
	for _, gobj := range gs.gameObjects {
		if !toRemove[gobj] {
			newList = append(newList, gobj)
		}
	}
	gs.gameObjects = newList
	delete(gs.gameObjectsByOwner, owner)
}

// AddStaticCollider adds a collider to gameObjects without assigning an owner.
// polygonPoints may be provided to register polygon shapes with the physics engine.
func (gs *GameMatchState) AddStaticCollider(rb *rigidbody.RigidBody, polygonPoints []vector.Vector) {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	gs.gameObjects = append(gs.gameObjects, rb)
	if gs.physicsEngine != nil && len(polygonPoints) > 0 {
		AddPolygonToPhysicsEngine(gs.physicsEngine, rb, polygonPoints)
	}
}

// AddPlayerObject registers a player-owned rigid body and keeps playerObjects mapping consistent.
func (gs *GameMatchState) AddPlayerObject(playerID string, rb *rigidbody.RigidBody) {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	gs.gameObjects = append(gs.gameObjects, rb)
	if gs.playerObjects == nil {
		gs.playerObjects = make(map[string]*rigidbody.RigidBody)
	}
	gs.playerObjects[playerID] = rb
}

// RemovePlayerObject removes a player's rigidbody from gameObjects and cleans up any related registries.
func (gs *GameMatchState) RemovePlayerObject(playerID string) {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	rb, ok := gs.playerObjects[playerID]
	if !ok || rb == nil {
		return
	}

	// remove from gameObjects slice
	for i, obj := range gs.gameObjects {
		if obj == rb {
			gs.gameObjects = append(gs.gameObjects[:i], gs.gameObjects[i+1:]...)
			break
		}
	}

	// remove from player mapping
	delete(gs.playerObjects, playerID)

	// remove polygon registry entry if present
	if gs.physicsEngine != nil {
		delete(gs.physicsEngine.polygonRegistry, rb)
	}

	// If this rigidbody was tracked in rbOwner, clean up owner indexes
	if owner, found := gs.rbOwner[rb]; found {
		// remove rb from owner's list
		list := gs.gameObjectsByOwner[owner]
		newList := make([]*rigidbody.RigidBody, 0, len(list))
		for _, r := range list {
			if r != rb {
				newList = append(newList, r)
			}
		}
		if len(newList) == 0 {
			delete(gs.gameObjectsByOwner, owner)
		} else {
			gs.gameObjectsByOwner[owner] = newList
		}
		delete(gs.rbOwner, rb)
	}
}

// BroadcastObjectUpdate builds a small object delta and broadcasts it to connected clients.
// If dispatcher is nil the function returns after preparing the payload (no-op for broadcast).
func (gs *GameMatchState) BroadcastObjectUpdate(oid int, dispatcher runtime.MatchDispatcher, logger runtime.Logger) {
	// Read object state under lock
	gs.mu.Lock()
	obj, ok := gs.objects[oid]
	gs.mu.Unlock()
	if !ok || obj == nil {
		return
	}

	// Build payload with minimal fields clients need to render
	payload := map[string]interface{}{
		"id":    obj.ID,
		"gid":   obj.GID,
		"props": obj.Props,
	}

	msg := GameMessage{
		Type: "object.update",
		Data: payload,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		logger.Error("BroadcastObjectUpdate: failed to marshal object update: %v", err)
		return
	}

	if dispatcher != nil {
		dispatcher.BroadcastMessage(OpCodeObjectUpdate, data, nil, nil, true)
	} else {
		// No dispatcher available; caller can choose to enqueue or log. For now we do nothing.
	}
}
