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

static const char* bridge_get_lua_version_string() {
  return LUA_RELEASE;
}
*/
import "C"

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"unsafe"
)

// Version returns the Lua version string (e.g., "Lua 5.4.8").
// This function directly accesses the LUA_RELEASE macro from lua.h via Cgo.
func Version() string {
	return C.GoString(C.bridge_get_lua_version_string())
}

// State represents a Lua state.
type State struct {
	s      *C.lua_State
	opChan chan func()
	done   chan struct{}
}

// NewState creates a new Lua state and opens the standard libraries.
func NewState() *State {
	s := &State{
		opChan: make(chan func()),
		done:   make(chan struct{}),
	}

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		s.s = C.luaL_newstate()
		C.luaL_openlibs(s.s)

		wg.Done()

		for {
			select {
			case op := <-s.opChan:
				op()
			case <-s.done:
				C.lua_close(s.s)
				s.s = nil
				return
			}
		}
	}()

	// wait until the lua state is created
	wg.Wait()

	return s
}

// Close closes the Lua state.
func (s *State) Close() {
	close(s.done)
}

// Execute executes a string of Lua code.
func (s *State) Execute(ctx context.Context, code string) error {
	if s.s == nil {
		return fmt.Errorf("lua state is closed")
	}

	resultChan := make(chan error, 1)

	s.opChan <- func() {
		select {
		case <-ctx.Done():
			resultChan <- ctx.Err()
			return
		default:
		}

		cCode := C.CString(code)
		defer C.free(unsafe.Pointer(cCode))

		if status := C.bridge_dostring(s.s, cCode); status != C.LUA_OK {
			errStr := C.GoString(C.lua_tolstring(s.s, -1, nil))
			C.bridge_pop(s.s, 1)
			resultChan <- fmt.Errorf("lua error: %s", errStr)
		} else {
			resultChan <- nil
		}
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-resultChan:
		return err
	}
}

// GetGlobal gets a global variable from the Lua state.
func (s *State) GetGlobal(ctx context.Context, name string) any {
	if s.s == nil {
		return fmt.Errorf("lua state is closed")
	}

	resultChan := make(chan any, 1)

	s.opChan <- func() {
		select {
		case <-ctx.Done():
			resultChan <- nil
			return
		default:
		}

		cName := C.CString(name)
		defer C.free(unsafe.Pointer(cName))

		C.lua_getglobal(s.s, cName)
		defer C.bridge_pop(s.s, 1)

		resultChan <- s.toGoValue(-1)
	}

	select {
	case <-ctx.Done():
		return nil
	case res := <-resultChan:
		return res
	}
}

// Evaluate executes a string of Lua code and returns its results.
func (s *State) Evaluate(ctx context.Context, code string) ([]any, error) {
	if s.s == nil {
		return nil, fmt.Errorf("lua state is closed")
	}

	resultChan := make(chan struct {
		results []any
		err     error
	}, 1)

	s.opChan <- func() {
		select {
		case <-ctx.Done():
			resultChan <- struct {
				results []any
				err     error
			}{nil, ctx.Err()}
			return
		default:
		}

		cCode := C.CString(code)
		defer C.free(unsafe.Pointer(cCode))

		// Save the current stack top to determine how many values were pushed
		top := C.lua_gettop(s.s)

		// Load the string as a Lua chunk
		status := C.luaL_loadstring(s.s, cCode)
		if status != C.LUA_OK {
			errStr := C.GoString(C.lua_tolstring(s.s, -1, nil))
			C.bridge_pop(s.s, 1) // Pop the error message
			resultChan <- struct {
				results []any
				err     error
			}{nil, fmt.Errorf("lua load error: %s", errStr)}
			return
		}

		// Call the loaded chunk (0 arguments, LUA_MULTRET results, 0 message handler)
		status = C.bridge_pcall(s.s, 0, C.LUA_MULTRET, 0)
		if status != C.LUA_OK {
			errStr := C.GoString(C.lua_tolstring(s.s, -1, nil))
			C.bridge_pop(s.s, 1) // Pop the error message
			resultChan <- struct {
				results []any
				err     error
			}{nil, fmt.Errorf("lua runtime error: %s", errStr)}
			return
		}

		// Get the number of results pushed onto the stack
		numResults := C.lua_gettop(s.s) - top
		results := make([]any, numResults)

		for i := 0; i < int(numResults); i++ {
			idx := top + C.int(i) + 1 // Index of the result on the stack
			results[i] = s.toGoValue(idx)
		}

		// Pop all results from the stack
		C.bridge_pop(s.s, numResults)

		resultChan <- struct {
			results []any
			err     error
		}{results, nil}
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-resultChan:
		return res.results, res.err
	}
}

// toGoValue converts a Lua value at the given index to a Go value.
// This function must be called from within the locked OS thread.
func (s *State) toGoValue(idx C.int) any {
	switch C.lua_type(s.s, idx) {
	case C.LUA_TSTRING:
		return C.GoString(C.lua_tolstring(s.s, idx, nil))
	case C.LUA_TBOOLEAN:
		return C.lua_toboolean(s.s, idx) != 0
	case C.LUA_TNUMBER:
		if C.lua_isinteger(s.s, idx) != 0 {
			return int64(C.bridge_tointeger(s.s, idx))
		}
		return float64(C.bridge_tonumber(s.s, idx))
	case C.LUA_TTABLE:
		absIdx := C.lua_absindex(s.s, idx)
		goMap := make(map[any]any)

		C.lua_pushnil(s.s) // first key
		for C.lua_next(s.s, absIdx) != 0 {
			// key is at -2, value is at -1
			key := s.toGoValue(-2)
			value := s.toGoValue(-1)
			goMap[key] = value
			C.bridge_pop(s.s, 1) // remove value, keep key for next iteration
		}

		// check if the map can be converted to a slice
		if len(goMap) > 0 {
			isSlice := true
			for i := 1; i <= len(goMap); i++ {
				if _, ok := goMap[int64(i)]; !ok {
					isSlice = false
					break
				}
			}

			if isSlice {
				goSlice := make([]any, len(goMap))
				for i := 1; i <= len(goMap); i++ {
					goSlice[i-1] = goMap[int64(i)]
				}
				return goSlice
			}
		} else {
			return []any{} // empty table is an empty slice
		}

		return goMap
	case C.LUA_TNIL:
		return nil
	default:
		// Return a string representation for other types like function, userdata, etc.
		// FIXME: support function, userdata, and thread
		return fmt.Sprintf("<unsupported Lua type: %s>", C.GoString(C.lua_typename(s.s, C.lua_type(s.s, idx))))
	}
}
