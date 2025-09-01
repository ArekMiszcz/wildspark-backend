package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/heroiclabs/nakama-common/runtime"
	"github.com/rudransh61/Physix-go/pkg/polygon"
	"github.com/rudransh61/Physix-go/pkg/rigidbody"
	"github.com/rudransh61/Physix-go/pkg/vector"
	lua "github.com/yuin/gopher-lua"
)

type ScriptEngine struct {
	logger  runtime.Logger
	baseDir string
	pool    sync.Pool
}

type ScriptEffect struct {
	ObjectID int

	AckMessage string
}

func NewScriptEngine(logger runtime.Logger, baseDir string) *ScriptEngine {
	return &ScriptEngine{
		logger:  logger,
		baseDir: baseDir,
		pool: sync.Pool{
			New: func() any {
				L := lua.NewState(
					lua.Options{
						SkipOpenLibs: false,
					},
				)
				return L
			},
		},
	}
}

func (se *ScriptEngine) Execute(scriptPath string, params map[string]any, gs *GameMatchState, dispatcher runtime.MatchDispatcher) ([]ScriptEffect, error) {
	L := se.pool.Get().(*lua.LState)
	defer func() {
		L.Close()
	}()

	effects := make([]ScriptEffect, 0, 4)

	register := func(name string, fn lua.LGFunction) {
		L.SetGlobal(name, L.NewFunction(fn))
	}

	register("effect_ack", func(L *lua.LState) int {
		msg := L.CheckString(1)
		effects = append(effects, ScriptEffect{AckMessage: msg})
		return 0
	})

	// helper to convert lua table back to Go types
	var luaTableToGo func(*lua.LTable) any
	luaTableToGo = func(tbl *lua.LTable) any {
		// detect if array-like
		maxIdx := 0
		isArray := true
		tbl.ForEach(func(k, v lua.LValue) {
			if keyNum, ok := k.(lua.LNumber); ok {
				if int(keyNum) > maxIdx {
					maxIdx = int(keyNum)
				}
			} else {
				isArray = false
			}
		})
		if isArray && maxIdx > 0 {
			arr := make([]any, 0, maxIdx)
			for i := 1; i <= maxIdx; i++ {
				val := tbl.RawGetInt(i)
				if vtbl, ok := val.(*lua.LTable); ok {
					arr = append(arr, luaTableToGo(vtbl))
				} else {
					switch vv := val.(type) {
					case lua.LString:
						arr = append(arr, string(vv))
					case lua.LNumber:
						arr = append(arr, float64(vv))
					case lua.LBool:
						arr = append(arr, bool(vv))
					default:
						arr = append(arr, val.String())
					}
				}
			}
			return arr
		}

		m := make(map[string]any)
		tbl.ForEach(func(k, v lua.LValue) {
			keyStr := k.String()
			switch val := v.(type) {
			case lua.LString:
				m[keyStr] = string(val)
			case lua.LNumber:
				m[keyStr] = float64(val)
			case lua.LBool:
				m[keyStr] = bool(val)
			case *lua.LTable:
				m[keyStr] = luaTableToGo(val)
			default:
				m[keyStr] = v.String()
			}
		})
		return m
	}

	// Script API: set_object_prop(objectId, key, value)
	register("set_object_prop", func(L *lua.LState) int {
		oid := int(L.CheckNumber(1))
		key := L.CheckString(2)
		val := L.CheckAny(3)

		var gv any
		switch val.Type() {
		case lua.LTNil:
			gv = nil
		case lua.LTBool:
			gv = lua.LVAsBool(val)
		case lua.LTNumber:
			gv = float64(lua.LVAsNumber(val))
		case lua.LTString:
			gv = string(lua.LVAsString(val))
		case lua.LTTable:
			gv = luaTableToGo(val.(*lua.LTable))
		default:
			gv = val.String()
		}

		if gs != nil {
			if obj := gs.objects[oid]; obj != nil {
				obj.Props[key] = gv
			}
		}
		return 0
	})

	// Script API: set_object_gid(objectId, gid)
	register("set_object_gid", func(L *lua.LState) int {
		oid := int(L.CheckNumber(1))
		gid := uint32(L.CheckNumber(2))
		if gs == nil {
			return 0
		}

		// Update GID under lock to avoid races with other state mutations
		gs.mu.Lock()
		obj := gs.objects[oid]
		if obj == nil {
			gs.mu.Unlock()
			return 0
		}
		obj.GID = gid
		gs.mu.Unlock()

		// Remove any existing colliders owned by this object
		gs.RemoveOwnerColliders(oid)

		// If we have map tile collision templates, rebuild colliders automatically
		if gs.currentMap == nil {
			se.logger.Info("Current map is nil, cannot set object gid %d", gid)
			return 0
		}

		template, ok := gs.currentMap.TileCollisions[int(gid)]
		if !ok {
			// No tile collision template for this gid
			se.logger.Info("No tile collision template for this gid %d", gid)
			return 0
		}

		// Read object's world center position from Props (set by MapLoader when map objects were created)
		gs.mu.Lock()
		od := gs.objects[oid]
		var centerX, centerY float64
		if od != nil {
			if xv, ok := od.Props["x"]; ok {
				if xf, ok2 := xv.(float64); ok2 {
					centerX = xf
				}
			}
			if yv, ok := od.Props["y"]; ok {
				if yf, ok2 := yv.(float64); ok2 {
					centerY = yf
				}
			}
		}
		gs.mu.Unlock()

		if centerX == 0 && centerY == 0 {
			se.logger.Info("set_object_gid: object %d missing world position props x/y; skipping auto-rebuild", oid)
			return 0
		}

		// Tile top-left (templates are stored relative to tile top-left)
		tileW := float64(gs.currentMap.TileWidth)
		tileH := float64(gs.currentMap.TileHeight)
		tileX := centerX - tileW/2.0
		tileY := centerY - tileH/2.0

		// Create colliders from template and register them as owned by this object
		for _, ct := range template.Colliders {
			rb, pts := MakeRigidBodyFromTileTemplate(tileX, tileY, ct)
			if rb == nil {
				continue
			}
			// If polygon, ensure physics engine gets the vertex list later when registered by GameMatchState
			if len(pts) > 0 {
				se.logger.Info("set_object_gid: object %d adding polygon collider with %d points", oid, len(pts))
			}
			gs.AddOwnerCollider(oid, rb, pts)
		}

		// Broadcast an immediate object update to clients so they can update texture/frame
		// Pass the dispatcher from Execute so scripts that run via the match can push updates immediately.
		if dispatcher != nil {
			gs.BroadcastObjectUpdate(oid, dispatcher, se.logger)
		} else {
			// Best-effort: still call with nil dispatcher so match loop/world snapshots will include the change
			gs.BroadcastObjectUpdate(oid, nil, se.logger)
		}

		return 0
	})

	// Script API: add_object_collider(objectId, colliderTable)
	register("add_object_collider", func(L *lua.LState) int {
		oid := int(L.CheckNumber(1))
		tbl := L.CheckTable(2)

		if gs == nil {
			return 0
		}
		if obj := gs.objects[oid]; obj == nil {
			return 0
		}

		shape := L.GetField(tbl, "shape")
		var rb rigidbody.RigidBody
		rb.Velocity = vector.Vector{X: 0, Y: 0}
		rb.Mass = 0
		rb.IsMovable = false

		if shapeStr, ok := shape.(lua.LString); ok {
			switch string(shapeStr) {
			case "rectangle":
				rb.Shape = "rectangle"
				rb.Width = float64(L.GetField(tbl, "width").(lua.LNumber))
				rb.Height = float64(L.GetField(tbl, "height").(lua.LNumber))
				rb.Position.X = float64(L.GetField(tbl, "x").(lua.LNumber))
				rb.Position.Y = float64(L.GetField(tbl, "y").(lua.LNumber))
				// add collider via helper (empty polygonPoints)
				gs.AddOwnerCollider(oid, &rb, nil)
			case "circle":
				rb.Shape = "circle"
				rb.Radius = float64(L.GetField(tbl, "radius").(lua.LNumber))
				rb.Position.X = float64(L.GetField(tbl, "x").(lua.LNumber))
				rb.Position.Y = float64(L.GetField(tbl, "y").(lua.LNumber))
				// add collider via helper (empty polygonPoints)
				gs.AddOwnerCollider(oid, &rb, nil)
			case "polygon":
				polyTbl := L.GetField(tbl, "polygon")
				if ptbl, ok := polyTbl.(*lua.LTable); ok {
					points := make([]vector.Vector, 0)
					ptbl.ForEach(func(key, val lua.LValue) {
						if vtbl, ok := val.(*lua.LTable); ok {
							x := float64(L.GetField(vtbl, "x").(lua.LNumber))
							y := float64(L.GetField(vtbl, "y").(lua.LNumber))
							points = append(points, vector.Vector{X: x, Y: y})
						}
					})
					poly := polygon.NewPolygon(points, 0, false)
					poly.RigidBody.IsMovable = false
					poly.RigidBody.Shape = "polygon"

					// add collider via helper (handles ownership and physics registration)
					gs.AddOwnerCollider(oid, &poly.RigidBody, points)
				}
			}
		}
		return 0
	})

	// Script API: remove_object_colliders(objectId)
	register("remove_object_colliders", func(L *lua.LState) int {
		oid := int(L.CheckNumber(1))
		if gs == nil {
			return 0
		}

		// delegate to GameMatchState helper (handles locking and cleanup)
		gs.RemoveOwnerColliders(oid)

		return 0
	})

	// Helper to convert Go values (including nested maps/slices) to lua.LValue
	var toLValue func(any) lua.LValue
	toLValue = func(v any) lua.LValue {
		switch v := v.(type) {
		case nil:
			return lua.LNil
		case string:
			return lua.LString(v)
		case bool:
			return lua.LBool(v)
		case float32:
			return lua.LNumber(v)
		case float64:
			return lua.LNumber(v)
		case int:
			return lua.LNumber(v)
		case int32:
			return lua.LNumber(v)
		case int64:
			return lua.LNumber(v)
		case uint:
			return lua.LNumber(v)
		case uint32:
			return lua.LNumber(v)
		case uint64:
			return lua.LNumber(v)
		case map[string]interface{}:
			tbl := L.NewTable()
			for kk, vv := range v {
				tbl.RawSetString(kk, toLValue(vv))
			}
			return tbl
		case []interface{}:
			tbl := L.NewTable()
			for i, vv := range v {
				tbl.RawSetInt(i+1, toLValue(vv))
			}
			return tbl
		default:
			// Fallback: try to stringify
			se.logger.Debug("script: converting unknown param type to string: %T", v)
			return lua.LString(fmt.Sprintf("%v", v))
		}
	}

	ctxTbl := L.NewTable()
	for k, v := range params {
		// Use generic converter for all supported types (including maps/slices)
		L.SetField(ctxTbl, k, toLValue(v))
	}
	L.SetGlobal("ctx", ctxTbl)

	abs := filepath.Join(se.baseDir, scriptPath)
	if _, err := os.Stat(abs); err != nil {
		se.logger.Error("Script file not found: %s", scriptPath)
		return effects, err
	}

	if err := L.DoFile(abs); err != nil {
		se.logger.Error("Error executing script %s: %v", scriptPath, err)
		return effects, err
	}

	return effects, nil
}
