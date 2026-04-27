// lua.go

// Package lua provides a wrapper for running Lua codes.
package lua

import (
	"context"

	"github.com/meinside/lua-go/luasrc"
)

// Version returns the Lua version string (e.g., "Lua 5.4.8").
func Version() string {
	return luasrc.Version()
}

// State wraps the low-level Lua state.
type State struct {
	s *luasrc.State
}

// NewState creates a new Lua state.
func NewState() *State {
	return &State{s: luasrc.NewState()}
}

// Close closes the Lua state.
func (s *State) Close() {
	s.s.Close()
}

// Execute executes a string of Lua code.
func (s *State) Execute(ctx context.Context, code string) error {
	return s.s.Execute(ctx, code)
}

// GetGlobal gets a global variable from the Lua state.
// Returns (nil, nil) if the global is unset or has the value nil;
// Lua does not distinguish between the two.
func (s *State) GetGlobal(ctx context.Context, name string) (any, error) {
	return s.s.GetGlobal(ctx, name)
}

// Evaluate evaluates a string of Lua code and returns its results.
func (s *State) Evaluate(ctx context.Context, code string) ([]any, error) {
	return s.s.Evaluate(ctx, code)
}

// Call invokes a global Lua function by name with the given Go arguments
// and returns its results. See luasrc.State.Call for supported argument
// types.
func (s *State) Call(ctx context.Context, name string, args ...any) ([]any, error) {
	return s.s.Call(ctx, name, args...)
}

// GoFunc is the signature for a Go function callable from Lua.
type GoFunc = luasrc.GoFunc

// FunctionRef is a handle to a Lua function. See luasrc.FunctionRef.
type FunctionRef = luasrc.FunctionRef

// Register binds fn to the given global Lua name.
func (s *State) Register(ctx context.Context, name string, fn GoFunc) error {
	return s.s.Register(ctx, name, fn)
}

// Unregister clears the Lua global previously bound by Register.
func (s *State) Unregister(ctx context.Context, name string) error {
	return s.s.Unregister(ctx, name)
}
