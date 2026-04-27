// bridge.go

// Package luasrc provides a wrapper for the low-level Lua C API.
package luasrc

// #cgo darwin CFLAGS: -DLUA_USE_MACOSX
// #cgo linux CFLAGS: -DLUA_USE_LINUX
// #cgo LDFLAGS: -lm
/*
#include <stdlib.h>
#include <stdatomic.h>
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

static void bridge_openlibs(lua_State* L) {
  luaL_openlibs(L);
}

static const char* bridge_get_lua_version_string() {
  return LUA_RELEASE;
}

// bridge_cancel_hook aborts Lua execution with an error when the cancel
// flag stored in the state's extraspace is non-zero.
static void bridge_cancel_hook(lua_State* L, lua_Debug* ar) {
  (void)ar;
  _Atomic int* flag = *(_Atomic int**)lua_getextraspace(L);
  if (flag != NULL && atomic_load(flag) != 0) {
    luaL_error(L, "context canceled");
  }
}

static void* bridge_alloc_cancel_flag(void) {
  _Atomic int* f = (_Atomic int*)malloc(sizeof(_Atomic int));
  if (f != NULL) {
    atomic_store(f, 0);
  }
  return (void*)f;
}

static void bridge_free_cancel_flag(void* f) {
  free(f);
}

static void bridge_set_cancel_flag(void* f, int v) {
  atomic_store((_Atomic int*)f, v);
}

// bridge_install_cancel_hook stores the cancel flag pointer in the state's
// extraspace and installs a count-based debug hook that checks it.
static void bridge_install_cancel_hook(lua_State* L, void* f, int count) {
  *(void**)lua_getextraspace(L) = f;
  lua_sethook(L, bridge_cancel_hook, LUA_MASKCOUNT, count);
}

static void bridge_pushlstring(lua_State* L, const char* s, size_t n) {
  lua_pushlstring(L, s, n);
}

static void bridge_pushboolean(lua_State* L, int b) {
  lua_pushboolean(L, b);
}

static void bridge_rawseti(lua_State* L, int idx, lua_Integer i) {
  lua_rawseti(L, idx, i);
}

static void bridge_rawset(lua_State* L, int idx) {
  lua_rawset(L, idx);
}

static void bridge_createtable(lua_State* L, int narr, int nrec) {
  lua_createtable(L, narr, nrec);
}

// The Go side exports this; forward-declared so the C trampoline below
// can reference it. A negative return value indicates the Go dispatcher
// has already pushed a single error message onto the stack and wants
// the trampoline to raise it via lua_error. This contract exists
// because calling lua_error / luaL_error from Go (cgo callback) would
// longjmp out of the Go stack and corrupt the Go runtime.
extern int bridgeGoDispatcher(lua_State* L);

static int bridge_go_dispatcher(lua_State* L) {
  int n = bridgeGoDispatcher(L);
  if (n < 0) {
    return lua_error(L);
  }
  return n;
}

// bridge_push_go_closure pushes a C closure that, when called, invokes
// the Go dispatcher. The given funcID is stored as the first upvalue.
static void bridge_push_go_closure(lua_State* L, lua_Integer funcID) {
  lua_pushinteger(L, funcID);
  lua_pushcclosure(L, bridge_go_dispatcher, 1);
}

static lua_Integer bridge_upvalue_integer(lua_State* L, int n) {
  return lua_tointeger(L, lua_upvalueindex(n));
}

// bridge_ref_value duplicates the value at idx and stores it in the
// registry, returning the luaL_ref handle. The original stack value is
// left untouched.
static int bridge_ref_value(lua_State* L, int idx) {
  lua_pushvalue(L, idx);
  return luaL_ref(L, LUA_REGISTRYINDEX);
}

static void bridge_unref(lua_State* L, int ref) {
  luaL_unref(L, LUA_REGISTRYINDEX, ref);
}

static void bridge_push_ref(lua_State* L, int ref) {
  lua_rawgeti(L, LUA_REGISTRYINDEX, ref);
}
*/
import "C"

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"
)

// ErrStateClosed is returned when a method is called on a closed State,
// or when the State is closed concurrently with a pending call.
var ErrStateClosed = errors.New("lua state is closed")

// Version returns the Lua version string (e.g., "Lua 5.4.8").
// This function directly accesses the LUA_RELEASE macro from lua.h via Cgo.
func Version() string {
	return C.GoString(C.bridge_get_lua_version_string())
}

// cancelHookCount is the number of Lua VM instructions between cancel-hook
// invocations. Lower values improve cancellation responsiveness at the cost
// of per-instruction overhead.
const cancelHookCount = 1000

// GoFunc is the signature for a Go function callable from Lua.
// ctx is the context of the Lua call that triggered the invocation, so
// implementations can honor cancellation. args and the first return
// value follow the same Go/Lua conversion rules as Evaluate and Call.
// Returning a non-nil error propagates as a Lua error to the pcall that
// invoked this function.
type GoFunc func(ctx context.Context, args []any) ([]any, error)

// State represents a Lua state.
type State struct {
	s          *C.lua_State
	cancelFlag unsafe.Pointer // *_Atomic int owned by C
	opChan     chan func()
	done       chan struct{}
	closed     chan struct{}
	closeOnce  sync.Once

	// Accessed only from the worker goroutine.
	goFuncs    map[int64]GoFunc
	nextFuncID int64
	currentCtx context.Context
}

// stateRegistry maps a *C.lua_State to its owning *State so the exported
// Go dispatcher can resolve back to the State when called via the C
// closure trampoline.
var stateRegistry sync.Map // map[uintptr]*State

// NewState creates a new Lua state and opens the standard libraries.
func NewState() *State {
	s := &State{
		opChan:  make(chan func()),
		done:    make(chan struct{}),
		closed:  make(chan struct{}),
		goFuncs: make(map[int64]GoFunc),
	}

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(s.closed)

		s.s = C.luaL_newstate()
		C.bridge_openlibs(s.s)
		s.cancelFlag = C.bridge_alloc_cancel_flag()
		C.bridge_install_cancel_hook(s.s, s.cancelFlag, C.int(cancelHookCount))
		stateRegistry.Store(uintptr(unsafe.Pointer(s.s)), s)

		wg.Done()

		for {
			select {
			case op := <-s.opChan:
				op()
			case <-s.done:
				stateRegistry.Delete(uintptr(unsafe.Pointer(s.s)))
				C.lua_close(s.s)
				s.s = nil
				C.bridge_free_cancel_flag(s.cancelFlag)
				s.cancelFlag = nil
				s.goFuncs = nil
				return
			}
		}
	}()

	// wait until the lua state is created
	wg.Wait()

	// Best-effort safety net if the caller forgets Close(). Users
	// should still call Close() explicitly — the finalizer blocks
	// until the worker goroutine exits, which delays GC.
	runtime.SetFinalizer(s, func(s *State) { s.Close() })

	return s
}

// Close closes the Lua state and waits for cleanup to finish.
// It is safe to call multiple times; subsequent callers also wait for
// the close to complete. If a Lua chunk is currently running, the
// cancel hook is signaled so it aborts promptly.
func (s *State) Close() {
	s.closeOnce.Do(func() {
		if s.cancelFlag != nil {
			C.bridge_set_cancel_flag(s.cancelFlag, 1)
		}
		close(s.done)
	})
	<-s.closed
	runtime.SetFinalizer(s, nil)
}

// startCancelWatcher spawns a goroutine that sets the cancel flag when ctx
// is canceled. It also records ctx as the current call's context so Go
// functions invoked from Lua can observe cancellation. The returned stop
// function must be called once the Lua operation finishes.
func (s *State) startCancelWatcher(ctx context.Context) (stop func()) {
	C.bridge_set_cancel_flag(s.cancelFlag, 0)
	s.currentCtx = ctx

	stopChan := make(chan struct{})
	doneChan := make(chan struct{})

	go func() {
		defer close(doneChan)
		select {
		case <-ctx.Done():
			C.bridge_set_cancel_flag(s.cancelFlag, 1)
		case <-stopChan:
		}
	}()

	return func() {
		close(stopChan)
		<-doneChan
		C.bridge_set_cancel_flag(s.cancelFlag, 0)
		s.currentCtx = nil
	}
}

// Execute executes a string of Lua code.
func (s *State) Execute(ctx context.Context, code string) error {
	resultChan := make(chan error, 1)

	op := func() {
		stop := s.startCancelWatcher(ctx)
		defer stop()

		cCode := C.CString(code)
		defer C.free(unsafe.Pointer(cCode))

		var err error
		if status := C.bridge_dostring(s.s, cCode); status != C.LUA_OK {
			errStr := C.GoString(C.lua_tolstring(s.s, -1, nil))
			C.bridge_pop(s.s, 1)
			err = fmt.Errorf("lua error: %s", errStr)
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			err = ctxErr
		}
		resultChan <- err
	}

	select {
	case s.opChan <- op:
		return <-resultChan
	case <-ctx.Done():
		return ctx.Err()
	case <-s.closed:
		return ErrStateClosed
	}
}

// GetGlobal gets a global variable from the Lua state.
// Returns (nil, nil) if the global is unset or has the value nil;
// Lua does not distinguish between the two.
func (s *State) GetGlobal(ctx context.Context, name string) (any, error) {
	resultChan := make(chan any, 1)

	op := func() {
		cName := C.CString(name)
		defer C.free(unsafe.Pointer(cName))

		C.lua_getglobal(s.s, cName)
		defer C.bridge_pop(s.s, 1)

		resultChan <- s.toGoValue(-1)
	}

	select {
	case s.opChan <- op:
		return <-resultChan, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.closed:
		return nil, ErrStateClosed
	}
}

// Evaluate executes a string of Lua code and returns its results.
func (s *State) Evaluate(ctx context.Context, code string) ([]any, error) {
	resultChan := make(chan struct {
		results []any
		err     error
	}, 1)

	op := func() {
		stop := s.startCancelWatcher(ctx)
		defer stop()

		cCode := C.CString(code)
		defer C.free(unsafe.Pointer(cCode))

		// Save the current stack top to determine how many values were pushed
		top := C.lua_gettop(s.s)

		sendErr := func(err error) {
			if ctxErr := ctx.Err(); ctxErr != nil {
				err = ctxErr
			}
			resultChan <- struct {
				results []any
				err     error
			}{nil, err}
		}

		// Load the string as a Lua chunk
		status := C.luaL_loadstring(s.s, cCode)
		if status != C.LUA_OK {
			errStr := C.GoString(C.lua_tolstring(s.s, -1, nil))
			C.bridge_pop(s.s, 1) // Pop the error message
			sendErr(fmt.Errorf("lua load error: %s", errStr))
			return
		}

		// Call the loaded chunk (0 arguments, LUA_MULTRET results, 0 message handler)
		status = C.bridge_pcall(s.s, 0, C.LUA_MULTRET, 0)
		if status != C.LUA_OK {
			errStr := C.GoString(C.lua_tolstring(s.s, -1, nil))
			C.bridge_pop(s.s, 1) // Pop the error message
			sendErr(fmt.Errorf("lua runtime error: %s", errStr))
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

		if ctxErr := ctx.Err(); ctxErr != nil {
			resultChan <- struct {
				results []any
				err     error
			}{nil, ctxErr}
			return
		}
		resultChan <- struct {
			results []any
			err     error
		}{results, nil}
	}

	select {
	case s.opChan <- op:
		res := <-resultChan
		return res.results, res.err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.closed:
		return nil, ErrStateClosed
	}
}

// bridgeGoDispatcher is invoked by the C trampoline for every Go
// function called from Lua. It runs on the worker goroutine (same OS
// thread the Lua state is pinned to), so accessing s.goFuncs and
// s.currentCtx without locking is safe.
//
//export bridgeGoDispatcher
func bridgeGoDispatcher(L *C.lua_State) C.int {
	// pushError pushes the error message as a Lua string and returns -1
	// so the C trampoline raises it via lua_error. Raising the error
	// from Go directly would longjmp out of the Go stack and corrupt
	// the Go runtime.
	pushError := func(msg string) C.int {
		cs := C.CString(msg)
		defer C.free(unsafe.Pointer(cs))
		C.bridge_pushlstring(L, cs, C.size_t(len(msg)))
		return -1
	}

	v, ok := stateRegistry.Load(uintptr(unsafe.Pointer(L)))
	if !ok {
		return pushError("lua-go: state not registered")
	}
	s := v.(*State)

	id := int64(C.bridge_upvalue_integer(L, 1))
	fn, ok := s.goFuncs[id]
	if !ok {
		return pushError(fmt.Sprintf("lua-go: unknown Go function id %d", id))
	}

	nargs := int(C.lua_gettop(L))
	args := make([]any, nargs)
	for i := 0; i < nargs; i++ {
		args[i] = s.toGoValue(C.int(i + 1))
	}
	C.bridge_pop(L, C.int(nargs))

	ctx := s.currentCtx
	if ctx == nil {
		ctx = context.Background()
	}

	results, err := fn(ctx, args)
	if err != nil {
		return pushError(err.Error())
	}

	for i, r := range results {
		if pushErr := s.pushGoValue(r); pushErr != nil {
			C.bridge_pop(L, C.int(i)) // clean up what we pushed
			return pushError(fmt.Sprintf("lua-go: return value %d: %v", i, pushErr))
		}
	}
	return C.int(len(results))
}

// Register binds fn to the global Lua name so Lua code can invoke it as
// a function. Replacing an existing binding reuses the same global name
// but creates a fresh internal entry; the previous entry is left dangling
// until Close but is no longer reachable from Lua.
func (s *State) Register(ctx context.Context, name string, fn GoFunc) error {
	if fn == nil {
		return fmt.Errorf("Register: fn must not be nil")
	}

	resultChan := make(chan error, 1)

	op := func() {
		s.nextFuncID++
		id := s.nextFuncID
		s.goFuncs[id] = fn

		C.bridge_push_go_closure(s.s, C.lua_Integer(id))

		cName := C.CString(name)
		defer C.free(unsafe.Pointer(cName))
		C.lua_setglobal(s.s, cName)

		resultChan <- nil
	}

	select {
	case s.opChan <- op:
		return <-resultChan
	case <-ctx.Done():
		return ctx.Err()
	case <-s.closed:
		return ErrStateClosed
	}
}

// Unregister removes a previously registered Go function by clearing the
// Lua global and releasing the Go-side reference. No-op if name is not
// currently bound to a Go function.
func (s *State) Unregister(ctx context.Context, name string) error {
	resultChan := make(chan error, 1)

	op := func() {
		cName := C.CString(name)
		defer C.free(unsafe.Pointer(cName))

		// If the global is a Go closure we registered, drop its entry.
		if C.lua_getglobal(s.s, cName) == C.LUA_TFUNCTION {
			// Walk the upvalues to find the integer funcID we stored.
			// The Go closure's first upvalue is always the ID.
			if C.lua_iscfunction(s.s, -1) != 0 {
				id := int64(C.bridge_upvalue_integer(s.s, 1))
				delete(s.goFuncs, id)
			}
		}
		C.bridge_pop(s.s, 1)

		// Clear the Lua global regardless.
		C.lua_pushnil(s.s)
		C.lua_setglobal(s.s, cName)

		resultChan <- nil
	}

	select {
	case s.opChan <- op:
		return <-resultChan
	case <-ctx.Done():
		return ctx.Err()
	case <-s.closed:
		return ErrStateClosed
	}
}

// Call invokes a global Lua function by name with the given Go arguments
// and returns its results. Supported argument types are: nil, bool,
// int/int8/int16/int32/int64, uint8/uint16/uint32, float32/float64,
// string, []any, and map[any]any. Nested slices/maps are supported
// recursively subject to the same rules.
func (s *State) Call(ctx context.Context, name string, args ...any) ([]any, error) {
	resultChan := make(chan struct {
		results []any
		err     error
	}, 1)

	op := func() {
		stop := s.startCancelWatcher(ctx)
		defer stop()

		sendErr := func(err error) {
			if ctxErr := ctx.Err(); ctxErr != nil {
				err = ctxErr
			}
			resultChan <- struct {
				results []any
				err     error
			}{nil, err}
		}

		top := C.lua_gettop(s.s)

		cName := C.CString(name)
		defer C.free(unsafe.Pointer(cName))

		if C.lua_getglobal(s.s, cName) != C.LUA_TFUNCTION {
			C.bridge_pop(s.s, 1)
			sendErr(fmt.Errorf("global %q is not a function", name))
			return
		}

		for i, arg := range args {
			if err := s.pushGoValue(arg); err != nil {
				// Pop the function plus any args already pushed.
				C.bridge_pop(s.s, C.int(1+i))
				sendErr(fmt.Errorf("arg %d: %w", i, err))
				return
			}
		}

		status := C.bridge_pcall(s.s, C.int(len(args)), C.LUA_MULTRET, 0)
		if status != C.LUA_OK {
			errStr := C.GoString(C.lua_tolstring(s.s, -1, nil))
			C.bridge_pop(s.s, 1)
			sendErr(fmt.Errorf("lua runtime error: %s", errStr))
			return
		}

		numResults := C.lua_gettop(s.s) - top
		results := make([]any, numResults)
		for i := 0; i < int(numResults); i++ {
			results[i] = s.toGoValue(top + C.int(i) + 1)
		}
		C.bridge_pop(s.s, numResults)

		if ctxErr := ctx.Err(); ctxErr != nil {
			resultChan <- struct {
				results []any
				err     error
			}{nil, ctxErr}
			return
		}
		resultChan <- struct {
			results []any
			err     error
		}{results, nil}
	}

	select {
	case s.opChan <- op:
		res := <-resultChan
		return res.results, res.err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.closed:
		return nil, ErrStateClosed
	}
}

// pushGoValue converts a Go value to a Lua value and pushes it onto the
// stack. Must be called from within the locked OS thread. On error, the
// stack is left unchanged (nothing is pushed).
func (s *State) pushGoValue(v any) error {
	switch x := v.(type) {
	case nil:
		C.lua_pushnil(s.s)
	case bool:
		b := C.int(0)
		if x {
			b = 1
		}
		C.bridge_pushboolean(s.s, b)
	case int:
		C.lua_pushinteger(s.s, C.lua_Integer(x))
	case int8:
		C.lua_pushinteger(s.s, C.lua_Integer(x))
	case int16:
		C.lua_pushinteger(s.s, C.lua_Integer(x))
	case int32:
		C.lua_pushinteger(s.s, C.lua_Integer(x))
	case int64:
		C.lua_pushinteger(s.s, C.lua_Integer(x))
	case uint8:
		C.lua_pushinteger(s.s, C.lua_Integer(x))
	case uint16:
		C.lua_pushinteger(s.s, C.lua_Integer(x))
	case uint32:
		C.lua_pushinteger(s.s, C.lua_Integer(x))
	case float32:
		C.lua_pushnumber(s.s, C.lua_Number(x))
	case float64:
		C.lua_pushnumber(s.s, C.lua_Number(x))
	case string:
		cs := C.CString(x)
		defer C.free(unsafe.Pointer(cs))
		C.bridge_pushlstring(s.s, cs, C.size_t(len(x)))
	case []any:
		C.bridge_createtable(s.s, C.int(len(x)), 0)
		for i, elem := range x {
			if err := s.pushGoValue(elem); err != nil {
				C.bridge_pop(s.s, 1) // pop the partially-built table
				return fmt.Errorf("slice index %d: %w", i, err)
			}
			C.bridge_rawseti(s.s, -2, C.lua_Integer(i+1))
		}
	case map[any]any:
		C.bridge_createtable(s.s, 0, C.int(len(x)))
		for k, val := range x {
			if err := s.pushGoValue(k); err != nil {
				C.bridge_pop(s.s, 1)
				return fmt.Errorf("map key: %w", err)
			}
			if err := s.pushGoValue(val); err != nil {
				C.bridge_pop(s.s, 2) // table + key
				return fmt.Errorf("map value: %w", err)
			}
			C.bridge_rawset(s.s, -3)
		}
	default:
		return fmt.Errorf("unsupported Go type: %T", v)
	}
	return nil
}

// FunctionRef is a handle to a Lua function stored in the registry so
// it can be invoked from Go repeatedly without re-loading. Release it
// when no longer needed to free the Lua-side reference; a finalizer
// serves as a best-effort backstop.
type FunctionRef struct {
	state    *State
	ref      C.int
	released atomic.Bool
}

// Call invokes the referenced Lua function with the given Go arguments.
func (f *FunctionRef) Call(ctx context.Context, args ...any) ([]any, error) {
	if f.released.Load() {
		return nil, fmt.Errorf("FunctionRef: already released")
	}
	s := f.state

	resultChan := make(chan struct {
		results []any
		err     error
	}, 1)

	op := func() {
		stop := s.startCancelWatcher(ctx)
		defer stop()

		sendErr := func(err error) {
			if ctxErr := ctx.Err(); ctxErr != nil {
				err = ctxErr
			}
			resultChan <- struct {
				results []any
				err     error
			}{nil, err}
		}

		top := C.lua_gettop(s.s)
		C.bridge_push_ref(s.s, f.ref)

		for i, arg := range args {
			if err := s.pushGoValue(arg); err != nil {
				C.bridge_pop(s.s, C.int(1+i))
				sendErr(fmt.Errorf("arg %d: %w", i, err))
				return
			}
		}

		status := C.bridge_pcall(s.s, C.int(len(args)), C.LUA_MULTRET, 0)
		if status != C.LUA_OK {
			errStr := C.GoString(C.lua_tolstring(s.s, -1, nil))
			C.bridge_pop(s.s, 1)
			sendErr(fmt.Errorf("lua runtime error: %s", errStr))
			return
		}

		numResults := C.lua_gettop(s.s) - top
		results := make([]any, numResults)
		for i := 0; i < int(numResults); i++ {
			results[i] = s.toGoValue(top + C.int(i) + 1)
		}
		C.bridge_pop(s.s, numResults)

		if ctxErr := ctx.Err(); ctxErr != nil {
			resultChan <- struct {
				results []any
				err     error
			}{nil, ctxErr}
			return
		}
		resultChan <- struct {
			results []any
			err     error
		}{results, nil}
	}

	select {
	case s.opChan <- op:
		res := <-resultChan
		return res.results, res.err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.closed:
		return nil, ErrStateClosed
	}
}

// Release drops the Lua-side reference. Subsequent Call returns an
// error. Safe to call multiple times.
func (f *FunctionRef) Release(ctx context.Context) error {
	if !f.released.CompareAndSwap(false, true) {
		return nil
	}
	s := f.state
	ref := f.ref

	done := make(chan struct{})
	op := func() {
		C.bridge_unref(s.s, ref)
		close(done)
	}

	select {
	case s.opChan <- op:
		<-done
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-s.closed:
		// The state's lua_close already released everything.
		return nil
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
		// Lua tables become []any when the keys are exactly 1..N (the
		// "array part" convention) and map[any]any otherwise. An empty
		// table is returned as []any{}; a table with any non-integer
		// key, a gap, or a key outside 1..N falls back to map[any]any.
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
			return []any{}
		}

		return goMap
	case C.LUA_TNIL:
		return nil
	case C.LUA_TFUNCTION:
		ref := C.bridge_ref_value(s.s, idx)
		f := &FunctionRef{state: s, ref: ref}
		runtime.SetFinalizer(f, func(f *FunctionRef) {
			_ = f.Release(context.Background())
		})
		return f
	default:
		// FIXME: support userdata and thread
		return fmt.Sprintf("<unsupported Lua type: %s>", C.GoString(C.lua_typename(s.s, C.lua_type(s.s, idx))))
	}
}
