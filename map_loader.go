package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/heroiclabs/nakama-common/runtime"
	"github.com/rudransh61/Physix-go/pkg/polygon"
	"github.com/rudransh61/Physix-go/pkg/rigidbody"
	"github.com/rudransh61/Physix-go/pkg/vector"
)

// ---- Tiled types ----

type TiledMap struct {
	Width           int             `json:"width"`
	Height          int             `json:"height"`
	TileWidth       int             `json:"tilewidth"`
	TileHeight      int             `json:"tileheight"`
	Orientation     string          `json:"orientation"`
	Layers          []TiledLayer    `json:"layers"`
	Tilesets        []TiledTileset  `json:"tilesets"`
	Properties      []TiledProperty `json:"properties,omitempty"`
	BackgroundColor string          `json:"backgroundcolor,omitempty"`
	// Type field exists in Tiled JSON but not needed here
}

// TiledTilesetData represents a standalone tileset file (.tsx)
type TiledTilesetData struct {
	Name        string          `json:"name"`
	TileWidth   int             `json:"tilewidth"`
	TileHeight  int             `json:"tileheight"`
	TileCount   int             `json:"tilecount"`
	Columns     int             `json:"columns"`
	Image       string          `json:"image,omitempty"`
	ImageWidth  int             `json:"imagewidth,omitempty"`
	ImageHeight int             `json:"imageheight,omitempty"`
	Properties  []TiledProperty `json:"properties,omitempty"`
	Tiles       []TiledTile     `json:"tiles,omitempty"`
}

// TiledTile represents a tile definition in a tileset
type TiledTile struct {
	ID          int             `json:"id"`
	Type        string          `json:"type,omitempty"`
	Properties  []TiledProperty `json:"properties,omitempty"`
	ObjectGroup TiledLayer      `json:"objectgroup,omitempty"` // Collision data
}

type TiledLayer struct {
	ID         int             `json:"id"`
	Name       string          `json:"name"`
	Type       string          `json:"type"` // "tilelayer" | "objectgroup" | etc.
	Width      int             `json:"width"`
	Height     int             `json:"height"`
	Data       []uint32        `json:"data,omitempty"` // use uint32 to safely handle flip flags
	Objects    []TiledObject   `json:"objects,omitempty"`
	Properties []TiledProperty `json:"properties,omitempty"`
	Visible    bool            `json:"visible"`
	Opacity    float64         `json:"opacity"`
	OffsetX    float64         `json:"offsetx,omitempty"`
	OffsetY    float64         `json:"offsety,omitempty"`
}

type TiledObject struct {
	ID         int             `json:"id"`
	Name       string          `json:"name"`
	Type       string          `json:"type"`
	X          float64         `json:"x"`
	Y          float64         `json:"y"`
	Width      float64         `json:"width"`
	Height     float64         `json:"height"`
	Rotation   float64         `json:"rotation,omitempty"`
	Properties []TiledProperty `json:"properties,omitempty"`
	Visible    bool            `json:"visible"`
	Polygon    []struct {      // for polygon objects
		X float64 `json:"x"`
		Y float64 `json:"y"`
	} `json:"polygon,omitempty"`
	Ellipse bool `json:"ellipse,omitempty"`
	// gid may exist for tile objects; we don't need it server-side
	GID uint32 `json:"gid,omitempty"`
}

type TiledTileset struct {
	FirstGID     int           `json:"firstgid"`
	Source       string        `json:"source,omitempty"`
	Name         string        `json:"name,omitempty"`
	TileWidth    int           `json:"tilewidth,omitempty"`
	TileHeight   int           `json:"tileheight,omitempty"`
	TileCount    int           `json:"tilecount,omitempty"`
	Columns      int           `json:"columns,omitempty"`
	Image        string        `json:"image,omitempty"`
	ImageWidth   int           `json:"imagewidth,omitempty"`
	ImageHeight  int           `json:"imageheight,omitempty"`
	Tiles        []TiledTile   `json:"tiles,omitempty"` // Embedded tiles with properties
	ObjectGroups []TiledObject `json:"objectgroup,omitempty"`
}

type TiledProperty struct {
	Name  string      `json:"name"`
	Type  string      `json:"type"`
	Value interface{} `json:"value"`
}

// ---- Loader types ----

type MapLoader struct {
	logger        runtime.Logger
	mapDir        string
	physicsEngine *PhysicsEngine
}

// TileCollisionTemplate stores collision information for a specific tile
type TileCollisionTemplate struct {
	TileID    int                    // The global tile ID
	Colliders []TileColliderTemplate // List of colliders defined for this tile
}

// TileColliderTemplate stores information about a single collider in a tile
type TileColliderTemplate struct {
	Type    string          // Type of collider: "rectangle", "polygon", "circle"
	Width   float64         // Width for rectangle
	Height  float64         // Height for rectangle
	Radius  float64         // Radius for circle
	OffsetX float64         // X offset from tile's top-left corner
	OffsetY float64         // Y offset from tile's top-left corner
	Polygon []vector.Vector // Points for polygon (if applicable)
}

type LoadedMap struct {
	Width          int
	Height         int
	TileWidth      int
	TileHeight     int
	GameObjects    []*rigidbody.RigidBody
	SpawnPoints    []vector.Vector
	Colliders      []*rigidbody.RigidBody
	Background     string
	Properties     map[string]interface{}
	TileCollisions map[int]TileCollisionTemplate // Map of tile ID to collision data
}

// ---- Public API ----

func NewMapLoader(logger runtime.Logger, mapDirectory string) *MapLoader {
	return &MapLoader{
		logger: logger,
		mapDir: mapDirectory,
	}
}

func (ml *MapLoader) LoadMap(filename string) (*LoadedMap, error) {
	ml.logger.Info("Loading map: %s", filename)

	// Read file
	filePath := filepath.Join(ml.mapDir, filename)
	data, err := os.ReadFile(filePath)
	if err != nil {
		ml.logger.Error("Failed to read map file %s: %v", filePath, err)
		return nil, fmt.Errorf("failed to read map file: %w", err)
	}

	// Parse JSON
	var tiledMap TiledMap
	if err := json.Unmarshal(data, &tiledMap); err != nil {
		ml.logger.Error("Failed to parse map JSON %s: %v", filePath, err)
		return nil, fmt.Errorf("failed to parse map JSON: %w", err)
	}

	// Load tilesets and external tilesets
	ml.logger.Debug("Processing %d tilesets in map", len(tiledMap.Tilesets))
	tilesetData := make(map[int]*TiledTilesetData)

	mapDir := filepath.Dir(filePath)
	for _, tileset := range tiledMap.Tilesets {
		if tileset.Source != "" {
			// It's an external tileset
			tilesetPath := filepath.Join(mapDir, tileset.Source)
			relPath, err := filepath.Rel(ml.mapDir, tilesetPath)
			if err != nil {
				ml.logger.Warn("Could not determine relative path for tileset %s: %v", tileset.Source, err)
				relPath = tileset.Source
			}

			data, err := ml.loadExternalTileset(relPath)
			if err != nil {
				ml.logger.Error("Failed to load external tileset %s: %v", tileset.Source, err)
				continue
			}

			tilesetData[tileset.FirstGID] = data
		} else if len(tileset.Tiles) > 0 {
			// Convert embedded tileset to our internal format
			embeddedTileset := &TiledTilesetData{
				Name:        tileset.Name,
				TileWidth:   tileset.TileWidth,
				TileHeight:  tileset.TileHeight,
				TileCount:   tileset.TileCount,
				Columns:     tileset.Columns,
				Image:       tileset.Image,
				ImageWidth:  tileset.ImageWidth,
				ImageHeight: tileset.ImageHeight,
				Tiles:       tileset.Tiles,
			}
			tilesetData[tileset.FirstGID] = embeddedTileset
			ml.logger.Debug("Added embedded tileset: %s with %d tiles", tileset.Name, len(tileset.Tiles))
		}
	}

	lm := &LoadedMap{
		Width:          tiledMap.Width,
		Height:         tiledMap.Height,
		TileWidth:      tiledMap.TileWidth,
		TileHeight:     tiledMap.TileHeight,
		GameObjects:    make([]*rigidbody.RigidBody, 0),
		SpawnPoints:    make([]vector.Vector, 0),
		Colliders:      make([]*rigidbody.RigidBody, 0),
		Background:     tiledMap.BackgroundColor,
		Properties:     map[string]interface{}{},
		TileCollisions: make(map[int]TileCollisionTemplate),
	}

	for _, p := range tiledMap.Properties {
		lm.Properties[p.Name] = p.Value
	}

	// Process tileset collision objects (if any)
	ml.processTilesetColliders(tilesetData, lm)

	// Process layers
	for i := range tiledMap.Layers {
		layer := &tiledMap.Layers[i]
		if !layer.Visible {
			continue
		}
		switch layer.Type {
		case "tilelayer":
			ml.processTileLayer(&tiledMap, layer, lm)
			// Additionally check if any tiles in this layer need special collision processing
			if len(tilesetData) > 0 {
				ml.processTileLayerCollisions(&tiledMap, layer, tilesetData, lm)
			}
		case "objectgroup":
			ml.processObjectLayer(&tiledMap, layer, lm)
			// Check if any objects in this layer reference tiles with collision data
			if len(tilesetData) > 0 {
				ml.processObjectLayerTileCollisions(&tiledMap, layer, tilesetData, lm)
			}
		default:
			ml.logger.Debug("Skipping unsupported layer type: %s (%s)", layer.Type, layer.Name)
		}
	}

	ml.logger.Info("Map loaded: objects=%d, spawnPoints=%d, colliders=%d",
		len(lm.GameObjects), len(lm.SpawnPoints), len(lm.Colliders))

	return lm, nil
}

func (ml *MapLoader) ApplyMapToGameState(loadedMap *LoadedMap, gameState *GameMatchState) {
	ml.logger.Info("Applying map to game state")

	// clear and add
	gameState.gameObjects = make([]*rigidbody.RigidBody, 0, len(loadedMap.GameObjects)+len(loadedMap.Colliders))
	gameState.gameObjects = append(gameState.gameObjects, loadedMap.GameObjects...)
	gameState.gameObjects = append(gameState.gameObjects, loadedMap.Colliders...)

	// set world bounds
	if gameState.physicsEngine != nil {
		worldBounds := WorldBounds{
			MinX: 0,
			MinY: 0,
			MaxX: float64(loadedMap.Width * loadedMap.TileWidth),
			MaxY: float64(loadedMap.Height * loadedMap.TileHeight),
		}
		gameState.physicsEngine.SetWorldBounds(worldBounds)
	}

	ml.logger.Info("Map applied. Total objects: %d, world size: %dx%d px",
		len(gameState.gameObjects),
		loadedMap.Width*loadedMap.TileWidth,
		loadedMap.Height*loadedMap.TileHeight)
}

func (ml *MapLoader) GetRandomSpawnPoint(loadedMap *LoadedMap) vector.Vector {
	if len(loadedMap.SpawnPoints) == 0 {
		return vector.Vector{X: 100, Y: 100}
	}
	return loadedMap.SpawnPoints[0] // deterministic for now
}

func (ml *MapLoader) GetSpawnPointByIndex(loadedMap *LoadedMap, index int) vector.Vector {
	if index < 0 || index >= len(loadedMap.SpawnPoints) {
		return ml.GetRandomSpawnPoint(loadedMap)
	}
	return loadedMap.SpawnPoints[index]
}

func (ml *MapLoader) GetMapInfo(loadedMap *LoadedMap) map[string]interface{} {
	return map[string]interface{}{
		"width":       loadedMap.Width,
		"height":      loadedMap.Height,
		"tileWidth":   loadedMap.TileWidth,
		"tileHeight":  loadedMap.TileHeight,
		"objectCount": len(loadedMap.GameObjects),
		"spawnPoints": len(loadedMap.SpawnPoints),
		"colliders":   len(loadedMap.Colliders),
		"properties":  loadedMap.Properties,
	}
}

// SetPhysicsEngine sets a reference to the physics engine
// This is needed to register custom polygon colliders
func (ml *MapLoader) SetPhysicsEngine(pe *PhysicsEngine) {
	ml.physicsEngine = pe
}

// ---- Internals ----

func (ml *MapLoader) processTileLayer(tmap *TiledMap, layer *TiledLayer, lm *LoadedMap) {
	isCollision := ml.isCollisionLayer(layer)

	// Quick exit for decorative tiles: we do not create physics for them on the server.
	if !isCollision {
		ml.logger.Debug("Skipping non-collision tile layer: %s", layer.Name)
		return
	}

	// Build boolean grid for occupied collision cells (with flip bits stripped)
	w, h := layer.Width, layer.Height
	occ := make([]bool, w*h)
	for i, gid := range layer.Data {
		raw := sanitizeGID(gid)
		if raw != 0 {
			occ[i] = true
		}
	}

	// Simple horizontal merge per row to limit collider count
	tw := float64(tmap.TileWidth)
	th := float64(tmap.TileHeight)

	for y := 0; y < h; y++ {
		x := 0
		for x < w {
			idx := y*w + x
			if !occ[idx] {
				x++
				continue
			}
			// start segment
			x0 := x
			for x < w && occ[y*w+x] {
				x++
			}
			segmentW := float64(x - x0)
			// collider rect in world space (centered)
			cx := float64(x0)*tw + (segmentW*tw)/2.0
			cy := float64(y)*th + th/2.0

			collider := &rigidbody.RigidBody{
				Position:  vector.Vector{X: cx, Y: cy},
				Velocity:  vector.Vector{X: 0, Y: 0},
				Mass:      0, // static
				Shape:     "rectangle",
				Width:     segmentW * tw,
				Height:    th,
				IsMovable: false,
			}
			lm.Colliders = append(lm.Colliders, collider)
		}
	}
	ml.logger.Debug("Built %d tile colliders from layer: %s", len(lm.Colliders), layer.Name)
}

// processTileLayerCollisions processes collision objects from tiles in a tilelayer
func (ml *MapLoader) processTileLayerCollisions(tmap *TiledMap, layer *TiledLayer, tilesetData map[int]*TiledTilesetData, lm *LoadedMap) {
	if len(layer.Data) == 0 || len(lm.TileCollisions) == 0 {
		return
	}

	ml.logger.Debug("Processing tile-based collisions for layer: %s", layer.Name)

	tileWidth := float64(tmap.TileWidth)
	tileHeight := float64(tmap.TileHeight)

	// Iterate through each tile in the layer
	for tileIdx, gid := range layer.Data {
		if gid == 0 {
			continue // Empty tile
		}

		// Get the real GID (removing flip bits)
		realGID := sanitizeGID(gid)

		// Check if this tile has collision templates
		tileTemplate, hasCollision := lm.TileCollisions[int(realGID)]
		if !hasCollision {
			// If no template in our optimized structure, fall back to the old method
			ml.processSingleTileCollision(tmap, layer, tileIdx, gid, realGID, tilesetData, lm)
			continue
		}

		// Calculate world position for this tile (top-left corner)
		tileX := float64((tileIdx % layer.Width)) * tileWidth
		tileY := float64((tileIdx / layer.Width)) * tileHeight

		ml.logger.Debug("Found tile with collision template: gid=%d, pos=(%.2f,%.2f)",
			realGID, tileX, tileY)

		// Process each collision template for this tile
		for i, colliderTemplate := range tileTemplate.Colliders {
			switch colliderTemplate.Type {
			case "rectangle":
				// Rectangle collider
				// Calculate center position
				centerX := tileX + colliderTemplate.OffsetX + colliderTemplate.Width/2
				centerY := tileY + colliderTemplate.OffsetY + colliderTemplate.Height/2

				c := &rigidbody.RigidBody{
					Position:  vector.Vector{X: centerX, Y: centerY},
					Velocity:  vector.Vector{X: 0, Y: 0},
					Mass:      0,
					Shape:     "rectangle",
					Width:     colliderTemplate.Width,
					Height:    colliderTemplate.Height,
					IsMovable: false,
				}

				ml.logger.Debug("Added tile collision rectangle: gid=%d, idx=%d, pos=(%.2f,%.2f), size=(%.2fx%.2f)",
					realGID, i, c.Position.X, c.Position.Y, c.Width, c.Height)
				lm.Colliders = append(lm.Colliders, c)

			case "polygon":
				// Polygon collider - translate all points relative to the tile position
				points := make([]vector.Vector, len(colliderTemplate.Polygon))

				minX, minY := float64(1e10), float64(1e10)
				maxX, maxY := float64(-1e10), float64(-1e10)

				for j, p := range colliderTemplate.Polygon {
					// Add the polygon point coordinates to the tile position and template offset
					vertexX := tileX + colliderTemplate.OffsetX + p.X
					vertexY := tileY + colliderTemplate.OffsetY + p.Y
					points[j] = vector.Vector{X: vertexX, Y: vertexY}

					if vertexX < minX {
						minX = vertexX
					}
					if vertexX > maxX {
						maxX = vertexX
					}
					if vertexY < minY {
						minY = vertexY
					}
					if vertexY > maxY {
						maxY = vertexY
					}
				}

				width := maxX - minX
				height := maxY - minY
				centerX := minX + width/2
				centerY := minY + height/2

				poly := polygon.NewPolygon(points, 0, false)

				poly.RigidBody.Position = vector.Vector{X: centerX, Y: centerY}
				poly.RigidBody.Width = width
				poly.RigidBody.Height = height
				poly.RigidBody.IsMovable = false
				poly.RigidBody.Shape = "polygon"

				ml.logger.Debug("Added tile collision polygon: gid=%d, idx=%d, pos=(%.2f,%.2f), vertices=%d",
					realGID, i, poly.RigidBody.Position.X, poly.RigidBody.Position.Y, len(points))

				if ml.physicsEngine != nil {
					AddPolygonToPhysicsEngine(ml.physicsEngine, &poly.RigidBody, points)
				}

				lm.Colliders = append(lm.Colliders, &poly.RigidBody)

			case "circle":
				// Circle collider - position at center
				centerX := tileX + colliderTemplate.OffsetX
				centerY := tileY + colliderTemplate.OffsetY

				c := &rigidbody.RigidBody{
					Position:  vector.Vector{X: centerX, Y: centerY},
					Velocity:  vector.Vector{X: 0, Y: 0},
					Mass:      0,
					Shape:     "circle",
					Radius:    colliderTemplate.Radius,
					IsMovable: false,
				}

				ml.logger.Debug("Added tile collision circle: gid=%d, idx=%d, pos=(%.2f,%.2f), radius=%.2f",
					realGID, i, c.Position.X, c.Position.Y, c.Radius)

				lm.Colliders = append(lm.Colliders, c)
			}
		}
	}
}

// processSingleTileCollision processes collision objects for a single tile instance
func (ml *MapLoader) processSingleTileCollision(tmap *TiledMap, layer *TiledLayer, tileIdx int, gid uint32, realGID uint32, tilesetData map[int]*TiledTilesetData, lm *LoadedMap) {
	// Find which tileset this tile belongs to
	var firstGID int
	var tileset *TiledTilesetData

	for id, ts := range tilesetData {
		if id <= int(realGID) && (firstGID == 0 || id > firstGID) {
			firstGID = id
			tileset = ts
		}
	}

	if tileset == nil {
		return // No tileset found for this GID
	}

	// Get the local tile ID within the tileset
	localID := int(realGID) - firstGID

	// Check if this tile has collision data
	var tileWithCollision *TiledTile
	for _, tile := range tileset.Tiles {
		if tile.ID == localID {
			if tile.ObjectGroup.Type == "objectgroup" && len(tile.ObjectGroup.Objects) > 0 {
				tileWithCollision = &tile
				break
			}
		}
	}

	if tileWithCollision == nil {
		return // No collision data for this tile
	}

	tileWidth := float64(tmap.TileWidth)
	tileHeight := float64(tmap.TileHeight)

	// Calculate world position for this tile
	// This is the top-left corner of the tile
	tileX := float64((tileIdx % layer.Width)) * tileWidth
	tileY := float64((tileIdx / layer.Width)) * tileHeight

	ml.logger.Debug("Processing collision objects for tile: gid=%d, localID=%d, pos=(%.2f,%.2f)",
		realGID, localID, tileX, tileY)

	// Process each collision object for this tile
	for _, obj := range tileWithCollision.ObjectGroup.Objects {
		if !obj.Visible {
			continue
		}

		if obj.Width > 0 && obj.Height > 0 && !obj.Ellipse {
			// Rectangle collider
			// Center the position based on the object width/height
			centerX := tileX + obj.X + obj.Width/2
			centerY := tileY + obj.Y + obj.Height/2

			c := &rigidbody.RigidBody{
				Position:  vector.Vector{X: centerX, Y: centerY},
				Velocity:  vector.Vector{X: 0, Y: 0},
				Mass:      0,
				Shape:     "rectangle",
				Width:     obj.Width,
				Height:    obj.Height,
				IsMovable: false,
			}

			ml.logger.Debug("Added tile collision rectangle: gid=%d, id=%d, pos=(%.2f,%.2f), size=(%.2fx%.2f)",
				realGID, obj.ID, c.Position.X, c.Position.Y, c.Width, c.Height)
			lm.Colliders = append(lm.Colliders, c)

		} else if len(obj.Polygon) > 2 {
			// Polygon collider - translate all points relative to the tile position
			points := make([]vector.Vector, len(obj.Polygon))

			minX, minY := float64(1e10), float64(1e10)
			maxX, maxY := float64(-1e10), float64(-1e10)

			for j, p := range obj.Polygon {
				// Add the polygon point coordinates to the tile position and object offset
				vertexX := tileX + obj.X + p.X
				vertexY := tileY + obj.Y + p.Y
				points[j] = vector.Vector{X: vertexX, Y: vertexY}

				if vertexX < minX {
					minX = vertexX
				}
				if vertexX > maxX {
					maxX = vertexX
				}
				if vertexY < minY {
					minY = vertexY
				}
				if vertexY > maxY {
					maxY = vertexY
				}
			}

			width := maxX - minX
			height := maxY - minY
			centerX := minX + width/2
			centerY := minY + height/2

			poly := polygon.NewPolygon(points, 0, false)

			poly.RigidBody.Position = vector.Vector{X: centerX, Y: centerY}
			poly.RigidBody.Width = width
			poly.RigidBody.Height = height
			poly.RigidBody.IsMovable = false
			poly.RigidBody.Shape = "polygon"

			ml.logger.Debug("Added tile collision polygon: gid=%d, id=%d, pos=(%.2f,%.2f), vertices=%d",
				realGID, obj.ID, poly.RigidBody.Position.X, poly.RigidBody.Position.Y, len(points))

			if ml.physicsEngine != nil {
				AddPolygonToPhysicsEngine(ml.physicsEngine, &poly.RigidBody, points)
			}

			lm.Colliders = append(lm.Colliders, &poly.RigidBody)

		} else if obj.Ellipse && obj.Width > 0 && obj.Height > 0 {
			// Ellipse collider
			radiusX := obj.Width / 2.0
			radiusY := obj.Height / 2.0

			// Center the ellipse - position relative to the tile
			centerX := tileX + obj.X + radiusX
			centerY := tileY + obj.Y + radiusY

			avgRadius := (radiusX + radiusY) / 2.0
			c := &rigidbody.RigidBody{
				Position:  vector.Vector{X: centerX, Y: centerY},
				Velocity:  vector.Vector{X: 0, Y: 0},
				Mass:      0,
				Shape:     "circle",
				Radius:    avgRadius,
				IsMovable: false,
			}

			ml.logger.Debug("Added tile collision circle: gid=%d, id=%d, pos=(%.2f,%.2f), radius=%.2f",
				realGID, obj.ID, c.Position.X, c.Position.Y, c.Radius)

			lm.Colliders = append(lm.Colliders, c)
		}
	}
}

func (ml *MapLoader) processObjectLayer(tmap *TiledMap, layer *TiledLayer, lm *LoadedMap) {
	isCollision := ml.isCollisionLayer(layer)

	ml.logger.Debug("Processing object layer: %s (isCollision=%v)", layer.Name, isCollision)

	for i := range layer.Objects {
		obj := &layer.Objects[i]
		if !obj.Visible {
			continue
		}

		// Skip tile objects with GID here - they're handled in processObjectLayerTileCollisions
		if obj.GID > 0 {
			continue
		}

		worldX := obj.X + obj.Width/2.0
		worldY := obj.Y + obj.Height/2.0

		if isCollision || strings.EqualFold(obj.Type, "collider") {
			if obj.Width > 0 && obj.Height > 0 {
				c := &rigidbody.RigidBody{
					Position:  vector.Vector{X: worldX, Y: worldY},
					Velocity:  vector.Vector{X: 0, Y: 0},
					Mass:      0,
					Shape:     "rectangle",
					Width:     obj.Width,
					Height:    obj.Height,
					IsMovable: false,
				}
				ml.logger.Debug("Added rectangle collider: %s (id=%d) pos=(%.2f,%.2f) size=(%.2fx%.2f)",
					obj.Name, obj.ID, c.Position.X, c.Position.Y, c.Width, c.Height)
				lm.Colliders = append(lm.Colliders, c)
			} else if len(obj.Polygon) > 2 {
				points := make([]vector.Vector, len(obj.Polygon))

				minX, minY := float64(1e10), float64(1e10)
				maxX, maxY := float64(-1e10), float64(-1e10)

				for j, p := range obj.Polygon {
					worldX := p.X + obj.X
					worldY := p.Y + obj.Y
					points[j] = vector.Vector{X: worldX, Y: worldY}

					if worldX < minX {
						minX = worldX
					}
					if worldX > maxX {
						maxX = worldX
					}
					if worldY < minY {
						minY = worldY
					}
					if worldY > maxY {
						maxY = worldY
					}
				}

				width := maxX - minX
				height := maxY - minY
				centerX := minX + width/2
				centerY := minY + height/2

				poly := polygon.NewPolygon(points, 0, false)

				poly.RigidBody.Position = vector.Vector{X: centerX, Y: centerY}
				poly.RigidBody.Width = width
				poly.RigidBody.Height = height
				poly.RigidBody.IsMovable = false
				poly.RigidBody.Shape = "polygon"

				ml.logger.Debug("Added polygon collider: %s (id=%d) pos=(%.2f,%.2f) vertices=%d",
					obj.Name, obj.ID, poly.RigidBody.Position.X, poly.RigidBody.Position.Y, len(points))

				if ml.physicsEngine != nil {
					AddPolygonToPhysicsEngine(ml.physicsEngine, &poly.RigidBody, points)
				}

				lm.Colliders = append(lm.Colliders, &poly.RigidBody)
			} else if obj.Ellipse && obj.Width > 0 && obj.Height > 0 {
				radiusX := obj.Width / 2.0
				radiusY := obj.Height / 2.0

				avgRadius := (radiusX + radiusY) / 2.0
				c := &rigidbody.RigidBody{
					Position:  vector.Vector{X: worldX, Y: worldY},
					Velocity:  vector.Vector{X: 0, Y: 0},
					Mass:      0,
					Shape:     "circle",
					Radius:    avgRadius,
					IsMovable: false,
				}

				ml.logger.Debug("Added ellipse collider: %s (id=%d) pos=(%.2f,%.2f) radius=%.2f",
					obj.Name, obj.ID, c.Position.X, c.Position.Y, c.Radius)

				lm.Colliders = append(lm.Colliders, c)
			} else {
				ml.logger.Warn("Skipping unsupported collider object (no size): %s (id=%d)", obj.Name, obj.ID)
			}
			continue
		}

		if strings.EqualFold(obj.Type, "spawn_point") || strings.Contains(strings.ToLower(obj.Name), "spawn") {
			lm.SpawnPoints = append(lm.SpawnPoints, vector.Vector{X: worldX, Y: worldY})
			continue
		}
	}
}

// processObjectLayerTileCollisions processes tile objects in an objectgroup that reference tilesets with collision data
func (ml *MapLoader) processObjectLayerTileCollisions(tmap *TiledMap, layer *TiledLayer, tilesetData map[int]*TiledTilesetData, lm *LoadedMap) {
	if len(layer.Objects) == 0 || len(lm.TileCollisions) == 0 {
		return
	}

	ml.logger.Debug("Processing tile-based collisions for object layer: %s", layer.Name)

	// Iterate through each object in the layer
	for _, obj := range layer.Objects {
		if !obj.Visible || obj.GID == 0 {
			continue // Skip invisible objects or non-tile objects
		}

		// Get the real GID (removing flip bits)
		realGID := sanitizeGID(obj.GID)

		// Check if this tile has collision templates
		tileTemplate, hasCollision := lm.TileCollisions[int(realGID)]
		if !hasCollision {
			// No collision data for this tile
			continue
		}

		// Calculate world position for this tile object
		// This is the bottom-left corner in Tiled's coordinate system
		// We need to adjust for the tile height to get the top-left corner
		tileX := obj.X
		tileY := obj.Y - float64(tmap.TileHeight) // Adjust for Tiled's coordinate system

		ml.logger.Debug("Found tile object with collision template: gid=%d, pos=(%.2f,%.2f)",
			realGID, tileX, tileY)

		// Process each collision template for this tile
		for i, colliderTemplate := range tileTemplate.Colliders {
			switch colliderTemplate.Type {
			case "rectangle":
				// Rectangle collider
				// Calculate center position
				centerX := tileX + colliderTemplate.OffsetX + colliderTemplate.Width/2
				centerY := tileY + colliderTemplate.OffsetX + colliderTemplate.Height/2

				c := &rigidbody.RigidBody{
					Position:  vector.Vector{X: centerX, Y: centerY},
					Velocity:  vector.Vector{X: 0, Y: 0},
					Mass:      0,
					Shape:     "rectangle",
					Width:     colliderTemplate.Width,
					Height:    colliderTemplate.Height,
					IsMovable: false,
				}

				ml.logger.Debug("Added tile object collision rectangle: gid=%d, idx=%d, pos=(%.2f,%.2f), size=(%.2fx%.2f)",
					realGID, i, c.Position.X, c.Position.Y, c.Width, c.Height)
				lm.Colliders = append(lm.Colliders, c)

			case "polygon":
				// Polygon collider - translate all points relative to the tile position
				points := make([]vector.Vector, len(colliderTemplate.Polygon))

				minX, minY := float64(1e10), float64(1e10)
				maxX, maxY := float64(-1e10), float64(-1e10)

				for j, p := range colliderTemplate.Polygon {
					// Add the polygon point coordinates to the tile position and template offset
					vertexX := tileX + colliderTemplate.OffsetX + p.X
					vertexY := tileY + colliderTemplate.OffsetX + p.Y
					points[j] = vector.Vector{X: vertexX, Y: vertexY}

					if vertexX < minX {
						minX = vertexX
					}
					if vertexX > maxX {
						maxX = vertexX
					}
					if vertexY < minY {
						minY = vertexY
					}
					if vertexY > maxY {
						maxY = vertexY
					}
				}

				width := maxX - minX
				height := maxY - minY
				centerX := minX + width/2
				centerY := minY + height/2

				poly := polygon.NewPolygon(points, 0, false)

				poly.RigidBody.Position = vector.Vector{X: centerX, Y: centerY}
				poly.RigidBody.Width = width
				poly.RigidBody.Height = height
				poly.RigidBody.IsMovable = false
				poly.RigidBody.Shape = "polygon"

				ml.logger.Debug("Added tile object collision polygon: gid=%d, idx=%d, pos=(%.2f,%.2f), size=(%.2f,%.2f), vertices=%d",
					realGID, i, poly.RigidBody.Position.X, poly.RigidBody.Position.Y, poly.RigidBody.Width, poly.RigidBody.Height, len(points))

				if ml.physicsEngine != nil {
					AddPolygonToPhysicsEngine(ml.physicsEngine, &poly.RigidBody, points)
				}

				lm.Colliders = append(lm.Colliders, &poly.RigidBody)

			case "circle":
				// Circle collider - position at center
				centerX := tileX + colliderTemplate.OffsetX
				centerY := tileY - colliderTemplate.Radius*2

				c := &rigidbody.RigidBody{
					Position:  vector.Vector{X: centerX, Y: centerY},
					Velocity:  vector.Vector{X: 0, Y: 0},
					Mass:      0,
					Shape:     "circle",
					Radius:    colliderTemplate.Radius,
					IsMovable: false,
				}

				ml.logger.Debug("Added tile object collision circle: gid=%d, idx=%d, pos=(%.2f,%.2f), radius=%.2f",
					realGID, i, c.Position.X, c.Position.Y, c.Radius)

				lm.Colliders = append(lm.Colliders, c)
			}
		}
	}
}

func (ml *MapLoader) isCollisionLayer(layer *TiledLayer) bool {
	name := strings.ToLower(layer.Name)
	ml.logger.Debug("Checking if layer is collision: %s", layer.Name)

	if strings.Contains(name, "coll") {
		ml.logger.Debug("Layer %s identified as collision layer by name.", layer.Name)
		return true
	}

	for _, p := range layer.Properties {
		if strings.EqualFold(p.Name, "collision") {
			if b, ok := p.Value.(bool); ok && b {
				return true
			}
		}
	}

	return false
}

const (
	hFlip uint32 = 0x80000000
	vFlip uint32 = 0x40000000
	dFlip uint32 = 0x20000000
)

func sanitizeGID(gid uint32) uint32 {
	return gid & ^(hFlip | vFlip | dFlip)
}

// loadExternalTileset loads an external tileset file and returns the parsed data
func (ml *MapLoader) loadExternalTileset(tilesetPath string) (*TiledTilesetData, error) {
	fullPath := filepath.Join(ml.mapDir, tilesetPath)
	ml.logger.Debug("Loading external tileset: %s", fullPath)

	data, err := os.ReadFile(fullPath)
	if err != nil {
		ml.logger.Error("Failed to read tileset file %s: %v", fullPath, err)
		return nil, fmt.Errorf("failed to read tileset file: %w", err)
	}

	var tileset TiledTilesetData
	if err := json.Unmarshal(data, &tileset); err != nil {
		ml.logger.Error("Failed to parse tileset JSON %s: %v", fullPath, err)
		return nil, fmt.Errorf("failed to parse tileset JSON: %w", err)
	}

	ml.logger.Debug("Loaded external tileset: %s with %d tiles", tileset.Name, tileset.TileCount)
	return &tileset, nil
}

// processTilesetColliders processes collision objects from external tilesets and creates templates
func (ml *MapLoader) processTilesetColliders(tilesetData map[int]*TiledTilesetData, lm *LoadedMap) {
	if len(tilesetData) == 0 {
		return
	}

	ml.logger.Debug("Processing tileset colliders from %d external tilesets", len(tilesetData))

	// Process each tileset
	for firstGID, tileset := range tilesetData {
		if tileset.Tiles == nil {
			continue
		}

		// Process each tile in the tileset that has collision data
		for _, tile := range tileset.Tiles {
			// Check if this tile has an objectgroup (collision data)
			if tile.ObjectGroup.Type != "objectgroup" || len(tile.ObjectGroup.Objects) == 0 {
				continue
			}

			// Calculate global tile ID
			tileID := firstGID + tile.ID
			ml.logger.Debug("Processing tile %d with objectgroup (%d objects)", tileID, len(tile.ObjectGroup.Objects))

			// Create a new tile collision template
			tileTemplate := TileCollisionTemplate{
				TileID:    tileID,
				Colliders: make([]TileColliderTemplate, 0, len(tile.ObjectGroup.Objects)),
			}

			// Process each collision object in this tile
			for _, obj := range tile.ObjectGroup.Objects {
				if !obj.Visible || obj.Type != "collider" {
					// Skip invisible objects or non-collider types
					continue
				}

				if obj.Width > 0 && obj.Height > 0 && !obj.Ellipse {
					// Rectangle collider
					collider := TileColliderTemplate{
						Type:    "rectangle",
						Width:   obj.Width,
						Height:  obj.Height,
						OffsetX: obj.X,
						OffsetY: obj.Y,
					}
					tileTemplate.Colliders = append(tileTemplate.Colliders, collider)

					ml.logger.Debug("Added rectangle collider template for tile %d: pos=(%.2f,%.2f) size=(%.2fx%.2f)",
						tileID, obj.X, obj.Y, obj.Width, obj.Height)

				} else if len(obj.Polygon) > 2 {
					// Polygon collider
					points := make([]vector.Vector, len(obj.Polygon))

					for j, p := range obj.Polygon {
						// Store points relative to object position
						points[j] = vector.Vector{X: p.X, Y: p.Y}
					}

					collider := TileColliderTemplate{
						Type:    "polygon",
						OffsetX: obj.X,
						OffsetY: obj.Y,
						Polygon: points,
					}
					tileTemplate.Colliders = append(tileTemplate.Colliders, collider)

					ml.logger.Debug("Added polygon collider template for tile %d: offset=(%.2f,%.2f) vertices=%d",
						tileID, obj.X, obj.Y, len(points))

				} else if obj.Ellipse && obj.Width > 0 && obj.Height > 0 {
					// Ellipse collider - approximate as circle using average radius
					radiusX := obj.Width / 2.0
					radiusY := obj.Height / 2.0
					avgRadius := (radiusX + radiusY) / 2.0

					collider := TileColliderTemplate{
						Type:    "circle",
						Radius:  avgRadius,
						OffsetX: obj.X + radiusX, // Store center X
						OffsetY: obj.Y + radiusY, // Store center Y
					}
					tileTemplate.Colliders = append(tileTemplate.Colliders, collider)

					ml.logger.Debug("Added circle collider template for tile %d: center=(%.2f,%.2f) radius=%.2f",
						tileID, obj.X+radiusX, obj.Y+radiusY, avgRadius)
				} else {
					ml.logger.Warn("Skipping unsupported tileset collider object (no size): tile=%d id=%d",
						tileID, obj.ID)
				}
			}

			// Add the collision template to the map if it has any colliders
			if len(tileTemplate.Colliders) > 0 {
				lm.TileCollisions[tileID] = tileTemplate
				ml.logger.Debug("Created collision template for tile %d with %d colliders",
					tileID, len(tileTemplate.Colliders))
			}
		}
	}

	ml.logger.Debug("Finished processing tileset colliders, created %d tile collision templates",
		len(lm.TileCollisions))
}
