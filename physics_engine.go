package main

import (
	"math"
	"strings"

	"github.com/heroiclabs/nakama-common/runtime"
	"github.com/rudransh61/Physix-go/pkg/polygon"
	"github.com/rudransh61/Physix-go/pkg/rigidbody"
	"github.com/rudransh61/Physix-go/pkg/vector"
)

type PhysicsEngine struct {
	gravity         vector.Vector
	worldBounds     WorldBounds
	deltaTime       float64
	polygonRegistry polygonRegistry
}

type WorldBounds struct {
	MinX, MinY float64
	MaxX, MaxY float64
}

func NewPhysicsEngine() *PhysicsEngine {
	return &PhysicsEngine{
		gravity: vector.Vector{X: 0, Y: 0},
		worldBounds: WorldBounds{
			MinX: 0, MinY: 0,
			MaxX: 1600, MaxY: 1200,
		},
		deltaTime:       1.0 / 60.0,
		polygonRegistry: make(polygonRegistry), // Initialize the polygon registry
	}
}

func (pe *PhysicsEngine) UpdatePhysics(gameState *GameMatchState, logger runtime.Logger) {
	// Count movable objects for debugging
	movableCount := 0
	for _, obj := range gameState.gameObjects {
		if obj.IsMovable {
			movableCount++
			pe.updateRigidBody(obj)
		}
	}

	// logger.Debug("Physics update: Processing %d game objects (%d movable)",
	// 	len(gameState.gameObjects), movableCount)

	// Cleanup polygon registry periodically (every 100 ticks)
	if gameState.currentTick%100 == 0 {
		pe.CleanupPolygonRegistry(gameState.gameObjects)
	}

	pe.handleCollisions(gameState.gameObjects, logger)
}

func (pe *PhysicsEngine) updateRigidBody(obj *rigidbody.RigidBody) {
	// Store old position to check if we've moved significantly
	oldPosition := obj.Position

	obj.Position.X += obj.Velocity.X * pe.deltaTime
	obj.Position.Y += obj.Velocity.Y * pe.deltaTime

	pe.handleBoundaryCollision(obj)
	pe.applyDrag(obj)

	// If the object has moved and is a polygon, update its vertices
	if obj.Shape == "polygon" && (obj.Position.X != oldPosition.X || obj.Position.Y != oldPosition.Y) {
		pe.UpdatePolygonVertices(obj)
	}
}

func (pe *PhysicsEngine) handleBoundaryCollision(obj *rigidbody.RigidBody) {
	bounce := 0.7

	if obj.Position.X-obj.Width/2 < pe.worldBounds.MinX {
		obj.Position.X = pe.worldBounds.MinX + obj.Width/2
		obj.Velocity.X = -obj.Velocity.X * bounce
	}
	if obj.Position.X+obj.Width/2 > pe.worldBounds.MaxX {
		obj.Position.X = pe.worldBounds.MaxX - obj.Width/2
		obj.Velocity.X = -obj.Velocity.X * bounce
	}
	if obj.Position.Y-obj.Height/2 < pe.worldBounds.MinY {
		obj.Position.Y = pe.worldBounds.MinY + obj.Height/2
		obj.Velocity.Y = -obj.Velocity.Y * bounce
	}
	if obj.Position.Y+obj.Height/2 > pe.worldBounds.MaxY {
		obj.Position.Y = pe.worldBounds.MaxY - obj.Height/2
		obj.Velocity.Y = -obj.Velocity.Y * bounce
	}
}

func (pe *PhysicsEngine) applyDrag(obj *rigidbody.RigidBody) {
	drag := 0.95
	obj.Velocity.X *= drag
	obj.Velocity.Y *= drag
	if obj.Velocity.Magnitude() < 0.5 {
		obj.Velocity.X, obj.Velocity.Y = 0, 0
	}
}

func (pe *PhysicsEngine) handleCollisions(objects []*rigidbody.RigidBody, logger runtime.Logger) {
	for i := 0; i < len(objects); i++ {
		for j := i + 1; j < len(objects); j++ {
			a := objects[i]
			b := objects[j]

			// Skip static-static
			if !a.IsMovable && !b.IsMovable {
				continue
			}

			// First use AABB as a quick check (broad phase)
			if !pe.aabbOverlap(a, b) {
				continue
			}

			// Detailed collision check (narrow phase)
			collisionInfo := pe.detectCollision(a, b)
			if !collisionInfo.collided {
				continue
			}

			logger.Debug("Collision detected: Object A(pos: %.2f,%.2f, size: %.2fx%.2f, movable: %t) <-> Object B(pos: %.2f,%.2f, size: %.2fx%.2f, movable: %t)",
				a.Position.X, a.Position.Y, a.Width, a.Height, a.IsMovable,
				b.Position.X, b.Position.Y, b.Width, b.Height, b.IsMovable)

			pe.resolvePolygonCollision(a, b, collisionInfo, logger)
		}
	}
}

func (pe *PhysicsEngine) aabbOverlap(a, b *rigidbody.RigidBody) bool {
	sa, sb := strings.ToLower(a.Shape), strings.ToLower(b.Shape)

	if sa == "circle" && sb == "circle" {
		dx := b.Position.X - a.Position.X
		dy := b.Position.Y - a.Position.Y
		distanceSquared := dx*dx + dy*dy
		radiusSumSquared := (a.Radius + b.Radius) * (a.Radius + b.Radius)

		return distanceSquared <= radiusSumSquared
	}

	if sa == "circle" || sb == "circle" {
		circle, rect := a, b

		if sb == "circle" {
			circle, rect = b, a
		}

		closestX := max(rect.Position.X-rect.Width/2, min(circle.Position.X, rect.Position.X+rect.Width/2))
		closestY := max(rect.Position.Y-rect.Height/2, min(circle.Position.Y, rect.Position.Y+rect.Height/2))
		dx := closestX - circle.Position.X
		dy := closestY - circle.Position.Y
		distanceSquared := dx*dx + dy*dy

		return distanceSquared <= circle.Radius*circle.Radius
	}

	halfAX, halfAY := a.Width/2, a.Height/2
	halfBX, halfBY := b.Width/2, b.Height/2

	overlapX := (halfAX + halfBX) - abs(a.Position.X-b.Position.X)
	overlapY := (halfAY + halfBY) - abs(a.Position.Y-b.Position.Y)

	return overlapX >= 0 && overlapY >= 0
}

// The old resolveCollision function is now replaced by the more accurate resolvePolygonCollision

func (pe *PhysicsEngine) SetGravity(g vector.Vector)   { pe.gravity = g }
func (pe *PhysicsEngine) SetWorldBounds(b WorldBounds) { pe.worldBounds = b }
func (pe *PhysicsEngine) GetWorldBounds() WorldBounds  { return pe.worldBounds }

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// CollisionInfo stores information about a collision
type CollisionInfo struct {
	collided     bool
	mtv          vector.Vector // Minimum Translation Vector
	depth        float64       // Penetration depth
	contactPoint vector.Vector // Point of contact
}

// detectCollision checks for collision between two rigidbodies
func (pe *PhysicsEngine) detectCollision(a, b *rigidbody.RigidBody) CollisionInfo {
	// Fast path: circle-to-circle collision
	if a.Shape == "circle" && b.Shape == "circle" {
		return pe.detectCircleCollision(a, b)
	}

	// Default path: SAT-based polygon collision
	return pe.detectPolygonCollision(a, b)
}

// detectCircleCollision handles circle to circle collision efficiently
func (pe *PhysicsEngine) detectCircleCollision(a, b *rigidbody.RigidBody) CollisionInfo {
	// Calculate distance between centers
	dx := b.Position.X - a.Position.X
	dy := b.Position.Y - a.Position.Y
	distanceSquared := dx*dx + dy*dy

	// Sum of radiuses
	radiusSum := a.Radius + b.Radius

	// No collision if distance is greater than sum of radiuses
	if distanceSquared > radiusSum*radiusSum {
		return CollisionInfo{collided: false}
	}

	// Calculate actual distance (avoid sqrt if possible)
	distance := math.Sqrt(distanceSquared)

	// Handle the case where circles are exactly at the same position
	if distance < 0.0001 {
		// Push in random direction to avoid singularity
		return CollisionInfo{
			collided:     true,
			mtv:          vector.Vector{X: a.Radius, Y: 0},
			depth:        radiusSum,
			contactPoint: a.Position,
		}
	}

	// Calculate overlap and direction
	overlap := radiusSum - distance
	direction := vector.Vector{X: dx / distance, Y: dy / distance}

	// Calculate contact point
	contactPoint := vector.Vector{
		X: a.Position.X + direction.X*a.Radius,
		Y: a.Position.Y + direction.Y*a.Radius,
	}

	return CollisionInfo{
		collided:     true,
		mtv:          direction.Scale(overlap),
		depth:        overlap,
		contactPoint: contactPoint,
	}
}

// detectPolygonCollision checks for collision between two rigidbodies using SAT
func (pe *PhysicsEngine) detectPolygonCollision(a, b *rigidbody.RigidBody) CollisionInfo {
	// Get polygon vertices for both objects
	verticesA := pe.getPolygonVertices(a)
	verticesB := pe.getPolygonVertices(b)

	// Get edges and normals for each polygon
	edgesA := pe.getEdges(verticesA)
	edgesB := pe.getEdges(verticesB)

	normalsA := pe.getNormals(edgesA)
	normalsB := pe.getNormals(edgesB)

	// Combine all normals to test
	axes := append(normalsA, normalsB...)

	smallestOverlap := math.MaxFloat64
	var smallestAxis vector.Vector

	// Check each axis
	for _, axis := range axes {
		minA, maxA := pe.projectPolygon(verticesA, axis)
		minB, maxB := pe.projectPolygon(verticesB, axis)

		overlaps, overlap := pe.checkOverlap(minA, maxA, minB, maxB)

		if !overlaps {
			// Separating axis found, no collision
			return CollisionInfo{collided: false}
		}

		// Keep track of smallest overlap
		if overlap < smallestOverlap {
			smallestOverlap = overlap
			smallestAxis = axis
		}
	}

	// Find if we need to flip the axis direction
	direction := b.Position.Sub(a.Position)

	// Make sure MTV points from A to B
	if direction.InnerProduct(smallestAxis) < 0 {
		smallestAxis = smallestAxis.Scale(-1)
	}

	// Collision detected, return collision info
	return CollisionInfo{
		collided: true,
		mtv:      smallestAxis.Scale(smallestOverlap),
		depth:    smallestOverlap,
		// For simple implementation, set contact point as the midpoint
		contactPoint: vector.Vector{
			X: (a.Position.X + b.Position.X) / 2,
			Y: (a.Position.Y + b.Position.Y) / 2,
		},
	}
}

func (pe *PhysicsEngine) getPolygonVertices(rb *rigidbody.RigidBody) []vector.Vector {
	switch strings.ToLower(rb.Shape) {
	case "circle":
		return pe.createCirclePolygon(rb.Position, rb.Radius, 16)
	case "polygon":
		if customVertices := pe.getCustomPolygonVertices(rb); customVertices != nil {
			return customVertices
		}
		return pe.createRectanglePolygon(rb.Position, rb.Width, rb.Height)
	default:
		return pe.createRectanglePolygon(rb.Position, rb.Width, rb.Height)
	}
}

// getCustomPolygonVertices retrieves custom polygon vertices from the registry
func (pe *PhysicsEngine) getCustomPolygonVertices(rb *rigidbody.RigidBody) []vector.Vector {
	// Check if the polygon registry exists
	if pe.polygonRegistry == nil {
		return nil
	}

	// Check if we have custom vertices for this rigidbody in our registry
	if vertices, exists := pe.polygonRegistry[rb]; exists {
		return vertices
	}

	// No custom vertices found
	return nil
}

// createRectanglePolygon creates vertices for a rectangle
func (pe *PhysicsEngine) createRectanglePolygon(position vector.Vector, width, height float64) []vector.Vector {
	halfWidth := width / 2
	halfHeight := height / 2

	return []vector.Vector{
		{X: position.X - halfWidth, Y: position.Y - halfHeight},
		{X: position.X + halfWidth, Y: position.Y - halfHeight},
		{X: position.X + halfWidth, Y: position.Y + halfHeight},
		{X: position.X - halfWidth, Y: position.Y + halfHeight},
	}
}

// createCirclePolygon approximates a circle with a regular polygon
func (pe *PhysicsEngine) createCirclePolygon(position vector.Vector, radius float64, numVertices int) []vector.Vector {
	if numVertices < 3 {
		numVertices = 8 // Minimum number of vertices
	}

	vertices := make([]vector.Vector, numVertices)
	angleStep := 2 * math.Pi / float64(numVertices)

	for i := 0; i < numVertices; i++ {
		angle := float64(i) * angleStep
		x := position.X + radius*math.Cos(angle)
		y := position.Y + radius*math.Sin(angle)
		vertices[i] = vector.Vector{X: x, Y: y}
	}

	return vertices
}

// getEdges returns the edges of a polygon as vectors
func (pe *PhysicsEngine) getEdges(vertices []vector.Vector) []vector.Vector {
	edges := make([]vector.Vector, len(vertices))
	for i := 0; i < len(vertices); i++ {
		edges[i] = vertices[(i+1)%len(vertices)].Sub(vertices[i])
	}
	return edges
}

// getNormals returns the normals of a polygon's edges
func (pe *PhysicsEngine) getNormals(edges []vector.Vector) []vector.Vector {
	normals := make([]vector.Vector, len(edges))
	for i, edge := range edges {
		normals[i] = vector.Vector{X: -edge.Y, Y: edge.X}.Normalize() // Perpendicular vector
	}
	return normals
}

// projectPolygon projects a polygon onto an axis and returns the min and max
func (pe *PhysicsEngine) projectPolygon(vertices []vector.Vector, axis vector.Vector) (float64, float64) {
	min := axis.InnerProduct(vertices[0])
	max := min

	for i := 1; i < len(vertices); i++ {
		projection := axis.InnerProduct(vertices[i])
		if projection < min {
			min = projection
		}
		if projection > max {
			max = projection
		}
	}

	return min, max
}

// checkOverlap checks if projections overlap and returns overlap amount
func (pe *PhysicsEngine) checkOverlap(min1, max1, min2, max2 float64) (bool, float64) {
	if min1 > max2 || min2 > max1 {
		return false, 0
	}

	// Return the overlap amount
	overlap1 := max2 - min1
	overlap2 := max1 - min2

	if overlap1 < overlap2 {
		return true, overlap1
	}
	return true, overlap2
}

// resolvePolygonCollision resolves a collision between two rigidbodies
func (pe *PhysicsEngine) resolvePolygonCollision(a, b *rigidbody.RigidBody, info CollisionInfo, logger runtime.Logger) {
	// Skip if no collision
	if !info.collided {
		return
	}

	moveA := a.IsMovable
	moveB := b.IsMovable

	logger.Debug("Resolving polygon collision with depth: %.2f", info.depth)

	// Apply the Minimum Translation Vector (MTV) to separate objects
	if moveA && moveB {
		// Both objects are movable, move each by half
		a.Position = a.Position.Sub(info.mtv.Scale(0.5))
		b.Position = b.Position.Add(info.mtv.Scale(0.5))
		logger.Debug("Both objects movable: A moved by (%.2f, %.2f), B moved by (%.2f, %.2f)",
			-info.mtv.X/2, -info.mtv.Y/2, info.mtv.X/2, info.mtv.Y/2)

		// Apply impulse to change velocities
		pe.applyCollisionImpulse(a, b, info, logger)
	} else if moveA && !moveB {
		// Only A is movable
		a.Position = a.Position.Sub(info.mtv)
		logger.Debug("Only A movable: moved by (%.2f, %.2f)", -info.mtv.X, -info.mtv.Y)
		a.Velocity = vector.Vector{X: 0, Y: 0}
	} else if !moveA && moveB {
		// Only B is movable
		b.Position = b.Position.Add(info.mtv)
		logger.Debug("Only B movable: moved by (%.2f, %.2f)", info.mtv.X, info.mtv.Y)
		b.Velocity = vector.Vector{X: 0, Y: 0}
	}

	logger.Debug("After resolution - A: (%.2f, %.2f), B: (%.2f, %.2f)",
		a.Position.X, a.Position.Y, b.Position.X, b.Position.Y)
}

// applyCollisionImpulse applies an impulse to change object velocities after collision
func (pe *PhysicsEngine) applyCollisionImpulse(a, b *rigidbody.RigidBody, info CollisionInfo, logger runtime.Logger) {
	// Simplified impulse resolution
	restitution := 0.7 // Bounciness

	// Normal vector
	normal := info.mtv.Normalize()

	// Relative velocity
	relVelocity := b.Velocity.Sub(a.Velocity)

	// Velocity along the normal
	velAlongNormal := relVelocity.InnerProduct(normal)

	// Don't apply impulse if velocities are separating
	if velAlongNormal > 0 {
		return
	}

	// Calculate impulse scalar
	impulseScalar := -(1 + restitution) * velAlongNormal
	impulseScalar /= 1/a.Mass + 1/b.Mass

	// Apply impulse
	impulse := normal.Scale(impulseScalar)
	a.Velocity = a.Velocity.Sub(impulse.Scale(1 / a.Mass))
	b.Velocity = b.Velocity.Add(impulse.Scale(1 / b.Mass))

	logger.Debug("Applied impulse: %.2f, new velocities - A: (%.2f, %.2f), B: (%.2f, %.2f)",
		impulseScalar, a.Velocity.X, a.Velocity.Y, b.Velocity.X, b.Velocity.Y)
}

// polygonRegistry stores custom polygon vertices for rigidbodies
// Key is a pointer to the rigidbody used as a unique identifier
type polygonRegistry map[*rigidbody.RigidBody][]vector.Vector

// AddPolygonToPhysicsEngine registers custom polygon vertices with the physics engine
func AddPolygonToPhysicsEngine(pe *PhysicsEngine, rb *rigidbody.RigidBody, vertices []vector.Vector) {
	// Initialize the registry if needed
	if pe.polygonRegistry == nil {
		pe.polygonRegistry = make(polygonRegistry)
	}

	// Store the vertices for this rigidbody
	pe.polygonRegistry[rb] = vertices
}

// AddPolygonToPhysicsEngineRelative registers polygon vertices that are relative to the rigidbody position
func AddPolygonToPhysicsEngineRelative(pe *PhysicsEngine, rb *rigidbody.RigidBody, relativeVertices []vector.Vector) {
	// Convert relative vertices to absolute world coordinates
	absoluteVertices := make([]vector.Vector, len(relativeVertices))
	for i, v := range relativeVertices {
		absoluteVertices[i] = vector.Vector{
			X: rb.Position.X + v.X,
			Y: rb.Position.Y + v.Y,
		}
	}

	// Register the absolute vertices
	AddPolygonToPhysicsEngine(pe, rb, absoluteVertices)
}

// UpdatePolygonVertices updates the vertices of a polygon when its rigidbody position changes
// This ensures that the custom polygon collider moves with the rigidbody
func (pe *PhysicsEngine) UpdatePolygonVertices(rb *rigidbody.RigidBody) {
	// Check if we have stored relative vertices for this rigidbody
	if pe.polygonRegistry == nil {
		return
	}

	// Get the stored vertices
	storedVertices, exists := pe.polygonRegistry[rb]
	if !exists || len(storedVertices) < 3 {
		return // No polygon or not enough vertices
	}

	// Calculate the current centroid of the polygon
	centroid := vector.Vector{X: 0, Y: 0}
	for _, v := range storedVertices {
		centroid.X += v.X
		centroid.Y += v.Y
	}
	centroid.X /= float64(len(storedVertices))
	centroid.Y /= float64(len(storedVertices))

	// Calculate displacement vector
	displacement := rb.Position.Sub(centroid)

	// Update all vertices by the displacement
	for i := range storedVertices {
		pe.polygonRegistry[rb][i] = storedVertices[i].Add(displacement)
	}
}

// CleanupPolygonRegistry removes entries for rigidbodies that are no longer in the game
// Call this periodically to prevent memory leaks
func (pe *PhysicsEngine) CleanupPolygonRegistry(activeRigidbodies []*rigidbody.RigidBody) {
	if len(pe.polygonRegistry) == 0 {
		return
	}

	// Create a set of active rigidbodies for quick lookup
	activeSet := make(map[*rigidbody.RigidBody]bool)
	for _, rb := range activeRigidbodies {
		activeSet[rb] = true
	}

	// Remove entries for rigidbodies that are not in the active set
	for rb := range pe.polygonRegistry {
		if !activeSet[rb] {
			delete(pe.polygonRegistry, rb)
		}
	}
}

// ---- Debug methods ----
// These methods are intended for development and debugging only

// LogPolygonInfo logs information about a polygon object for debugging
func (pe *PhysicsEngine) LogPolygonInfo(rb *rigidbody.RigidBody, logger runtime.Logger) {
	if rb.Shape != "polygon" {
		logger.Debug("Object is not a polygon: %s", rb.Shape)
		return
	}

	vertices := pe.getPolygonVertices(rb)

	logger.Debug("Polygon Info - Position: (%.2f, %.2f), Size: %.2fx%.2f, Vertices: %d",
		rb.Position.X, rb.Position.Y, rb.Width, rb.Height, len(vertices))

	for i, v := range vertices {
		logger.Debug("  Vertex %d: (%.2f, %.2f)", i, v.X, v.Y)
	}
}

// GetPolygonVertexCount returns the number of vertices in a polygon
func (pe *PhysicsEngine) GetPolygonVertexCount(rb *rigidbody.RigidBody) int {
	if rb.Shape != "polygon" {
		return 0
	}

	vertices := pe.getPolygonVertices(rb)
	return len(vertices)
}

// DumpPolygonRegistry outputs information about all polygons in the registry
func (pe *PhysicsEngine) DumpPolygonRegistry(logger runtime.Logger) {
	if len(pe.polygonRegistry) == 0 {
		logger.Debug("Polygon registry is empty")
		return
	}

	logger.Debug("Polygon Registry Contents: %d entries", len(pe.polygonRegistry))

	count := 0
	for rb, vertices := range pe.polygonRegistry {
		logger.Debug("[%d] Polygon at (%.2f, %.2f) - %d vertices",
			count, rb.Position.X, rb.Position.Y, len(vertices))
		count++
	}
}

// MakeRectangleRigidBody creates a rectangle rigidbody centered at (cx,cy)
func MakeRectangleRigidBody(cx, cy, width, height float64) *rigidbody.RigidBody {
	return &rigidbody.RigidBody{
		Position:  vector.Vector{X: cx, Y: cy},
		Velocity:  vector.Vector{X: 0, Y: 0},
		Mass:      0,
		Shape:     "rectangle",
		Width:     width,
		Height:    height,
		IsMovable: false,
	}
}

// MakeCircleRigidBody creates a circle rigidbody with center (cx,cy) and radius r
func MakeCircleRigidBody(cx, cy, r float64) *rigidbody.RigidBody {
	return &rigidbody.RigidBody{
		Position:  vector.Vector{X: cx, Y: cy},
		Velocity:  vector.Vector{X: 0, Y: 0},
		Mass:      0,
		Shape:     "circle",
		Radius:    r,
		IsMovable: false,
	}
}

// MakePolygonRigidBodyFromPoints creates a polygon rigidbody from absolute world-space points
// It returns the rigidbody and the raw vertex list (useful for registering with physics engine)
func MakePolygonRigidBodyFromPoints(points []vector.Vector) (*rigidbody.RigidBody, []vector.Vector) {
	if len(points) == 0 {
		return nil, nil
	}

	// compute bounding box
	minX, minY := points[0].X, points[0].Y
	maxX, maxY := points[0].X, points[0].Y
	for _, p := range points {
		if p.X < minX {
			minX = p.X
		}
		if p.X > maxX {
			maxX = p.X
		}
		if p.Y < minY {
			minY = p.Y
		}
		if p.Y > maxY {
			maxY = p.Y
		}
	}

	width := maxX - minX
	height := maxY - minY
	centerX := minX + width/2.0
	centerY := minY + height/2.0

	poly := polygon.NewPolygon(points, 0, false)
	poly.RigidBody.Position = vector.Vector{X: centerX, Y: centerY}
	poly.RigidBody.Width = width
	poly.RigidBody.Height = height
	poly.RigidBody.IsMovable = false
	poly.RigidBody.Shape = "polygon"

	return &poly.RigidBody, points
}

// MakeRigidBodyFromTileTemplate creates a rigidbody (and optional vertex list) from a TileColliderTemplate
// tileX/tileY are the top-left world coordinates of the tile
func MakeRigidBodyFromTileTemplate(tileX, tileY float64, ct TileColliderTemplate) (*rigidbody.RigidBody, []vector.Vector) {
	switch ct.Type {
	case "rectangle":
		cx := tileX + ct.OffsetX + ct.Width/2.0
		cy := tileY + ct.OffsetX + ct.Height/2.0
		return MakeRectangleRigidBody(cx, cy, ct.Width, ct.Height), nil
	case "circle":
		cx := tileX + ct.OffsetX
		cy := tileY - ct.Radius*2.0
		return MakeCircleRigidBody(cx, cy, ct.Radius), nil
	case "polygon":
		points := make([]vector.Vector, len(ct.Polygon))
		for i, p := range ct.Polygon {
			points[i] = vector.Vector{X: tileX + ct.OffsetX + p.X, Y: tileY + ct.OffsetX + p.Y}
		}
		rb, pts := MakePolygonRigidBodyFromPoints(points)
		return rb, pts
	default:
		return nil, nil
	}
}
