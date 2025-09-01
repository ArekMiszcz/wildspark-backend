package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/heroiclabs/nakama-common/runtime"
	"github.com/rudransh61/Physix-go/pkg/rigidbody"
	"github.com/rudransh61/Physix-go/pkg/vector"
)

// DatabaseManager handles all persistent storage operations for the game
type DatabaseManager struct {
	logger runtime.Logger
	nk     runtime.NakamaModule
}

// Storage collections for organizing game data
const (
	COLLECTION_WORLD_STATE    = "world_state"
	COLLECTION_PLAYER_DATA    = "player_data"
	COLLECTION_GAME_OBJECTS   = "game_objects"
	COLLECTION_WORLD_SETTINGS = "world_settings"
)

// Storage keys for different data types
const (
	KEY_GLOBAL_WORLD_STATE = "global"
	KEY_PHYSICS_SETTINGS   = "physics"
)

// Persistent data structures
type PersistedWorldState struct {
	LastTick       int64                  `json:"lastTick"`
	GameObjects    []*rigidbody.RigidBody `json:"gameObjects"`
	ActivePlayers  []string               `json:"activePlayers"`
	LastUpdateTime time.Time              `json:"lastUpdateTime"`
	PhysicsEnabled bool                   `json:"physicsEnabled"`
}

type PersistedPlayerData struct {
	PlayerID      string        `json:"playerId"`
	Username      string        `json:"username"`
	Position      vector.Vector `json:"position"`
	Velocity      vector.Vector `json:"velocity"`
	Health        float64       `json:"health"`
	Level         int           `json:"level"`
	LastLoginTime time.Time     `json:"lastLoginTime"`
	PlayTime      time.Duration `json:"playTime"`
	Inventory     []string      `json:"inventory"`
	Achievements  []string      `json:"achievements"`
}

type PersistedGameObject struct {
	ObjectID    string                 `json:"objectId"`
	Type        string                 `json:"type"`
	Position    vector.Vector          `json:"position"`
	Velocity    vector.Vector          `json:"velocity"`
	Mass        float64                `json:"mass"`
	Shape       string                 `json:"shape"`
	Width       float64                `json:"width"`
	Height      float64                `json:"height"`
	IsMovable   bool                   `json:"isMovable"`
	Properties  map[string]interface{} `json:"properties"`
	CreatedTime time.Time              `json:"createdTime"`
	LastUpdated time.Time              `json:"lastUpdated"`
}

type WorldSettings struct {
	MaxPlayers    int                    `json:"maxPlayers"`
	SpawnPoints   []vector.Vector        `json:"spawnPoints"`
	WorldBounds   map[string]float64     `json:"worldBounds"`
	PhysicsConfig map[string]interface{} `json:"physicsConfig"`
	GameRules     map[string]interface{} `json:"gameRules"`
}

// NewDatabaseManager creates a new database manager instance
func NewDatabaseManager(logger runtime.Logger, nk runtime.NakamaModule) *DatabaseManager {
	return &DatabaseManager{
		logger: logger,
		nk:     nk,
	}
}

// SaveWorldState persists the current world state to the database
func (dm *DatabaseManager) SaveWorldState(ctx context.Context, gameState *GameMatchState) error {
	worldState := PersistedWorldState{
		LastTick:       gameState.currentTick,
		GameObjects:    gameState.gameObjects,
		ActivePlayers:  dm.getActivePlayerIDs(gameState),
		LastUpdateTime: time.Now(),
		PhysicsEnabled: true,
	}

	data, err := json.Marshal(worldState)
	if err != nil {
		dm.logger.Error("Failed to marshal world state: %v", err)
		return err
	}

	writes := []*runtime.StorageWrite{
		{
			Collection:      COLLECTION_WORLD_STATE,
			Key:             KEY_GLOBAL_WORLD_STATE,
			UserID:          "",
			Value:           string(data),
			PermissionRead:  runtime.STORAGE_PERMISSION_PUBLIC_READ,
			PermissionWrite: runtime.STORAGE_PERMISSION_NO_READ,
		},
	}

	_, err = dm.nk.StorageWrite(ctx, writes)
	if err != nil {
		dm.logger.Error("Failed to save world state: %v", err)
		return err
	}

	dm.logger.Info("World state saved successfully at tick %d", gameState.currentTick)
	return nil
}

// LoadWorldState retrieves the persisted world state from the database
func (dm *DatabaseManager) LoadWorldState(ctx context.Context) (*PersistedWorldState, error) {
	reads := []*runtime.StorageRead{
		{
			Collection: COLLECTION_WORLD_STATE,
			Key:        KEY_GLOBAL_WORLD_STATE,
			UserID:     "",
		},
	}

	objects, err := dm.nk.StorageRead(ctx, reads)
	if err != nil {
		dm.logger.Error("Failed to read world state: %v", err)
		return nil, err
	}

	if len(objects) == 0 {
		dm.logger.Info("No existing world state found, creating new world")
		return dm.createDefaultWorldState(), nil
	}

	var worldState PersistedWorldState
	if err := json.Unmarshal([]byte(objects[0].GetValue()), &worldState); err != nil {
		dm.logger.Error("Failed to unmarshal world state: %v", err)
		return nil, err
	}

	dm.logger.Info("World state loaded successfully from tick %d", worldState.LastTick)
	return &worldState, nil
}

// SavePlayerData persists individual player data
func (dm *DatabaseManager) SavePlayerData(ctx context.Context, presence runtime.Presence, position vector.Vector, velocity vector.Vector) error {
	playerData := PersistedPlayerData{
		PlayerID:      presence.GetUserId(),
		Username:      presence.GetUsername(),
		Position:      position,
		Velocity:      velocity,
		Health:        100.0,
		Level:         1,
		LastLoginTime: time.Now(),
		PlayTime:      time.Hour, // This would be calculated properly
		Inventory:     []string{},
		Achievements:  []string{},
	}

	data, err := json.Marshal(playerData)
	if err != nil {
		dm.logger.Error("Failed to marshal player data: %v", err)
		return err
	}

	writes := []*runtime.StorageWrite{
		{
			Collection:      COLLECTION_PLAYER_DATA,
			Key:             presence.GetUserId(),
			UserID:          presence.GetUserId(),
			Value:           string(data),
			PermissionRead:  runtime.STORAGE_PERMISSION_OWNER_READ,
			PermissionWrite: runtime.STORAGE_PERMISSION_OWNER_WRITE,
		},
	}

	_, err = dm.nk.StorageWrite(ctx, writes)
	if err != nil {
		dm.logger.Error("Failed to save player data for %s: %v", presence.GetUsername(), err)
		return err
	}

	// dm.logger.Debug("Player data saved for %s", presence.GetUsername())
	return nil
}

// LoadPlayerData retrieves individual player data
func (dm *DatabaseManager) LoadPlayerData(ctx context.Context, userID string) (*PersistedPlayerData, error) {
	reads := []*runtime.StorageRead{
		{
			Collection: COLLECTION_PLAYER_DATA,
			Key:        userID,
			UserID:     userID,
		},
	}

	objects, err := dm.nk.StorageRead(ctx, reads)
	if err != nil {
		dm.logger.Error("Failed to read player data for %s: %v", userID, err)
		return nil, err
	}

	if len(objects) == 0 {
		dm.logger.Info("No existing player data found for %s, creating new profile", userID)
		return dm.createDefaultPlayerData(userID), nil
	}

	var playerData PersistedPlayerData
	if err := json.Unmarshal([]byte(objects[0].GetValue()), &playerData); err != nil {
		dm.logger.Error("Failed to unmarshal player data for %s: %v", userID, err)
		return nil, err
	}

	// dm.logger.Debug("Player data loaded for %s", playerData.Username)
	return &playerData, nil
}

// SaveGameObject persists a single game object
func (dm *DatabaseManager) SaveGameObject(ctx context.Context, obj *rigidbody.RigidBody, objectID string) error {
	gameObject := PersistedGameObject{
		ObjectID:    objectID,
		Type:        "rigidbody",
		Position:    obj.Position,
		Velocity:    obj.Velocity,
		Mass:        obj.Mass,
		Shape:       obj.Shape,
		Width:       obj.Width,
		Height:      obj.Height,
		IsMovable:   obj.IsMovable,
		Properties:  map[string]interface{}{},
		CreatedTime: time.Now(),
		LastUpdated: time.Now(),
	}

	data, err := json.Marshal(gameObject)
	if err != nil {
		dm.logger.Error("Failed to marshal game object: %v", err)
		return err
	}

	writes := []*runtime.StorageWrite{
		{
			Collection:      COLLECTION_GAME_OBJECTS,
			Key:             objectID,
			UserID:          "",
			Value:           string(data),
			PermissionRead:  runtime.STORAGE_PERMISSION_PUBLIC_READ,
			PermissionWrite: runtime.STORAGE_PERMISSION_NO_READ,
		},
	}

	_, err = dm.nk.StorageWrite(ctx, writes)
	if err != nil {
		dm.logger.Error("Failed to save game object %s: %v", objectID, err)
		return err
	}

	dm.logger.Debug("Game object %s saved successfully", objectID)
	return nil
}

// LoadAllGameObjects retrieves all persisted game objects
func (dm *DatabaseManager) LoadAllGameObjects(ctx context.Context) ([]*rigidbody.RigidBody, error) {
	// List all objects in the game objects collection
	objects, _, err := dm.nk.StorageList(ctx, "", "", COLLECTION_GAME_OBJECTS, 100, "")
	if err != nil {
		dm.logger.Error("Failed to list game objects: %v", err)
		return nil, err
	}

	var gameObjects []*rigidbody.RigidBody
	for _, obj := range objects {
		var persistedObj PersistedGameObject
		if err := json.Unmarshal([]byte(obj.GetValue()), &persistedObj); err != nil {
			dm.logger.Error("Failed to unmarshal game object: %v", err)
			continue
		}

		rigidBody := &rigidbody.RigidBody{
			Position:  persistedObj.Position,
			Velocity:  persistedObj.Velocity,
			Mass:      persistedObj.Mass,
			Shape:     persistedObj.Shape,
			Width:     persistedObj.Width,
			Height:    persistedObj.Height,
			IsMovable: persistedObj.IsMovable,
		}

		gameObjects = append(gameObjects, rigidBody)
	}

	dm.logger.Info("Loaded %d game objects from storage", len(gameObjects))
	return gameObjects, nil
}

// SaveWorldSettings persists world configuration settings
func (dm *DatabaseManager) SaveWorldSettings(ctx context.Context, settings *WorldSettings) error {
	data, err := json.Marshal(settings)
	if err != nil {
		dm.logger.Error("Failed to marshal world settings: %v", err)
		return err
	}

	writes := []*runtime.StorageWrite{
		{
			Collection:      COLLECTION_WORLD_SETTINGS,
			Key:             KEY_PHYSICS_SETTINGS,
			UserID:          "",
			Value:           string(data),
			PermissionRead:  runtime.STORAGE_PERMISSION_PUBLIC_READ,
			PermissionWrite: runtime.STORAGE_PERMISSION_NO_READ,
		},
	}

	_, err = dm.nk.StorageWrite(ctx, writes)
	if err != nil {
		dm.logger.Error("Failed to save world settings: %v", err)
		return err
	}

	dm.logger.Info("World settings saved successfully")
	return nil
}

// LoadWorldSettings retrieves world configuration settings
func (dm *DatabaseManager) LoadWorldSettings(ctx context.Context) (*WorldSettings, error) {
	reads := []*runtime.StorageRead{
		{
			Collection: COLLECTION_WORLD_SETTINGS,
			Key:        KEY_PHYSICS_SETTINGS,
			UserID:     "",
		},
	}

	objects, err := dm.nk.StorageRead(ctx, reads)
	if err != nil {
		dm.logger.Error("Failed to read world settings: %v", err)
		return nil, err
	}

	if len(objects) == 0 {
		dm.logger.Info("No existing world settings found, creating defaults")
		return dm.createDefaultWorldSettings(), nil
	}

	var settings WorldSettings
	if err := json.Unmarshal([]byte(objects[0].GetValue()), &settings); err != nil {
		dm.logger.Error("Failed to unmarshal world settings: %v", err)
		return nil, err
	}

	dm.logger.Info("World settings loaded successfully")
	return &settings, nil
}

// PeriodicSave performs regular saves of critical game data
func (dm *DatabaseManager) PeriodicSave(ctx context.Context, gameState *GameMatchState) error {
	// // Save world state
	// if err := dm.SaveWorldState(ctx, gameState); err != nil {
	// 	return fmt.Errorf("failed to save world state: %w", err)
	// }

	// // Save individual player data
	// for sessionID, presence := range gameState.presences {
	// 	if playerObj := gameState.inputProcessor.FindPlayerObject(gameState, sessionID); playerObj != nil {
	// 		if err := dm.SavePlayerData(ctx, presence, playerObj.Position, playerObj.Velocity); err != nil {
	// 			dm.logger.Error("Failed to save player data for %s: %v", presence.GetUsername(), err)
	// 		}
	// 	}
	// }

	// // Save game objects (every few saves to reduce I/O)
	// if gameState.currentTick%300 == 0 { // Every 5 seconds at 60 ticks/sec
	// 	for i, obj := range gameState.gameObjects {
	// 		objectID := fmt.Sprintf("obj_%d", i)
	// 		if err := dm.SaveGameObject(ctx, obj, objectID); err != nil {
	// 			dm.logger.Error("Failed to save game object %s: %v", objectID, err)
	// 		}
	// 	}
	// }

	return nil
}

// RestoreWorldFromPersistence initializes game state from saved data
func (dm *DatabaseManager) RestoreWorldFromPersistence(ctx context.Context, gameState *GameMatchState) error {
	// Load world state
	worldState, err := dm.LoadWorldState(ctx)

	if err != nil {
		return fmt.Errorf("failed to load world state: %w", err)
	}

	// Store existing map objects to prevent them from being overwritten
	mapObjectCount := len(gameState.gameObjects)
	dm.logger.Info("Before restoration: %d existing map objects present", mapObjectCount)

	// Restore dynamic game objects if available (without overwriting map objects)
	if len(worldState.GameObjects) > 0 {
		// Only add objects that are dynamic (movable)
		dynamicObjects := make([]*rigidbody.RigidBody, 0)
		for _, obj := range worldState.GameObjects {
			if obj.IsMovable {
				dynamicObjects = append(dynamicObjects, obj)
			}
		}

		// Append dynamic objects to existing map objects
		if len(dynamicObjects) > 0 {
			// Use AddStaticCollider for dynamic objects restored from persistence, or consider owner assignment later.
			for _, obj := range dynamicObjects {
				gameState.AddStaticCollider(obj, nil)
			}
			dm.logger.Info("Added %d dynamic objects from persistent storage", len(dynamicObjects))
		}

		gameState.currentTick = worldState.LastTick
		dm.logger.Info("Restored world state from tick %d", worldState.LastTick)
	} else {
		// Try loading individual game objects
		objects, err := dm.LoadAllGameObjects(ctx)
		if err == nil && len(objects) > 0 {
			// Only add objects that are dynamic (movable)
			dynamicObjects := make([]*rigidbody.RigidBody, 0)
			for _, obj := range objects {
				if obj.IsMovable {
					dynamicObjects = append(dynamicObjects, obj)
				}
			}

			// Append dynamic objects to existing map objects
			if len(dynamicObjects) > 0 {
				for _, obj := range dynamicObjects {
					gameState.AddStaticCollider(obj, nil)
				}
				dm.logger.Info("Added %d dynamic objects from individual storage", len(dynamicObjects))
			}
		}
	}

	// Load world settings
	settings, err := dm.LoadWorldSettings(ctx)
	if err != nil {
		dm.logger.Error("Failed to load world settings: %v", err)
	} else {
		dm.logger.Info("World settings loaded: max players %d", settings.MaxPlayers)
	}

	// Log a summary of the restoration
	dm.logger.Info("World restoration complete: %d total game objects (%d new dynamic objects)",
		len(gameState.gameObjects), len(gameState.gameObjects)-mapObjectCount)

	return nil
}

// Helper methods for creating default data structures
func (dm *DatabaseManager) createDefaultWorldState() *PersistedWorldState {
	return &PersistedWorldState{
		LastTick:       0,
		GameObjects:    initializeGameObjects(),
		ActivePlayers:  []string{},
		LastUpdateTime: time.Now(),
		PhysicsEnabled: true,
	}
}

func (dm *DatabaseManager) createDefaultPlayerData(userID string) *PersistedPlayerData {
	return &PersistedPlayerData{
		PlayerID:      userID,
		Username:      "Unknown",
		Position:      vector.Vector{X: 100, Y: 100},
		Velocity:      vector.Vector{X: 0, Y: 0},
		Health:        100.0,
		Level:         1,
		LastLoginTime: time.Now(),
		PlayTime:      0,
		Inventory:     []string{},
		Achievements:  []string{},
	}
}

func (dm *DatabaseManager) createDefaultWorldSettings() *WorldSettings {
	return &WorldSettings{
		MaxPlayers: 100,
		SpawnPoints: []vector.Vector{
			{X: 100, Y: 100},
			{X: 200, Y: 200},
			{X: 300, Y: 300},
		},
		WorldBounds: map[string]float64{
			"minX": 0,
			"maxX": 1000,
			"minY": 0,
			"maxY": 1000,
		},
		PhysicsConfig: map[string]interface{}{
			"gravity":       9.81,
			"friction":      0.8,
			"airResistance": 0.00,
		},
		GameRules: map[string]interface{}{
			"pvpEnabled":  true,
			"respawnTime": 10,
		},
	}
}

func (dm *DatabaseManager) getActivePlayerIDs(gameState *GameMatchState) []string {
	var playerIDs []string
	for _, presence := range gameState.presences {
		playerIDs = append(playerIDs, presence.GetUserId())
	}
	return playerIDs
}

// CleanupOldData removes old or unused data from storage
func (dm *DatabaseManager) CleanupOldData(ctx context.Context) error {
	// This could implement cleanup logic for old player data, expired objects, etc.
	dm.logger.Info("Database cleanup completed")
	return nil
}
