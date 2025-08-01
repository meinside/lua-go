// lua.go

// Package lua provides a wrapper for running Lua codes.
package lua

import "github.com/meinside/lua-go/luasrc"

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
func (s *State) Execute(code string) error {
	return s.s.Execute(code)
}

// GetGlobal gets a global variable from the Lua state.
func (s *State) GetGlobal(name string) any {
	return s.s.GetGlobal(name)
}

// Evaluate evaluates a string of Lua code and returns its results.
func (s *State) Evaluate(code string) ([]any, error) {
	return s.s.Evaluate(code)
}
