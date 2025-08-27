package main

import (
	"github.com/heroiclabs/nakama-common/runtime"
	"github.com/rudransh61/Physix-go/pkg/rigidbody"
	"github.com/rudransh61/Physix-go/pkg/vector"
)

type InputProcessor struct{}

// NewInputProcessor creates a new input processor instance
func NewInputProcessor() *InputProcessor {
	return &InputProcessor{}
}

// ProcessPlayerInput handles different types of player actions
func (ip *InputProcessor) ProcessPlayerInput(gameState *GameMatchState, input *PlayerInput, logger runtime.Logger) {
	switch input.Action {
	case "spawn":
		ip.handleSpawn(gameState, input, logger)
	case "move":
		ip.handleMovement(gameState, input, logger)
	default:
		// logger.Debug("Unknown action: %s from player: %s", input.Action, input.PlayerID)
	}
}

// handleSpawn processes player spawn action
func (ip *InputProcessor) handleSpawn(gameState *GameMatchState, input *PlayerInput, logger runtime.Logger) {
	playerObject := ip.FindPlayerObject(gameState, input.PlayerID)
	if playerObject == nil {
		// Create new player object at spawn position
		spawnPosition := vector.Vector{X: input.X, Y: input.Y}
		if input.X == 0 && input.Y == 0 {
			// Use default spawn position if none provided
			spawnPosition = vector.Vector{X: 400, Y: 300}
		}
		ip.CreatePlayerObject(gameState, input.PlayerID, spawnPosition)
		logger.Info("Created new player object for %s at position (%f, %f)", input.PlayerID, spawnPosition.X, spawnPosition.Y)
	} else {
		// Player object already exists, update position
		if input.X != 0 || input.Y != 0 {
			playerObject.Position = vector.Vector{X: input.X, Y: input.Y}
			playerObject.Velocity = vector.Vector{X: 0, Y: 0}
			// logger.Debug("Player %s re-spawned at position (%f, %f)", input.PlayerID, input.X, input.Y)
		}
	}
}

// handleMovement processes player movement input by setting player velocity.
// The physics engine will then update the position based on this velocity and its fixed deltaTime.
func (ip *InputProcessor) handleMovement(gameState *GameMatchState, input *PlayerInput, logger runtime.Logger) {
	playerObject := ip.FindPlayerObject(gameState, input.PlayerID)
	if playerObject == nil {
		logger.Error("Player object not found for %s", input.PlayerID)
		return
	}

	// Client sends velocity (direction * speed). Set this as the player's current velocity.
	// The physics engine will use this velocity and its own fixed deltaTime for position updates.
	targetVelocity := vector.Vector{
		X: input.VelocityX,
		Y: input.VelocityY,
	}

	// Validate movement speed to prevent cheating (max speed should be reasonable)
	// This check is now on the magnitude of the raw velocity vector sent by client.
	maxSpeed := 300.0 // Maximum pixels per second
	speed := targetVelocity.Magnitude()

	if speed > maxSpeed {
		// Clamp velocity to maximum allowed
		if speed > 0 {
			scaleFactor := maxSpeed / speed
			targetVelocity.X *= scaleFactor
			targetVelocity.Y *= scaleFactor
		}
		// logger.Debug("Player %s velocity clamped from %f to %f", input.PlayerID, speed, maxSpeed)
	}

	// Set the player's velocity. The physics engine will handle position updates.
	playerObject.Velocity = targetVelocity

	// Position will be updated by the physics engine based on this new velocity.
	// Boundary checks will also be handled by the physics engine after it updates the position.

	// logger.Debug("Player %s velocity set to (%f, %f). Position will be updated by physics engine.",
	// 	input.PlayerID, playerObject.Velocity.X, playerObject.Velocity.Y)
}

// FindPlayerObject finds the game object associated with a player
func (ip *InputProcessor) FindPlayerObject(gameState *GameMatchState, playerID string) *rigidbody.RigidBody {
	// Use the player objects mapping to find the player's object
	if playerObject, exists := gameState.playerObjects[playerID]; exists {
		return playerObject
	}
	return nil
}

// CreatePlayerObject creates a new game object for a joining player
func (ip *InputProcessor) CreatePlayerObject(gameState *GameMatchState, playerID string, spawnPosition vector.Vector) *rigidbody.RigidBody {
	playerObject := &rigidbody.RigidBody{
		Position:  spawnPosition,
		Velocity:  vector.Vector{X: 0, Y: 0},
		Mass:      10.0,
		Shape:     "rectangle",
		Width:     40,
		Height:    40,
		IsMovable: true,
	}

	// Add to game objects
	gameState.gameObjects = append(gameState.gameObjects, playerObject)

	// Add to player objects mapping
	gameState.playerObjects[playerID] = playerObject

	return playerObject
}

// RemovePlayerObject removes a player's game object when they leave
func (ip *InputProcessor) RemovePlayerObject(gameState *GameMatchState, playerID string) {
	// Find and remove player's object
	playerObject := ip.FindPlayerObject(gameState, playerID)
	if playerObject != nil {
		// Remove from game objects slice
		for i, obj := range gameState.gameObjects {
			if obj == playerObject {
				// Remove from slice
				gameState.gameObjects = append(gameState.gameObjects[:i], gameState.gameObjects[i+1:]...)
				break
			}
		}

		// Remove from player objects mapping
		delete(gameState.playerObjects, playerID)
	}
}
