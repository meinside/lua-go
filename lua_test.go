package lua

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestVersion tests the Version function.
func TestVersion(t *testing.T) {
	if ver := Version(); len(ver) <= 0 {
		t.Errorf("Version returned an empty string")
	}
}

// TestNewStateAndClose creates a new Lua state and then closes it.
func TestNewStateAndClose(t *testing.T) {
	s := NewState()
	if s == nil || s.s == nil {
		t.Fatal("NewState() failed to create a new Lua state.")
	}
	s.Close()
}

// TestCloseIsIdempotent verifies Close can be called multiple times
// without panicking, and that subsequent callers also wait for teardown.
func TestCloseIsIdempotent(t *testing.T) {
	s := NewState()
	s.Close()
	s.Close()
	s.Close()
}

// TestCallsAfterCloseDoNotBlock verifies that Execute/Evaluate/GetGlobal
// called on a closed state return promptly instead of deadlocking on the
// unbuffered opChan.
func TestCallsAfterCloseDoNotBlock(t *testing.T) {
	s := NewState()
	s.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = s.Execute(context.Background(), `a = 1`)
		_, _ = s.Evaluate(context.Background(), `return 1`)
		_, _ = s.GetGlobal(context.Background(), "a")
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("calls on a closed state blocked instead of returning")
	}
}

// TestCloseAbortsRunningLua verifies Close returns promptly even when a
// long-running Lua chunk is in progress — the cancel hook is signaled so
// the worker goroutine unblocks quickly.
func TestCloseAbortsRunningLua(t *testing.T) {
	s := NewState()

	go func() {
		_ = s.Execute(context.Background(), `while true do end`)
	}()

	// Give the worker a moment to enter the Lua chunk.
	time.Sleep(20 * time.Millisecond)

	done := make(chan struct{})
	start := time.Now()
	go func() {
		s.Close()
		close(done)
	}()

	select {
	case <-done:
		if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
			t.Fatalf("Close took %v; cancel hook did not abort the running chunk", elapsed)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not return within 2s; the worker is stuck inside Lua")
	}
}

// TestExecute executes simple Lua scripts.
func TestExecute(t *testing.T) {
	s := NewState()
	defer s.Close()

	ctx := context.Background()

	err := s.Execute(ctx, `a = 10`)
	if err != nil {
		t.Errorf("Execute failed with error: %v", err)
	}

	err = s.Execute(ctx, `a = b c`) // Invalid Lua syntax
	if err == nil {
		t.Error("Execute should have returned an error for invalid syntax, but it didn't.")
	}
}

// TestGetGlobal tests the GetGlobal function.
func TestGetGlobal(t *testing.T) {
	s := NewState()
	defer s.Close()

	ctx := context.Background()

	err := s.Execute(
		ctx,
		`
		my_string = "hello"
		my_int = 42
		my_float = 3.14
		my_bool = true
		my_nil = nil
	`,
	)
	if err != nil {
		t.Fatalf("Execute failed with error: %v", err)
	}

	// Test string
	if val, err := s.GetGlobal(ctx, "my_string"); err != nil || val.(string) != "hello" {
		t.Errorf(`GetGlobal("my_string") = (%v, %v), want ("hello", nil)`, val, err)
	}

	// Test integer
	if val, err := s.GetGlobal(ctx, "my_int"); err != nil || val.(int64) != 42 {
		t.Errorf(`GetGlobal("my_int") = (%v, %v), want (42, nil)`, val, err)
	}

	// Test float
	if val, err := s.GetGlobal(ctx, "my_float"); err != nil || val.(float64) != 3.14 {
		t.Errorf(`GetGlobal("my_float") = (%v, %v), want (3.14, nil)`, val, err)
	}

	// Test boolean
	if val, err := s.GetGlobal(ctx, "my_bool"); err != nil || val.(bool) != true {
		t.Errorf(`GetGlobal("my_bool") = (%v, %v), want (true, nil)`, val, err)
	}

	// Test nil
	if val, err := s.GetGlobal(ctx, "my_nil"); err != nil || val != nil {
		t.Errorf(`GetGlobal("my_nil") = (%v, %v), want (nil, nil)`, val, err)
	}

	// Test non-existent global
	if val, err := s.GetGlobal(ctx, "non_existent"); err != nil || val != nil {
		t.Errorf(`GetGlobal("non_existent") = (%v, %v), want (nil, nil)`, val, err)
	}
}

// TestEvaluate tests the Evaluate function.
func TestEvaluate(t *testing.T) {
	s := NewState()
	defer s.Close()

	ctx := context.Background()

	// Test single return value (number)
	results, err := s.Evaluate(ctx, `return 123`)
	if err != nil {
		t.Fatalf("Evaluate failed with error: %v", err)
	}
	if len(results) != 1 || results[0].(int64) != 123 {
		t.Errorf(`Evaluate("return 123") = %v, want [123]`, results)
	}

	// Test single return value (string)
	results, err = s.Evaluate(ctx, `return 'hello'`)
	if err != nil {
		t.Fatalf("Evaluate failed with error: %v", err)
	}
	if len(results) != 1 || results[0].(string) != "hello" {
		t.Errorf(`Evaluate("return 'hello'") = %v, want ["hello"]`, results)
	}

	// Test multiple return values
	results, err = s.Evaluate(ctx, `return 1, 'two', true, {1, 2, 3}, {a = "a", b = "b"}`)
	if err != nil {
		t.Fatalf("Evaluate failed with error: %v", err)
	}
	if len(results) != 5 ||
		results[0].(int64) != 1 ||
		results[1].(string) != "two" ||
		results[2].(bool) != true ||
		len(results[3].([]any)) != 3 ||
		len(results[4].(map[any]any)) != 2 {
		t.Errorf(`Evaluate("return 1, 'two', true, {1, 2, 3}, {a = \"a\", b = \"b\"") = %v, want [1, "two", true, [1, 2, 3], {a: "a", b: "b"}]`, results)
	}

	// Test nil return value
	results, err = s.Evaluate(ctx, `return nil`)
	if err != nil {
		t.Fatalf("Evaluate failed with error: %v", err)
	}
	if len(results) != 1 || results[0] != nil {
		t.Errorf(`Evaluate("return nil") = %v, want [nil]`, results)
	}

	// Test no return value (side effect)
	results, err = s.Evaluate(ctx, `a = 10`)
	if err != nil {
		t.Fatalf("Evaluate failed with error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf(`Evaluate("a = 10") = %v, want []`, results)
	}

	// Test runtime error
	_, err = s.Evaluate(ctx, `error('test error')`)
	if err == nil {
		t.Error("Evaluate should have returned an error for runtime error, but it didn't.")
	}

	// Test syntax error
	_, err = s.Evaluate(ctx, "a = b c")
	if err == nil {
		t.Error("Evaluate should have returned an error for syntax error, but it didn't.")
	}
}

// TestLuaFunctionCall tests calling a Lua function from Go.
func TestLuaFunctionCall(t *testing.T) {
	s := NewState()
	defer s.Close()

	ctx := context.Background()

	// Define a Lua function
	err := s.Execute(
		ctx,
		`
		function add(a, b)
			return a + b
		end
	`)
	if err != nil {
		t.Fatalf("Failed to define Lua function: %v", err)
	}

	// Call the Lua function and get results
	results, err := s.Evaluate(ctx, `return add(5, 3)`)
	if err != nil {
		t.Fatalf("Failed to call Lua function: %v", err)
	}

	// Verify the result
	if len(results) != 1 || results[0].(int64) != 8 {
		t.Errorf("Expected add(5, 3) to return 8, got %v", results)
	}

	// Test with different types
	results, err = s.Evaluate(ctx, `return add(10.5, 2.5)`)
	if err != nil {
		t.Fatalf("Failed to call Lua function with floats: %v", err)
	}
	if len(results) != 1 || results[0].(float64) != 13.0 {
		t.Errorf("Expected add(10.5, 2.5) to return 13.0, got %v", results)
	}

	// Test calling a non-existent function
	_, err = s.Evaluate(ctx, `return nonExistentFunction()`)
	if err == nil {
		t.Error("Expected error for calling non-existent function, got nil")
	}
}

// TestCall exercises Call with a variety of Go argument types and
// verifies the arguments are passed through to Lua unmodified.
func TestCall(t *testing.T) {
	s := NewState()
	defer s.Close()

	ctx := context.Background()

	err := s.Execute(ctx, `
		function echo(...) return ... end
		function sum(t)
			local total = 0
			for _, v in ipairs(t) do total = total + v end
			return total
		end
		function lookup(t, k) return t[k] end
	`)
	if err != nil {
		t.Fatalf("setup Execute failed: %v", err)
	}

	// Scalars round-trip.
	results, err := s.Call(ctx, "echo", nil, true, int64(42), 3.14, "hi")
	if err != nil {
		t.Fatalf("Call echo failed: %v", err)
	}
	if len(results) != 5 ||
		results[0] != nil ||
		results[1].(bool) != true ||
		results[2].(int64) != 42 ||
		results[3].(float64) != 3.14 ||
		results[4].(string) != "hi" {
		t.Errorf("echo round-trip mismatch: %v", results)
	}

	// Slice argument becomes a 1-based table on the Lua side.
	results, err = s.Call(ctx, "sum", []any{int64(1), int64(2), int64(3), int64(4)})
	if err != nil {
		t.Fatalf("Call sum failed: %v", err)
	}
	if len(results) != 1 || results[0].(int64) != 10 {
		t.Errorf("sum returned %v, want 10", results)
	}

	// Map argument with a string key.
	results, err = s.Call(ctx, "lookup", map[any]any{"name": "alice", "age": int64(30)}, "name")
	if err != nil {
		t.Fatalf("Call lookup failed: %v", err)
	}
	if len(results) != 1 || results[0].(string) != "alice" {
		t.Errorf("lookup returned %v, want alice", results)
	}

	// Unsupported Go type surfaces as an error without crashing the state.
	_, err = s.Call(ctx, "echo", make(chan int))
	if err == nil {
		t.Error("expected error for unsupported arg type, got nil")
	}

	// Non-function global.
	if err := s.Execute(ctx, `notafunc = 7`); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	_, err = s.Call(ctx, "notafunc")
	if err == nil {
		t.Error("expected error for non-function global, got nil")
	}
}

// TestRegister verifies Go functions can be called from Lua, including
// argument passthrough, multi-value returns, and error propagation.
func TestRegister(t *testing.T) {
	s := NewState()
	defer s.Close()

	ctx := context.Background()

	addCalls := 0
	err := s.Register(ctx, "go_add", func(_ context.Context, args []any) ([]any, error) {
		addCalls++
		a := args[0].(int64)
		b := args[1].(int64)
		return []any{a + b}, nil
	})
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	results, err := s.Evaluate(ctx, `return go_add(2, 3)`)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if len(results) != 1 || results[0].(int64) != 5 {
		t.Errorf("go_add(2,3) = %v, want 5", results)
	}
	if addCalls != 1 {
		t.Errorf("Go function called %d times, want 1", addCalls)
	}

	// Multiple return values.
	err = s.Register(ctx, "go_pair", func(_ context.Context, _ []any) ([]any, error) {
		return []any{"first", int64(2)}, nil
	})
	if err != nil {
		t.Fatalf("Register go_pair failed: %v", err)
	}
	results, err = s.Evaluate(ctx, `local a, b = go_pair() return a, b`)
	if err != nil {
		t.Fatalf("Evaluate go_pair failed: %v", err)
	}
	if len(results) != 2 || results[0].(string) != "first" || results[1].(int64) != 2 {
		t.Errorf("go_pair returned %v", results)
	}

	// Error propagation: returning an error surfaces as a Lua runtime error
	// that pcall can catch.
	err = s.Register(ctx, "go_fail", func(_ context.Context, _ []any) ([]any, error) {
		return nil, fmt.Errorf("boom")
	})
	if err != nil {
		t.Fatalf("Register go_fail failed: %v", err)
	}
	results, err = s.Evaluate(ctx, `local ok, err = pcall(go_fail) return ok, err`)
	if err != nil {
		t.Fatalf("Evaluate go_fail failed: %v", err)
	}
	if len(results) != 2 || results[0].(bool) != false {
		t.Fatalf("pcall(go_fail) ok=%v, want false", results[0])
	}
	if msg, _ := results[1].(string); !strings.Contains(msg, "boom") {
		t.Errorf("pcall error message %q does not contain %q", results[1], "boom")
	}
}

// TestRegisterContextPropagation verifies that a Go function invoked
// from Lua receives the context of the enclosing Lua call.
func TestRegisterContextPropagation(t *testing.T) {
	s := NewState()
	defer s.Close()

	var gotCtx context.Context
	_ = s.Register(context.Background(), "grab_ctx", func(ctx context.Context, _ []any) ([]any, error) {
		gotCtx = ctx
		return nil, nil
	})

	type ctxKey struct{}
	callerCtx := context.WithValue(context.Background(), ctxKey{}, "tag")
	_, err := s.Evaluate(callerCtx, `grab_ctx()`)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if gotCtx == nil {
		t.Fatal("Go function received nil context")
	}
	if v := gotCtx.Value(ctxKey{}); v != "tag" {
		t.Errorf("Go function received ctx value %v, want 'tag'", v)
	}
}

// TestUnregister verifies Unregister clears the binding.
func TestUnregister(t *testing.T) {
	s := NewState()
	defer s.Close()

	ctx := context.Background()
	_ = s.Register(ctx, "f", func(_ context.Context, _ []any) ([]any, error) {
		return []any{int64(1)}, nil
	})
	if err := s.Unregister(ctx, "f"); err != nil {
		t.Fatalf("Unregister failed: %v", err)
	}
	results, err := s.Evaluate(ctx, `return type(f)`)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if len(results) != 1 || results[0].(string) != "nil" {
		t.Errorf("type(f) after Unregister = %v, want 'nil'", results)
	}
}

// TestFunctionRef verifies that a Lua function returned from Evaluate
// comes back as a reusable handle.
func TestFunctionRef(t *testing.T) {
	s := NewState()
	defer s.Close()

	ctx := context.Background()

	results, err := s.Evaluate(ctx, `return function(a, b) return a + b, a * b end`)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	f, ok := results[0].(*FunctionRef)
	if !ok {
		t.Fatalf("expected *FunctionRef, got %T", results[0])
	}

	out, err := f.Call(ctx, int64(3), int64(4))
	if err != nil {
		t.Fatalf("f.Call failed: %v", err)
	}
	if len(out) != 2 || out[0].(int64) != 7 || out[1].(int64) != 12 {
		t.Errorf("f(3,4) = %v, want [7 12]", out)
	}

	// Call again to confirm the handle is reusable.
	out, err = f.Call(ctx, int64(10), int64(2))
	if err != nil {
		t.Fatalf("second f.Call failed: %v", err)
	}
	if out[0].(int64) != 12 || out[1].(int64) != 20 {
		t.Errorf("f(10,2) = %v, want [12 20]", out)
	}

	if err := f.Release(ctx); err != nil {
		t.Fatalf("Release failed: %v", err)
	}
	if err := f.Release(ctx); err != nil {
		t.Fatalf("second Release failed: %v", err)
	}
	if _, err := f.Call(ctx, int64(1)); err == nil {
		t.Error("Call after Release should return an error")
	}
}

// TestContextTimeout tests that Lua execution respects context timeouts.
func TestContextTimeout(t *testing.T) {
	s := NewState()
	defer s.Close()

	// Create a context with a short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Execute a Lua script that runs for longer than the timeout
	err := s.Execute(ctx, `
		local start_time = os.clock()
		while (os.clock() - start_time < 1) do end -- Loop for 1 second
	`)

	// Expect a context cancellation error
	if err == nil {
		t.Error("Expected context.DeadlineExceeded error, but got nil")
	} else if err != context.DeadlineExceeded && err != context.Canceled {
		t.Errorf("Expected context.DeadlineExceeded or context.Canceled, but got %v", err)
	}
}

// TestContextTimeoutAbortsLua verifies that a context timeout actually
// aborts the running Lua chunk, not just the Go-side wait. If the hook
// fails to interrupt Lua, the worker goroutine stays stuck inside the
// previous Execute call and the subsequent Evaluate blocks well beyond
// the 200ms budget.
func TestContextTimeoutAbortsLua(t *testing.T) {
	s := NewState()
	defer s.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_ = s.Execute(ctx, `while true do end`)

	start := time.Now()
	results, err := s.Evaluate(context.Background(), `return 1 + 1`)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("follow-up Evaluate failed: %v", err)
	}
	if len(results) != 1 || results[0].(int64) != 2 {
		t.Fatalf("follow-up Evaluate returned %v, want [2]", results)
	}
	if elapsed > 200*time.Millisecond {
		t.Fatalf("follow-up Evaluate took %v; worker was stuck in the canceled Lua chunk", elapsed)
	}
}
