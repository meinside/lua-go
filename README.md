# lua-go

[![test](https://github.com/meinside/lua-go/actions/workflows/test.yml/badge.svg)](https://github.com/meinside/lua-go/actions/workflows/test.yml)

`lua-go` is a Go package that provides a simple and idiomatic wrapper for
embedding and interacting with the Lua programming language. It allows
Go applications to execute Lua code, evaluate expressions, call Lua
functions with Go arguments, and expose Go functions back to Lua.

## Features

- **Execute Lua code** — run arbitrary Lua source strings.
- **Evaluate expressions** — retrieve multi-value return types.
- **Global variable access** — read Lua globals as Go values.
- **Call Lua functions from Go** — pass Go values as arguments.
- **Register Go functions in Lua** — call Go code from Lua, with
  context propagation and error forwarding.
- **Lua function handles** — Lua functions returned to Go come back as
  a reusable `*FunctionRef` that can be invoked repeatedly.
- **Context-aware cancellation** — a Lua chunk is interrupted when the
  associated `context.Context` is canceled.

## Installation

```bash
go get github.com/meinside/lua-go
```

## Usage

```go
package main

import (
	"context"
	"fmt"
	"log"

	lua "github.com/meinside/lua-go"
)

func main() {
	fmt.Printf("version: %s\n", lua.Version())

	s := lua.NewState()
	defer s.Close()

	ctx := context.Background()

	// Execute Lua code.
	if err := s.Execute(ctx, `
		message = "Hello from Lua!"
		function multiply(a, b) return a * b end
	`); err != nil {
		log.Fatal(err)
	}

	// Read a global.
	msg, err := s.GetGlobal(ctx, "message")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("message: %v\n", msg)
	// Output: message: Hello from Lua!

	// Call a Lua function with Go arguments.
	results, err := s.Call(ctx, "multiply", int64(6), int64(7))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("multiply(6, 7): %v\n", results[0])
	// Output: multiply(6, 7): 42

	// Register a Go function so it is callable from Lua.
	_ = s.Register(ctx, "go_sum", func(_ context.Context, args []any) ([]any, error) {
		a := args[0].(int64)
		b := args[1].(int64)
		return []any{a + b}, nil
	})
	results, _ = s.Evaluate(ctx, `return go_sum(40, 2)`)
	fmt.Printf("go_sum(40, 2): %v\n", results[0])
	// Output: go_sum(40, 2): 42
}
```

## Type conversions

| Lua type      | Go value                                                     |
| ------------- | ------------------------------------------------------------ |
| `nil`         | `nil`                                                        |
| `boolean`     | `bool`                                                       |
| `integer`     | `int64`                                                      |
| `number`      | `float64`                                                    |
| `string`      | `string`                                                     |
| `function`    | `*FunctionRef` (call again via `f.Call(ctx, args...)`)       |
| `table`       | `[]any` when keys are exactly `1..N`, otherwise `map[any]any` (an empty table returns `[]any{}`) |
| other types   | placeholder string; `userdata`/`thread` are not yet supported |

Go values accepted by `Call`, `FunctionRef.Call`, and as `Register`
return values must be one of: `nil`, `bool`, `int` / `int8` / `int16` /
`int32` / `int64`, `uint8` / `uint16` / `uint32`, `float32`, `float64`,
`string`, `[]any`, `map[any]any`. `uint64` is not supported because Lua
integers are signed `int64`.

`GetGlobal` returns `(nil, nil)` if the global is either unset or
explicitly set to `nil` — Lua does not distinguish between the two.

## Cancellation

When the `context.Context` passed to `Execute`, `Evaluate`, `Call`, or
`FunctionRef.Call` is canceled, a Lua debug hook aborts the running
chunk and the call returns `ctx.Err()`. The hook runs every
~1000 Lua VM instructions, so cancellation is best-effort and cannot
interrupt blocking C calls made from within Lua (for example
`io.read`, `os.execute` waiting on a child process). Close the state
or let the chunk finish to unblock those cases.

## Roadmap

- [x] Call Lua functions with Go arguments
- [x] Register Go functions in Lua
- [x] `function` return values (`*FunctionRef`)
- [ ] `userdata` return values
- [ ] `thread` return values
- [ ] Nested-path names for `Register` / `Call` (e.g. `"math.go_sum"`)
- [ ] `uint64` argument support (requires API decision on overflow)
- [ ] Explicit unmarshaling into Go structs/types

`State.Close()` should always be called explicitly. A finalizer is
registered as a best-effort backstop, but because the worker goroutine
holds a reference to the state, the finalizer rarely runs in practice
— forgotten states may leak until process exit.

## License

Released under the MIT License. See [LICENSE.md](LICENSE.md).
