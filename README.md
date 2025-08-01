# lua-go

`lua-go` is a Go package that provides a simple and idiomatic wrapper for embedding and interacting with the Lua programming language. It allows Go applications to execute Lua code, manipulate Lua global variables, and evaluate Lua expressions, bridging the gap between Go and Lua.

## Features

- **Execute Lua Code**: Run arbitrary Lua code strings directly from Go.
- **Global Variable Access**: Get global variables from the Lua state, supporting various Lua types (string, number, boolean, nil).
- **Evaluate Lua Expressions**: Evaluate Lua code and retrieve multiple return values.

## Installation

To install `lua-go`, use `go get`:

```bash
go get github.com/meinside/lua-go
```

## Usage

Here's a basic example of how to use `lua-go` in your Go application:

```go
package main

import (
	"fmt"
	"log"

	"github.com/meinside/lua-go"
)

func main() {
	// Create a new Lua state
	s := lua.NewState()
	defer s.Close() // Ensure the Lua state is closed when done

	// Execute some Lua code
	err := s.Execute(`
		message = "Hello from Lua!"
		x = 10
		y = 20
		sum = x + y
		function multiply(a, b)
			return a * b
		end
	`)
	if err != nil {
		log.Fatalf("Error executing Lua code: %v", err)
	}

	// Get global variables
	fmt.Printf("message: %v\n", s.GetGlobal("message")) // Output: Hello from Lua!
	fmt.Printf("sum: %v\n", s.GetGlobal("sum"))         // Output: 30

	// Evaluate a Lua expression (calling a function)
	results, err := s.Evaluate(`return multiply(5, 6)`)
	if err != nil {
		log.Fatalf("Error evaluating Lua expression: %v", err)
	}
	if len(results) > 0 {
		fmt.Printf("multiply(5, 6): %v\n", results[0]) // Output: 30
	}

	// Evaluate an expression with multiple return values
	results, err = s.Evaluate(`return "apple", 123, true`)
	if err != nil {
		log.Fatalf("Error evaluating Lua expression: %v", err)
	}
	fmt.Printf("Multiple results: %v\n", results) // Output: [apple 123 true]
}
```

## License

This project is licensed under the MIT License - see the [LICENSE.md](LICENSE.md) file for details.

