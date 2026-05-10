package main

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
)

func main() {
	rootDir, err := findProjectRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	app := newModel(rootDir)
	finalModel, err := tea.NewProgram(app).Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		app.Shutdown()
		os.Exit(1)
	}

	if finalApp, ok := finalModel.(*model); ok {
		finalApp.Shutdown()
		return
	}

	app.Shutdown()
}
