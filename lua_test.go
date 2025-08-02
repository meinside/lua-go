package lua

import (
	"context"
	"testing"
	"time"
)

// TestNewStateAndClose creates a new Lua state and then closes it.
func TestNewStateAndClose(t *testing.T) {
	s := NewState()
	if s == nil || s.s == nil {
		t.Fatal("NewState() failed to create a new Lua state.")
	}
	s.Close()
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
	if val := s.GetGlobal(ctx, "my_string"); val.(string) != "hello" {
		t.Errorf(`GetGlobal("my_string") = %v, want "hello"`, val)
	}

	// Test integer
	if val := s.GetGlobal(ctx, "my_int"); val.(int64) != 42 {
		t.Errorf(`GetGlobal("my_int") = %v, want 42`, val)
	}

	// Test float
	if val := s.GetGlobal(ctx, "my_float"); val.(float64) != 3.14 {
		t.Errorf(`GetGlobal("my_float") = %v, want 3.14`, val)
	}

	// Test boolean
	if val := s.GetGlobal(ctx, "my_bool"); val.(bool) != true {
		t.Errorf(`GetGlobal("my_bool") = %v, want true`, val)
	}

	// Test nil
	if val := s.GetGlobal(ctx, "my_nil"); val != nil {
		t.Errorf(`GetGlobal("my_nil") = %v, want nil`, val)
	}

	// Test non-existent global
	if val := s.GetGlobal(ctx, "non_existent"); val != nil {
		t.Errorf(`GetGlobal("non_existent") = %v, want nil`, val)
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
		t.Error("Evaluate should have returned an error for runtime error, but it't.")
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
