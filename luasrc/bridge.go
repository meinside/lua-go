// bridge.go

// Package luasrc provides a wrapper for the low-level Lua C API.
package luasrc

// #cgo darwin CFLAGS: -DLUA_USE_MACOSX
// #cgo linux CFLAGS: -DLUA_USE_LINUX
// #cgo LDFLAGS: -lm
/*
#include <stdlib.h>
#include "lua.h"
#include "lauxlib.h"
#include "lualib.h"

static int bridge_dostring(lua_State* L, const char* s) {
  return luaL_dostring(L, s);
}

static void bridge_pop(lua_State* L, int n) {
  lua_pop(L, n);
}

static lua_Integer bridge_tointeger(lua_State* L, int i) {
  return lua_tointeger(L, i);
}

static lua_Number bridge_tonumber(lua_State* L, int i) {
  return lua_tonumber(L, i);
}

static int bridge_pcall(lua_State* L, int nargs, int nresults, int errfunc) {
  return lua_pcall(L, nargs, nresults, errfunc);
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// State represents a Lua state.
type State struct {
	L *C.lua_State
}

// NewState creates a new Lua state and opens the standard libraries.
func NewState() *State {
	L := C.luaL_newstate()
	C.luaL_openlibs(L)
	return &State{L: L}
}

// Close closes the Lua state.
func (s *State) Close() {
	if s.L != nil {
		C.lua_close(s.L)
		s.L = nil
	}
}

// Execute executes a string of Lua code.
func (s *State) Execute(code string) error {
	cCode := C.CString(code)
	defer C.free(unsafe.Pointer(cCode))

	if status := C.bridge_dostring(s.L, cCode); status != C.LUA_OK {
		// Pop the error message from the stack
		errStr := C.GoString(C.lua_tolstring(s.L, -1, nil))
		C.bridge_pop(s.L, 1)
		return fmt.Errorf("lua error: %s", errStr)
	}
	return nil
}

// GetGlobal gets a global variable from the Lua state.
func (s *State) GetGlobal(name string) any {
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))

	C.lua_getglobal(s.L, cName)
	defer C.bridge_pop(s.L, 1)

	switch C.lua_type(s.L, -1) {
	case C.LUA_TSTRING:
		return C.GoString(C.lua_tolstring(s.L, -1, nil))
	case C.LUA_TBOOLEAN:
		return C.lua_toboolean(s.L, -1) != 0
	case C.LUA_TNUMBER:
		if C.lua_isinteger(s.L, -1) != 0 {
			return int64(C.bridge_tointeger(s.L, -1))
		}
		return float64(C.bridge_tonumber(s.L, -1))
	case C.LUA_TNIL:
		return nil
	default:
		return nil // Unsupported type
	}
}

// Evaluate executes a string of Lua code and returns its results.
func (s *State) Evaluate(code string) ([]any, error) {
	cCode := C.CString(code)
	defer C.free(unsafe.Pointer(cCode))

	// Save the current stack top to determine how many values were pushed
	top := C.lua_gettop(s.L)

	// Load the string as a Lua chunk
	status := C.luaL_loadstring(s.L, cCode)
	if status != C.LUA_OK {
		errStr := C.GoString(C.lua_tolstring(s.L, -1, nil))
		C.bridge_pop(s.L, 1) // Pop the error message
		return nil, fmt.Errorf("lua load error: %s", errStr)
	}

	// Call the loaded chunk (0 arguments, LUA_MULTRET results, 0 message handler)
	status = C.bridge_pcall(s.L, 0, C.LUA_MULTRET, 0)
	if status != C.LUA_OK {
		errStr := C.GoString(C.lua_tolstring(s.L, -1, nil))
		C.bridge_pop(s.L, 1) // Pop the error message
		return nil, fmt.Errorf("lua runtime error: %s", errStr)
	}

	// Get the number of results pushed onto the stack
	numResults := C.lua_gettop(s.L) - top
	results := make([]any, numResults)

	for i := 0; i < int(numResults); i++ {
		idx := top + C.int(i) + 1 // Index of the result on the stack
		switch C.lua_type(s.L, idx) {
		case C.LUA_TSTRING:
			results[i] = C.GoString(C.lua_tolstring(s.L, idx, nil))
		case C.LUA_TBOOLEAN:
			results[i] = C.lua_toboolean(s.L, idx) != 0
		case C.LUA_TNUMBER:
			if C.lua_isinteger(s.L, idx) != 0 {
				results[i] = int64(C.bridge_tointeger(s.L, idx))
			} else {
				results[i] = float64(C.bridge_tonumber(s.L, idx))
			}
		case C.LUA_TNIL:
			results[i] = nil
		default:
			// For unsupported types, return nil or an error, or a string representation
			results[i] = fmt.Sprintf("<unsupported Lua type: %s>", C.GoString(C.lua_typename(s.L, C.lua_type(s.L, idx))))
		}
	}

	// Pop all results from the stack
	C.bridge_pop(s.L, numResults)

	return results, nil
}
