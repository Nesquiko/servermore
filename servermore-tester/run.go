package servermoretester

import tea "charm.land/bubbletea/v2"

func Run() error {
	rootDir, err := findProjectRoot()
	if err != nil {
		return err
	}

	app := newModel(rootDir)
	_, err = tea.NewProgram(app).Run()
	app.Shutdown()
	return err
}
