package servermoretester

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	huh "charm.land/huh/v2"
	lipgloss "charm.land/lipgloss/v2"
)

type phase int

const (
	phaseSetup phase = iota
	phaseDashboard
	phaseQuitting
)

var (
	spinnerFrames = []string{"|", "/", "-", "\\"}
	appBorder     = lipgloss.Border{
		Top:          "-",
		Bottom:       "-",
		Left:         "|",
		Right:        "|",
		TopLeft:      "+",
		TopRight:     "+",
		BottomLeft:   "+",
		BottomRight:  "+",
		MiddleLeft:   "|",
		MiddleRight:  "|",
		Middle:       "-",
		MiddleTop:    "+",
		MiddleBottom: "+",
	}
)

type uiTickMsg time.Time
type stackPollTickMsg time.Time
type dashboardTickMsg time.Time

type functionEntry struct {
	requester   Requester
	binaryPath  string
	deploying   bool
	deployError string
	deployed    *deploymentState
}

type model struct {
	rootDir string
	phase   phase

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	width  int
	height int

	spinnerIndex int
	setupStatus  string
	setupOutput  string
	setupStream  *rollingLog
	setupErr     error
	setupPolls   int

	dozzleContainerID string
	dozzleURL         string
	dozzleStarting    bool
	dozzleError       string
	grafanaURL        string

	functions        []*functionEntry
	selectedFunction int
	selectedField    int
	statusLine       string
	stackStarted     bool
	stackStarting    bool
	shuttingDown     bool

	deployFunctionIndex int
	deployName          string
	deployForm          *huh.Form
}

func newModel(rootDir string) *model {
	ctx, cancel := context.WithCancel(context.Background())

	requesters := catalog()
	functions := make([]*functionEntry, 0, len(requesters))
	for _, requester := range requesters {
		functions = append(functions, &functionEntry{requester: requester})
	}

	return &model{
		rootDir:             rootDir,
		phase:               phaseSetup,
		ctx:                 ctx,
		cancel:              cancel,
		width:               120,
		height:              40,
		setupStatus:         "Compiling testing functions...",
		functions:           functions,
		selectedFunction:    0,
		selectedField:       0,
		statusLine:          "Preparing the local test stack...",
		grafanaURL:          "http://127.0.0.1:3000",
		deployFunctionIndex: -1,
	}
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(uiTickCmd(), compileFunctionsCmd(m.ctx, m.rootDir, m.requesters()))
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg.(type) {
	case uiTickMsg:
		if m.phase == phaseSetup {
			m.spinnerIndex = (m.spinnerIndex + 1) % len(spinnerFrames)
			return m, uiTickCmd()
		}
		return m, nil
	case dashboardTickMsg:
		if m.phase == phaseDashboard {
			return m, dashboardTickCmd()
		}
		return m, nil
	}

	if m.phase == phaseQuitting {
		if msg, ok := msg.(stackShutdownDoneMsg); ok {
			if msg.err != nil {
				m.statusLine = "Compose stack shutdown failed."
			} else {
				m.statusLine = "Compose stack is down."
			}
			return m, tea.Quit
		}
		return m, nil
	}

	if m.deployForm != nil {
		if key, ok := msg.(tea.KeyPressMsg); ok {
			switch key.String() {
			case "ctrl+c", "q":
				return m.beginShutdown()
			case "esc":
				m.deployForm = nil
				m.deployFunctionIndex = -1
				m.statusLine = "Deployment cancelled."
				return m, nil
			}
		}

		form, cmd := m.deployForm.Update(msg)
		if nextForm, ok := form.(*huh.Form); ok {
			m.deployForm = nextForm
		}
		if m.deployForm != nil && m.deployForm.State == huh.StateCompleted {
			name := strings.TrimSpace(m.deployForm.GetString("name"))
			functionIndex := m.deployFunctionIndex
			m.deployForm = nil
			m.deployFunctionIndex = -1
			m.functions[functionIndex].deploying = true
			m.functions[functionIndex].deployError = ""
			m.statusLine = fmt.Sprintf(
				"Deploying %s...",
				m.functions[functionIndex].requester.BinaryName(),
			)
			return m, tea.Batch(cmd, deployFunctionCmd(
				m.ctx,
				functionIndex,
				m.functions[functionIndex].binaryPath,
				name,
			))
		}
		return m, cmd
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = maxInt(80, msg.Width)
		m.height = maxInt(24, msg.Height)
		return m, nil
	case compileDoneMsg:
		m.setupOutput = msg.output
		m.setupStream = newRollingLog(10)
		m.stackStarting = true
		if msg.err != nil {
			m.setupErr = msg.err
			m.statusLine = "Compilation failed."
			m.stackStarting = false
			return m, nil
		}
		for _, function := range m.functions {
			function.binaryPath = msg.binaries[function.requester.BinaryName()]
		}
		m.setupStatus = "Starting the Servermore stack..."
		m.statusLine = "Building containers and waiting for the stack to answer..."
		return m, startStackCmd(m.ctx, m.rootDir, m.setupStream)
	case stackReadyMsg:
		m.setupOutput = msg.output
		m.stackStarted = msg.started
		m.stackStarting = false
		m.setupStream = nil
		if msg.err != nil {
			m.setupErr = msg.err
			m.statusLine = "Stack startup failed."
			return m, nil
		}
		m.statusLine = "Stack started. Waiting for commander and gateway..."
		m.setupPolls = 0
		return m, pollStackCmd(m.ctx, m.rootDir)
	case stackPollMsg:
		if strings.TrimSpace(msg.output) != "" {
			m.setupOutput = msg.output
		}
		if msg.err != nil {
			m.setupErr = msg.err
			m.statusLine = "Stack log polling failed."
			return m, nil
		}
		if msg.commanderReady && msg.gatewayReady {
			m.phase = phaseDashboard
			m.statusLine = "Stack is ready. Press Enter to deploy the selected function."
			return m, dashboardTickCmd()
		}
		m.setupPolls++
		m.statusLine = fmt.Sprintf(
			"Waiting for services... commander=%t gateway=%t",
			msg.commanderReady,
			msg.gatewayReady,
		)
		if m.setupPolls >= 240 {
			m.setupErr = fmt.Errorf("stack readiness timed out after 2 minutes")
			m.statusLine = "Stack startup timed out."
			return m, nil
		}
		return m, tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
			return stackPollTickMsg(t)
		})
	case stackPollTickMsg:
		return m, pollStackCmd(m.ctx, m.rootDir)
	case dozzleStartMsg:
		m.dozzleStarting = false
		if msg.err != nil {
			m.dozzleError = msg.err.Error()
			m.statusLine = "Failed to start Dozzle."
			return m, nil
		}
		m.dozzleContainerID = msg.containerID
		m.dozzleURL = msg.url
		m.dozzleError = ""
		m.statusLine = "Dozzle is running."
		return m, nil
	case deployDoneMsg:
		function := m.functions[msg.functionIndex]
		function.deploying = false
		if msg.err != nil {
			function.deployError = msg.err.Error()
			m.statusLine = fmt.Sprintf("Deploy %s failed.", function.requester.BinaryName())
			return m, nil
		}

		workerCtx, workerCancel := context.WithCancel(m.ctx)
		deployed := newDeploymentState(msg.name, msg.functionID, workerCancel)
		function.deployed = deployed
		function.deployError = ""
		startWorker(workerCtx, &m.wg, function.requester, deployed)
		m.statusLine = fmt.Sprintf(
			"%s deployed as %q with function id %s.",
			function.requester.BinaryName(),
			msg.name,
			msg.functionID,
		)
		return m, nil
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m *model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m.beginShutdown()
	}

	if m.phase == phaseSetup {
		return m, nil
	}

	switch msg.String() {
	case "up", "k":
		m.selectedField = clampInt(m.selectedField-1, 0, m.selectedFunctionFieldCount()-1)
	case "down", "j":
		m.selectedField = clampInt(m.selectedField+1, 0, m.selectedFunctionFieldCount()-1)
	case "d":
		if m.dozzleContainerID == "" && !m.dozzleStarting {
			m.dozzleStarting = true
			m.dozzleError = ""
			m.statusLine = "Starting Dozzle..."
			return m, startDozzleCmd(m.ctx, m.rootDir)
		}
	case "left", "h":
		m.selectedFunction = clampInt(m.selectedFunction-1, 0, len(m.functions)-1)
		m.normalizeSelectedField()
	case "right", "l":
		m.selectedFunction = clampInt(m.selectedFunction+1, 0, len(m.functions)-1)
		m.normalizeSelectedField()
	case "0":
		if deployed := m.selectedDeployment(); deployed != nil {
			enabled := deployed.ToggleEnabled()
			if enabled {
				m.statusLine = "Requests enabled."
			} else {
				m.statusLine = "Requests disabled."
			}
		}
	case "+", "=":
		if deployed := m.selectedDeployment(); deployed != nil {
			deployed.AdjustSetting(m.selectedField, 1)
		}
	case "-", "_":
		if deployed := m.selectedDeployment(); deployed != nil {
			deployed.AdjustSetting(m.selectedField, -1)
		}
	case "enter":
		selected := m.functions[m.selectedFunction]
		if selected.deployed == nil && !selected.deploying {
			m.openDeployForm(m.selectedFunction)
			return m, m.deployForm.Init()
		}
	}

	return m, nil
}

func (m *model) View() tea.View {
	styles := newStyles()

	if m.phase == phaseQuitting {
		return tea.NewView(m.renderQuittingView(styles))
	}
	if m.deployForm != nil {
		return tea.NewView(m.renderDeployView(styles))
	}
	if m.phase == phaseSetup {
		return tea.NewView(m.renderSetupView(styles))
	}
	return tea.NewView(m.renderDashboardView(styles))
}

func (m *model) Shutdown() {
	m.cancel()
	for _, function := range m.functions {
		if function.deployed != nil {
			function.deployed.Stop()
		}
	}
	m.wg.Wait()

	if !m.stackStarted {
		return
	}

	cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if m.dozzleContainerID != "" {
		_, _, _ = runCommand(cleanupCtx, m.rootDir, "docker", "rm", "-f", m.dozzleContainerID)
	}
	_ = stopStack(cleanupCtx, m.rootDir)
}

func (m *model) beginShutdown() (tea.Model, tea.Cmd) {
	if m.shuttingDown {
		return m, nil
	}
	m.shuttingDown = true
	m.phase = phaseQuitting
	m.statusLine = "Compose stack is going down..."
	m.cancel()
	return m, shutdownStackCmd(m.rootDir)
}

func (m *model) requesters() []Requester {
	requesters := make([]Requester, 0, len(m.functions))
	for _, function := range m.functions {
		requesters = append(requesters, function.requester)
	}
	return requesters
}

func (m *model) selectedDeployment() *deploymentState {
	return m.functions[m.selectedFunction].deployed
}

func (m *model) selectedFunctionFieldCount() int {
	if m.selectedDeployment() == nil {
		return 1
	}
	return 3
}

func (m *model) normalizeSelectedField() {
	m.selectedField = clampInt(m.selectedField, 0, m.selectedFunctionFieldCount()-1)
}

func (m *model) openDeployForm(functionIndex int) {
	function := m.functions[functionIndex]
	m.deployFunctionIndex = functionIndex
	m.deployName = function.requester.SuggestedName()
	m.deployForm = huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Key("name").
				Title("Function name").
				Description("Commander stores this as the human-readable function name.").
				Value(&m.deployName).
				Validate(func(value string) error {
					if strings.TrimSpace(value) == "" {
						return errors.New("function name is required")
					}
					return nil
				}),
		),
	).
		WithWidth(minInt(72, maxInt(48, m.width-10))).
		WithShowHelp(false)
	m.statusLine = fmt.Sprintf(
		"Deploying %s. Press Esc to cancel.",
		function.requester.BinaryName(),
	)
}

func (m *model) renderSetupView(styles viewStyles) string {
	spinner := spinnerFrames[m.spinnerIndex]
	output := m.setupOutput
	if m.stackStarting && m.setupStream != nil {
		output = m.setupStream.Snapshot()
	}
	body := []string{
		styles.Title.Render("Servermore Tester"),
		"",
		styles.SectionTitle.Render(spinner + " " + m.setupStatus),
		styles.Muted.Render(m.statusLine),
	}

	if m.setupErr != nil {
		body = append(body,
			"",
			styles.Error.Render("Error: "+m.setupErr.Error()),
		)
	}
	if strings.TrimSpace(output) != "" {
		body = append(body,
			"",
			styles.Help.Render("Recent command output:"),
			styles.Output.Width(maxInt(60, m.width-14)).Render(compactText(output, 800)),
		)
	}
	body = append(body, "", styles.Help.Render("q quits"))

	content := styles.Panel.Width(maxInt(90, minInt(140, m.width-4))).Render(strings.Join(body, "\n"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
}

func (m *model) renderQuittingView(styles viewStyles) string {
	body := []string{
		styles.Title.Render("Servermore Tester"),
		"",
		styles.SectionTitle.Render("Compose stack is going down..."),
		styles.Muted.Render("Please wait while Docker Compose stops the stack."),
	}

	content := styles.Panel.Width(maxInt(60, minInt(100, m.width-8))).Render(strings.Join(body, "\n"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
}

func (m *model) renderDeployView(styles viewStyles) string {
	function := m.functions[m.deployFunctionIndex]
	sections := []string{
		styles.Title.Render("Deploy Function"),
		"",
		styles.Muted.Render("Binary: " + function.requester.BinaryName()),
		styles.Muted.Render(function.requester.Description()),
		"",
		strings.TrimSpace(m.deployForm.View()),
		"",
		styles.Help.Render("enter submits | esc cancels"),
	}

	content := styles.Panel.Width(maxInt(60, minInt(90, m.width-8))).Render(strings.Join(sections, "\n"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
}

func (m *model) renderDashboardView(styles viewStyles) string {
	parts := []string{
		styles.Title.Render("Servermore Tester"),
		styles.Muted.Render(
			"Compile test binaries, deploy them into the local stack, and keep live traffic running.",
		),
		"",
		m.renderDozzleStatus(styles),
		"",
		m.renderFunctionSelector(styles),
		"",
		styles.Status.Render(m.statusLine),
		"",
		m.renderFunctionPanel(
			styles,
			m.functions[m.selectedFunction],
			maxInt(60, minInt(110, m.width-8)),
		),
		"",
		styles.Help.Render(
			"left/right or h/l switch functions | up/down or k/j choose setting | 0 toggle requests | enter deploy | +/- adjust | d dozzle | q quits",
		),
	}
	return styles.App.Width(maxInt(80, m.width)).Render(strings.Join(parts, "\n"))
}

func (m *model) renderDozzleStatus(styles viewStyles) string {
	button := "[ d Start Dozzle ]"
	if m.dozzleContainerID != "" {
		button = "[ Dozzle running ]"
	}
	if m.dozzleStarting {
		button = "[ Starting Dozzle... ]"
	}

	url := "not started"
	if m.dozzleURL != "" {
		url = m.dozzleURL
	}

	line := styles.FieldLabel.Render("Dozzle: ") + styles.SelectedField.Render(button) +
		styles.FieldLabel.Render(" | URL: ") + styles.FieldValue.Render(url)
	if m.dozzleError != "" {
		line += "\n" + styles.Error.Render(m.dozzleError)
	}
	line += "\n" + styles.FieldLabel.Render("Grafana: ") + styles.FieldValue.Render(m.grafanaURL)
	return line
}

func (m *model) renderFunctionSelector(styles viewStyles) string {
	selectors := make([]string, 0, len(m.functions))
	for index, function := range m.functions {
		label := strings.ToUpper(function.requester.BinaryName())
		if index == m.selectedFunction {
			selectors = append(selectors, styles.SelectorSelected.Render(" "+label+" "))
			continue
		}
		selectors = append(selectors, styles.Selector.Render(" "+label+" "))
	}
	return lipgloss.JoinHorizontal(lipgloss.Bottom, selectors...)
}

func (m *model) renderFunctionPanel(
	styles viewStyles,
	function *functionEntry,
	width int,
) string {
	sections := []string{
		styles.FunctionTitle.Render(strings.ToUpper(function.requester.BinaryName())),
		styles.Muted.Render(function.requester.Description()),
	}

	if function.deploying {
		sections = append(sections, "", styles.Status.Render("Deploying..."))
	} else if function.deployed == nil {
		sections = append(sections, "", styles.SelectedField.Render("[ Deploy function ]"))
		if function.deployError != "" {
			sections = append(sections, styles.Error.Render(compactText(function.deployError, 180)))
		}
	} else {
		snapshot := function.deployed.Snapshot()
		sections = append(
			sections,
			"",
			styles.FieldLabel.Render("Name: ")+styles.FieldValue.Render(snapshot.Name),
			styles.FieldLabel.Render("Function ID: ")+styles.FieldValue.Render(snapshot.FunctionID),
			"",
			m.renderSetting(styles, m.selectedField == 0, "Batch size", fmt.Sprintf("%d", snapshot.Settings.BatchSize)),
			m.renderSetting(styles, m.selectedField == 1, "Requests/s", fmt.Sprintf("%d", snapshot.Settings.RequestsPerSecond)),
			m.renderSetting(styles, m.selectedField == 2, "Delay (s)", fmt.Sprintf("%d", snapshot.Settings.DelayBetweenBatches)),
			"",
			styles.FieldLabel.Render("Sent: ")+styles.FieldValue.Render(fmt.Sprintf("%d", snapshot.Stats.RequestsSent)),
			styles.FieldLabel.Render("Responses: ")+styles.FieldValue.Render(fmt.Sprintf("%d", snapshot.Stats.ResponsesReceived)),
			styles.FieldLabel.Render("Transport errors: ")+styles.FieldValue.Render(fmt.Sprintf("%d", snapshot.Stats.TransportErrors)),
			styles.FieldLabel.Render("Batches: ")+styles.FieldValue.Render(fmt.Sprintf("%d", snapshot.Stats.BatchesCompleted)),
		)

		if snapshot.Stats.LastPath != "" {
			sections = append(sections,
				styles.FieldLabel.Render("Last request: ")+styles.FieldValue.Render(snapshot.Stats.LastMethod+" "+snapshot.Stats.LastPath),
			)
		}
		if snapshot.Stats.LastStatusCode != 0 {
			sections = append(sections,
				styles.FieldLabel.Render("Last status: ")+styles.FieldValue.Render(fmt.Sprintf("%d", snapshot.Stats.LastStatusCode)),
			)
		}
		if !snapshot.Enabled {
			sections = append(sections, "", styles.Error.Render("Requests are disabled."))
		}
		if snapshot.Stats.LastDuration > 0 {
			sections = append(sections,
				styles.FieldLabel.Render("Last latency: ")+styles.FieldValue.Render(snapshot.Stats.LastDuration.String()),
			)
		}
		if snapshot.Stats.LastResponse != "" {
			sections = append(sections,
				styles.FieldLabel.Render("Last response: ")+styles.FieldValue.Render(snapshot.Stats.LastResponse),
			)
		}
		if snapshot.Stats.LastError != "" {
			sections = append(sections, styles.Error.Render("Last error: "+snapshot.Stats.LastError))
		}
	}

	return styles.FunctionPanel.Width(width).Render(strings.Join(sections, "\n"))
}

func (m *model) renderSetting(styles viewStyles, selected bool, label string, value string) string {
	renderedValue := styles.FieldValue.Render("[ " + value + " ]")
	if selected {
		renderedValue = styles.SelectedField.Render("[ " + value + " ]")
	}
	return styles.FieldLabel.Render(label+": ") + renderedValue
}

type viewStyles struct {
	App              lipgloss.Style
	Panel            lipgloss.Style
	Title            lipgloss.Style
	Muted            lipgloss.Style
	Status           lipgloss.Style
	Help             lipgloss.Style
	Error            lipgloss.Style
	Output           lipgloss.Style
	Selector         lipgloss.Style
	SelectorSelected lipgloss.Style
	FunctionPanel    lipgloss.Style
	SectionTitle     lipgloss.Style
	FunctionTitle    lipgloss.Style
	FieldLabel       lipgloss.Style
	FieldValue       lipgloss.Style
	SelectedField    lipgloss.Style
}

func newStyles() viewStyles {
	return viewStyles{
		App: lipgloss.NewStyle().Padding(1, 2),
		Panel: lipgloss.NewStyle().
			Border(appBorder).
			BorderForeground(lipgloss.Color("63")).
			Padding(1, 2),
		Title: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("86")),
		Muted: lipgloss.NewStyle().Foreground(lipgloss.Color("246")),
		Status: lipgloss.NewStyle().
			Foreground(lipgloss.Color("229")).
			Background(lipgloss.Color("24")).
			Padding(0, 1),
		Help:   lipgloss.NewStyle().Foreground(lipgloss.Color("243")),
		Error:  lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Bold(true),
		Output: lipgloss.NewStyle().Foreground(lipgloss.Color("250")),
		Selector: lipgloss.NewStyle().
			Foreground(lipgloss.Color("246")).
			Padding(0, 1),
		SelectorSelected: lipgloss.NewStyle().
			Foreground(lipgloss.Color("16")).
			Background(lipgloss.Color("86")).
			Bold(true).
			Padding(0, 1),
		FunctionPanel: lipgloss.NewStyle().
			Border(appBorder).
			BorderForeground(lipgloss.Color("86")).
			Padding(1, 1),
		SectionTitle: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("111")),
		FunctionTitle: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("111")),
		FieldLabel: lipgloss.NewStyle().Foreground(lipgloss.Color("246")),
		FieldValue: lipgloss.NewStyle().Foreground(lipgloss.Color("229")),
		SelectedField: lipgloss.NewStyle().
			Foreground(lipgloss.Color("16")).
			Background(lipgloss.Color("86")).
			Bold(true),
	}
}

func uiTickCmd() tea.Cmd {
	return func() tea.Msg {
		time.Sleep(120 * time.Millisecond)
		return uiTickMsg(time.Now())
	}
}

func dashboardTickCmd() tea.Cmd {
	return func() tea.Msg {
		time.Sleep(300 * time.Millisecond)
		return dashboardTickMsg(time.Now())
	}
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

func clampInt(value int, minValue int, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}
