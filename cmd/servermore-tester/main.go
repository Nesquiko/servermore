package main

import (
	"fmt"
	"os"

	servermoretester "github.com/Nesquiko/servermore/servermore-tester"
)

func main() {
	if err := servermoretester.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
