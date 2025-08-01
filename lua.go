// lua.go

// Package lua provides a wrapper for running Lua codes.
package lua

import (
	"context"

	"github.com/meinside/lua-go/luasrc"
)

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
func (s *State) GetGlobal(ctx context.Context, name string) any {
	return s.s.GetGlobal(ctx, name)
}

// Evaluate evaluates a string of Lua code and returns its results.
func (s *State) Evaluate(ctx context.Context, code string) ([]any, error) {
	return s.s.Evaluate(ctx, code)
}
