package main

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

type functionCard struct {
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
	setupErr     error

	cards         []*functionCard
	selectedCard  int
	selectedField int
	statusLine    string
	stackStarted  bool

	deployCardIndex int
	deployName      string
	deployForm      *huh.Form
}

func newModel(rootDir string) *model {
	ctx, cancel := context.WithCancel(context.Background())

	requesters := catalog()
	cards := make([]*functionCard, 0, len(requesters))
	for _, requester := range requesters {
		cards = append(cards, &functionCard{requester: requester})
	}

	return &model{
		rootDir:         rootDir,
		phase:           phaseSetup,
		ctx:             ctx,
		cancel:          cancel,
		width:           120,
		height:          40,
		setupStatus:     "Compiling testing functions...",
		cards:           cards,
		selectedCard:    0,
		selectedField:   0,
		statusLine:      "Preparing the local test stack...",
		deployCardIndex: -1,
	}
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(uiTickCmd(), compileFunctionsCmd(m.ctx, m.rootDir, m.requesters()))
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.deployForm != nil {
		if key, ok := msg.(tea.KeyPressMsg); ok {
			switch key.String() {
			case "ctrl+c", "q":
				m.cancel()
				return m, tea.Quit
			case "esc":
				m.deployForm = nil
				m.deployCardIndex = -1
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
			cardIndex := m.deployCardIndex
			m.deployForm = nil
			m.deployCardIndex = -1
			m.cards[cardIndex].deploying = true
			m.cards[cardIndex].deployError = ""
			m.statusLine = fmt.Sprintf("Deploying %s...", m.cards[cardIndex].requester.BinaryName())
			return m, tea.Batch(cmd, deployFunctionCmd(
				m.ctx,
				cardIndex,
				m.cards[cardIndex].binaryPath,
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
	case uiTickMsg:
		m.spinnerIndex = (m.spinnerIndex + 1) % len(spinnerFrames)
		return m, uiTickCmd()
	case compileDoneMsg:
		m.setupOutput = msg.output
		if msg.err != nil {
			m.setupErr = msg.err
			m.statusLine = "Compilation failed."
			return m, nil
		}
		for _, card := range m.cards {
			card.binaryPath = msg.binaries[card.requester.BinaryName()]
		}
		m.setupStatus = "Starting the Servermore stack..."
		m.statusLine = "Building containers and waiting for the stack to answer..."
		return m, startStackCmd(m.ctx, m.rootDir)
	case stackReadyMsg:
		m.setupOutput = msg.output
		m.stackStarted = msg.started
		if msg.err != nil {
			m.setupErr = msg.err
			m.statusLine = "Stack startup failed."
			return m, nil
		}
		m.phase = phaseDashboard
		m.statusLine = "Stack is ready. Press Enter to deploy the selected function."
		return m, nil
	case deployDoneMsg:
		card := m.cards[msg.cardIndex]
		card.deploying = false
		if msg.err != nil {
			card.deployError = msg.err.Error()
			m.statusLine = fmt.Sprintf("Deploy %s failed.", card.requester.BinaryName())
			return m, nil
		}

		workerCtx, workerCancel := context.WithCancel(m.ctx)
		deployed := newDeploymentState(msg.name, msg.functionID, workerCancel)
		card.deployed = deployed
		card.deployError = ""
		startWorker(workerCtx, &m.wg, card.requester, deployed)
		m.statusLine = fmt.Sprintf(
			"%s deployed as %q with function id %s.",
			card.requester.BinaryName(),
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
		m.cancel()
		return m, tea.Quit
	}

	if m.phase == phaseSetup {
		return m, nil
	}

	switch msg.String() {
	case "up", "k":
		m.selectedCard = clampInt(m.selectedCard-1, 0, len(m.cards)-1)
		m.normalizeSelectedField()
	case "down", "j":
		m.selectedCard = clampInt(m.selectedCard+1, 0, len(m.cards)-1)
		m.normalizeSelectedField()
	case "left", "h", "shift+tab":
		m.selectedField = clampInt(m.selectedField-1, 0, m.selectedCardFieldCount()-1)
	case "right", "l", "tab":
		m.selectedField = clampInt(m.selectedField+1, 0, m.selectedCardFieldCount()-1)
	case "+", "=":
		if deployed := m.selectedDeployment(); deployed != nil {
			deployed.AdjustSetting(m.selectedField, 1)
		}
	case "-", "_":
		if deployed := m.selectedDeployment(); deployed != nil {
			deployed.AdjustSetting(m.selectedField, -1)
		}
	case "enter":
		selectedCard := m.cards[m.selectedCard]
		if selectedCard.deployed == nil && !selectedCard.deploying {
			m.openDeployForm(m.selectedCard)
			return m, m.deployForm.Init()
		}
	}

	return m, nil
}

func (m *model) View() tea.View {
	styles := newStyles()

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
	for _, card := range m.cards {
		if card.deployed != nil {
			card.deployed.Stop()
		}
	}
	m.wg.Wait()

	if !m.stackStarted {
		return
	}

	cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = stopStack(cleanupCtx, m.rootDir)
}

func (m *model) requesters() []Requester {
	requesters := make([]Requester, 0, len(m.cards))
	for _, card := range m.cards {
		requesters = append(requesters, card.requester)
	}
	return requesters
}

func (m *model) selectedDeployment() *deploymentState {
	return m.cards[m.selectedCard].deployed
}

func (m *model) selectedCardFieldCount() int {
	if m.selectedDeployment() == nil {
		return 1
	}
	return 3
}

func (m *model) normalizeSelectedField() {
	m.selectedField = clampInt(m.selectedField, 0, m.selectedCardFieldCount()-1)
}

func (m *model) openDeployForm(cardIndex int) {
	card := m.cards[cardIndex]
	m.deployCardIndex = cardIndex
	m.deployName = card.requester.SuggestedName()
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
	m.statusLine = fmt.Sprintf("Deploying %s. Press Esc to cancel.", card.requester.BinaryName())
}

func (m *model) renderSetupView(styles viewStyles) string {
	spinner := spinnerFrames[m.spinnerIndex]
	body := []string{
		styles.Title.Render("Servermore Tester"),
		"",
		styles.CardTitle.Render(spinner + " " + m.setupStatus),
		styles.Muted.Render(m.statusLine),
	}

	if m.setupErr != nil {
		body = append(body,
			"",
			styles.Error.Render("Error: "+m.setupErr.Error()),
		)
	}
	if strings.TrimSpace(m.setupOutput) != "" {
		body = append(body,
			"",
			styles.Help.Render("Recent command output:"),
			styles.Output.Width(maxInt(40, m.width-12)).Render(compactText(m.setupOutput, 800)),
		)
	}
	body = append(body, "", styles.Help.Render("q quits"))

	content := styles.Panel.Width(maxInt(60, minInt(100, m.width-8))).
		Render(strings.Join(body, "\n"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
}

func (m *model) renderDeployView(styles viewStyles) string {
	card := m.cards[m.deployCardIndex]
	sections := []string{
		styles.Title.Render("Deploy Function"),
		"",
		styles.Muted.Render("Binary: " + card.requester.BinaryName()),
		styles.Muted.Render(card.requester.Description()),
		"",
		strings.TrimSpace(m.deployForm.View()),
		"",
		styles.Help.Render("enter submits | esc cancels"),
	}

	content := styles.Panel.Width(maxInt(60, minInt(90, m.width-8))).
		Render(strings.Join(sections, "\n"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
}

func (m *model) renderDashboardView(styles viewStyles) string {
	parts := []string{
		styles.Title.Render("Servermore Tester"),
		styles.Muted.Render(
			"Compile test binaries, deploy them into the local stack, and keep live traffic running.",
		),
		"",
		styles.Status.Render(m.statusLine),
		"",
		m.renderCards(styles),
		"",
		styles.Help.Render(
			"up/down choose card | left/right choose setting | enter deploy | +/- adjust | 0 pauses traffic | q quits",
		),
	}
	return styles.App.Width(maxInt(80, m.width)).Render(strings.Join(parts, "\n"))
}

func (m *model) renderCards(styles viewStyles) string {
	columns := 1
	if m.width >= 150 {
		columns = 2
	}
	cardGap := 2
	cardWidth := maxInt(42, (m.width-6-cardGap*(columns-1))/columns)

	rendered := make([]string, 0, len(m.cards))
	for index, card := range m.cards {
		rendered = append(rendered, m.renderCard(styles, card, index, cardWidth))
	}

	if columns == 1 {
		return strings.Join(rendered, "\n\n")
	}

	rows := make([]string, 0, (len(rendered)+1)/2)
	for i := 0; i < len(rendered); i += 2 {
		left := rendered[i]
		right := ""
		if i+1 < len(rendered) {
			right = rendered[i+1]
		}
		rows = append(
			rows,
			lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Repeat(" ", cardGap)+right),
		)
	}
	return strings.Join(rows, "\n\n")
}

func (m *model) renderCard(styles viewStyles, card *functionCard, index int, width int) string {
	isSelected := index == m.selectedCard
	cardStyle := styles.Card.Width(width)
	if isSelected {
		cardStyle = styles.CardFocused.Width(width)
	}

	sections := []string{
		styles.CardTitle.Render(strings.ToUpper(card.requester.BinaryName())),
		styles.Muted.Render(card.requester.Description()),
	}

	if card.deploying {
		sections = append(sections, "", styles.Status.Render("Deploying..."))
	} else if card.deployed == nil {
		action := "[ Deploy function ]"
		if isSelected {
			action = styles.SelectedField.Render(action)
		} else {
			action = styles.FieldValue.Render(action)
		}
		sections = append(sections, "", action)
		if card.deployError != "" {
			sections = append(sections, styles.Error.Render(compactText(card.deployError, 180)))
		}
	} else {
		snapshot := card.deployed.Snapshot()
		sections = append(
			sections,
			"",
			styles.FieldLabel.Render("Name: ")+styles.FieldValue.Render(snapshot.Name),
			styles.FieldLabel.Render("Function ID: ")+styles.FieldValue.Render(snapshot.FunctionID),
			"",
			m.renderSetting(
				styles,
				isSelected && m.selectedField == 0,
				"Batch size",
				fmt.Sprintf("%d", snapshot.Settings.BatchSize),
			),
			m.renderSetting(
				styles,
				isSelected && m.selectedField == 1,
				"Requests/s",
				fmt.Sprintf("%d", snapshot.Settings.RequestsPerSecond),
			),
			m.renderSetting(
				styles,
				isSelected && m.selectedField == 2,
				"Delay (s)",
				fmt.Sprintf("%d", snapshot.Settings.DelayBetweenBatches),
			),
			"",
			styles.FieldLabel.Render(
				"Sent: ",
			)+styles.FieldValue.Render(
				fmt.Sprintf("%d", snapshot.Stats.RequestsSent),
			),
			styles.FieldLabel.Render(
				"Responses: ",
			)+styles.FieldValue.Render(
				fmt.Sprintf("%d", snapshot.Stats.ResponsesReceived),
			),
			styles.FieldLabel.Render(
				"Transport errors: ",
			)+styles.FieldValue.Render(
				fmt.Sprintf("%d", snapshot.Stats.TransportErrors),
			),
			styles.FieldLabel.Render(
				"Batches: ",
			)+styles.FieldValue.Render(
				fmt.Sprintf("%d", snapshot.Stats.BatchesCompleted),
			),
		)

		if snapshot.Stats.LastPath != "" {
			sections = append(
				sections,
				styles.FieldLabel.Render(
					"Last request: ",
				)+styles.FieldValue.Render(
					snapshot.Stats.LastMethod+" "+snapshot.Stats.LastPath,
				),
			)
		}
		if snapshot.Stats.LastStatusCode != 0 {
			sections = append(
				sections,
				styles.FieldLabel.Render(
					"Last status: ",
				)+styles.FieldValue.Render(
					fmt.Sprintf("%d", snapshot.Stats.LastStatusCode),
				),
			)
		}
		if snapshot.Stats.LastDuration > 0 {
			sections = append(
				sections,
				styles.FieldLabel.Render(
					"Last latency: ",
				)+styles.FieldValue.Render(
					snapshot.Stats.LastDuration.String(),
				),
			)
		}
		if snapshot.Stats.LastResponse != "" {
			sections = append(
				sections,
				styles.FieldLabel.Render(
					"Last response: ",
				)+styles.FieldValue.Render(
					snapshot.Stats.LastResponse,
				),
			)
		}
		if snapshot.Stats.LastError != "" {
			sections = append(
				sections,
				styles.Error.Render("Last error: "+snapshot.Stats.LastError),
			)
		}
	}

	return cardStyle.Render(strings.Join(sections, "\n"))
}

func (m *model) renderSetting(styles viewStyles, selected bool, label string, value string) string {
	renderedValue := styles.FieldValue.Render("[ " + value + " ]")
	if selected {
		renderedValue = styles.SelectedField.Render("[ " + value + " ]")
	}
	return styles.FieldLabel.Render(label+": ") + renderedValue
}

type viewStyles struct {
	App           lipgloss.Style
	Panel         lipgloss.Style
	Title         lipgloss.Style
	Muted         lipgloss.Style
	Status        lipgloss.Style
	Help          lipgloss.Style
	Error         lipgloss.Style
	Output        lipgloss.Style
	Card          lipgloss.Style
	CardFocused   lipgloss.Style
	CardTitle     lipgloss.Style
	FieldLabel    lipgloss.Style
	FieldValue    lipgloss.Style
	SelectedField lipgloss.Style
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
		Help: lipgloss.NewStyle().Foreground(lipgloss.Color("243")),
		Error: lipgloss.NewStyle().
			Foreground(lipgloss.Color("203")).
			Bold(true),
		Output: lipgloss.NewStyle().Foreground(lipgloss.Color("250")),
		Card: lipgloss.NewStyle().
			Border(appBorder).
			BorderForeground(lipgloss.Color("240")).
			Padding(1, 1),
		CardFocused: lipgloss.NewStyle().
			Border(appBorder).
			BorderForeground(lipgloss.Color("86")).
			Padding(1, 1),
		CardTitle: lipgloss.NewStyle().
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
