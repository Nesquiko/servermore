// Runtime assertion to catch unexpected invariants
package assert

import "fmt"

// That checks if trueCond is true and if yes panics with the message
func That(trueCond bool, format string, args ...any) {
	if !trueCond {
		panic(fmt.Sprintf(format, args...))
	}
}

func NoError(err error) {
	That(err == nil, "error was not nil, %v", err)
}
