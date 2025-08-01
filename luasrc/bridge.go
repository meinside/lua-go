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
	"context"
	"fmt"
	"runtime"
	"sync"
	"unsafe"
)

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

		switch C.lua_type(s.s, -1) {
		case C.LUA_TSTRING:
			resultChan <- C.GoString(C.lua_tolstring(s.s, -1, nil))
		case C.LUA_TBOOLEAN:
			resultChan <- C.lua_toboolean(s.s, -1) != 0
		case C.LUA_TNUMBER:
			if C.lua_isinteger(s.s, -1) != 0 {
				resultChan <- int64(C.bridge_tointeger(s.s, -1))
			} else {
				resultChan <- float64(C.bridge_tonumber(s.s, -1))
			}
		case C.LUA_TNIL:
			resultChan <- nil
		default:
			resultChan <- nil // Unsupported type
		}
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
			switch C.lua_type(s.s, idx) {
			case C.LUA_TSTRING:
				results[i] = C.GoString(C.lua_tolstring(s.s, idx, nil))
			case C.LUA_TBOOLEAN:
				results[i] = C.lua_toboolean(s.s, idx) != 0
			case C.LUA_TNUMBER:
				if C.lua_isinteger(s.s, idx) != 0 {
					results[i] = int64(C.bridge_tointeger(s.s, idx))
				} else {
					results[i] = float64(C.bridge_tonumber(s.s, idx))
				}
			case C.LUA_TNIL:
				results[i] = nil
			default:
				// For unsupported types, return nil or an error, or a string representation
				results[i] = fmt.Sprintf("<unsupported Lua type: %s>", C.GoString(C.lua_typename(s.s, C.lua_type(s.s, idx))))
			}
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
