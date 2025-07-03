package ui

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/evisdrenova/devgru/internal/config"
	"github.com/evisdrenova/devgru/internal/ide"
	"github.com/evisdrenova/devgru/internal/runner"
)

//go:embed devgru_logo.txt
var devgruLogo string

// AppState represents the current state of the application
type AppState int

const (
	StateInput AppState = iota
	StateProcessing
	StateResults
	StateError
)

// InteractiveModel represents the main interactive application model
type InteractiveModel struct {
	state  AppState
	width  int
	height int

	// Dependencies
	runner    *runner.Runner
	config    *config.Config
	ideServer *ide.Server

	// Input screen
	inputModel *InputModel

	// Processing screen
	spinner     spinner.Model
	currentTask string

	// Results screen - using your existing ResultsModel
	resultsModel *ResultsModel

	// Error state
	errorMessage string

	// IDE context from VS Code
	ideContext *ide.IDEContext

	// Global key bindings
	keys GlobalKeyMap
}

// Messages for async operations
type ProcessingMsg struct {
	task string
}

type RunCompleteMsg struct {
	result *runner.RunResult
	err    error
}

// IDE context update message
type IDEContextUpdateMsg struct {
	context *ide.IDEContext
}

// GlobalKeyMap defines global key bindings
type GlobalKeyMap struct {
	Back key.Binding
	Quit key.Binding
}

// DefaultGlobalKeyMap returns default global key bindings
func DefaultGlobalKeyMap() GlobalKeyMap {
	return GlobalKeyMap{
		Back: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "back"),
		),
		Quit: key.NewBinding(
			key.WithKeys("ctrl+c"),
			key.WithHelp("ctrl+c", "quit"),
		),
	}
}

// InputModel represents the command input screen
type InputModel struct {
	textInput textinput.Model
	history   []string
	cursor    int
	keys      InputKeyMap
}

type InputKeyMap struct {
	Submit key.Binding
	Clear  key.Binding
	Up     key.Binding
	Down   key.Binding
	Help   key.Binding
}

func DefaultInputKeyMap() InputKeyMap {
	return InputKeyMap{
		Submit: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "run command"),
		),
		Clear: key.NewBinding(
			key.WithKeys("ctrl+l"),
			key.WithHelp("ctrl+l", "clear"),
		),
		Up: key.NewBinding(
			key.WithKeys("up"),
			key.WithHelp("↑", "history up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down"),
			key.WithHelp("↓", "history down"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "shortcuts"),
		),
	}
}

// NewInteractiveModel creates a new interactive application model
func NewInteractiveModel(r *runner.Runner, cfg *config.Config, ideServer *ide.Server) *InteractiveModel {
	// Create input model
	ti := textinput.New()
	ti.Placeholder = `Try "write a test for <filepath>"`
	ti.Focus()
	ti.CharLimit = 1000
	ti.Width = 80

	inputModel := &InputModel{
		textInput: ti,
		history:   []string{},
		keys:      DefaultInputKeyMap(),
	}

	// Create spinner
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	return &InteractiveModel{
		state:      StateInput,
		runner:     r,
		config:     cfg,
		ideServer:  ideServer,
		inputModel: inputModel,
		spinner:    s,
		ideContext: &ide.IDEContext{},
		keys:       DefaultGlobalKeyMap(),
	}
}

// Init implements bubbletea.Model
func (m *InteractiveModel) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		m.spinner.Tick,
		m.pollIDEContext(), // Start polling IDE context
	)
}

// Update implements bubbletea.Model
func (m *InteractiveModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		// Update child models
		if m.resultsModel != nil {
			m.resultsModel.width = msg.Width
			m.resultsModel.height = msg.Height
		}

		return m, nil

	case ProcessingMsg:
		m.currentTask = msg.task
		return m, m.spinner.Tick

	case RunCompleteMsg:
		if msg.err != nil {
			m.state = StateError
			m.errorMessage = msg.err.Error()
		} else {
			m.resultsModel = NewResultsModel(msg.result)
			m.state = StateResults
		}
		return m, nil

	case IDEContextUpdateMsg:
		if msg.context != nil {
			m.ideContext = msg.context
		}
		return m, m.pollIDEContext() // Continue polling

	case tea.KeyMsg:
		// Global key handling
		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, m.keys.Back):
			if m.state == StateResults || m.state == StateError {
				m.state = StateInput
				return m, nil
			}
		}

		// State-specific key handling
		switch m.state {
		case StateInput:
			return m.updateInput(msg)
		case StateResults:
			if m.resultsModel != nil {
				var updatedModel tea.Model
				updatedModel, cmd = m.resultsModel.Update(msg)
				m.resultsModel = updatedModel.(*ResultsModel)
				return m, cmd
			}
		case StateError:
			// Any key in error state goes back to input
			m.state = StateInput
			return m, nil
		}

	case spinner.TickMsg:
		if m.state == StateProcessing {
			m.spinner, cmd = m.spinner.Update(msg)
			cmds = append(cmds, cmd)
		}
	}

	// Update input model if we're in input state
	if m.state == StateInput {
		m.inputModel.textInput, cmd = m.inputModel.textInput.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

// updateInput handles input screen updates
func (m *InteractiveModel) updateInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch {
	case key.Matches(msg, m.inputModel.keys.Submit):
		// Get the input command
		input := m.inputModel.textInput.Value()
		if input != "" {
			// Add to history
			m.inputModel.history = append(m.inputModel.history, input)

			// Start processing
			m.state = StateProcessing
			m.currentTask = "Processing your request..."

			// Clear input
			m.inputModel.textInput.SetValue("")

			// Start async run
			return m, tea.Batch(
				func() tea.Msg { return ProcessingMsg{task: "Processing your request..."} },
				m.runPromptAsync(input),
				m.spinner.Tick,
			)
		}
		return m, nil

	case key.Matches(msg, m.inputModel.keys.Clear):
		m.inputModel.textInput.SetValue("")
		return m, nil

	case key.Matches(msg, m.inputModel.keys.Up):
		if len(m.inputModel.history) > 0 && m.inputModel.cursor < len(m.inputModel.history) {
			m.inputModel.cursor++
			idx := len(m.inputModel.history) - m.inputModel.cursor
			m.inputModel.textInput.SetValue(m.inputModel.history[idx])
		}
		return m, nil

	case key.Matches(msg, m.inputModel.keys.Down):
		if m.inputModel.cursor > 0 {
			m.inputModel.cursor--
			if m.inputModel.cursor == 0 {
				m.inputModel.textInput.SetValue("")
			} else {
				idx := len(m.inputModel.history) - m.inputModel.cursor
				m.inputModel.textInput.SetValue(m.inputModel.history[idx])
			}
		}
		return m, nil
	}

	// Update text input
	m.inputModel.textInput, cmd = m.inputModel.textInput.Update(msg)
	return m, cmd
}

// runPromptAsync executes the prompt asynchronously
func (m *InteractiveModel) runPromptAsync(prompt string) tea.Cmd {
	return func() tea.Msg {
		// Create context with timeout
		ctx, cancel := context.WithTimeout(context.Background(), m.config.Consensus.Timeout+10*time.Second)
		defer cancel()

		// Execute the run
		result, err := m.runner.Run(ctx, prompt)

		return RunCompleteMsg{
			result: result,
			err:    err,
		}
	}
}

// View implements bubbletea.Model
func (m *InteractiveModel) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	switch m.state {
	case StateInput:
		return m.renderInput()
	case StateProcessing:
		return m.renderProcessing()
	case StateResults:
		if m.resultsModel != nil {
			return m.resultsModel.View()
		}
		return "No results to display"
	case StateError:
		return m.renderError()
	}

	return "Unknown state"
}

// renderInput renders the input screen
func (m *InteractiveModel) renderInput() string {
	// Status line above input - VS Code status + Workers info on left, Current file on right
	statusLineStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Padding(0, 4).
		Width(m.width)

	logoStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("208")). // Orange fire color
		Align(lipgloss.Center).
		Width(m.width).
		Padding(1, 0)

	logo := logoStyle.Render(devgruLogo)

	// Left side: VS Code status + Workers info
	var leftStatus string
	if m.ideServer != nil && m.ideServer.IsConnected() {
		leftStatus = fmt.Sprintf("✅ VS Code Connected • Workers: %d • Algorithm: %s • Timeout: %v",
			len(m.config.Workers),
			m.config.Consensus.Algorithm,
			m.config.Consensus.Timeout)
	} else {
		leftStatus = fmt.Sprintf("🔌 VS Code Ready • Workers: %d • Algorithm: %s • Timeout: %v",
			len(m.config.Workers),
			m.config.Consensus.Algorithm,
			m.config.Consensus.Timeout)
	}

	// Right side: Current file info
	var rightStatus string
	if m.ideServer != nil && m.ideServer.IsConnected() && m.ideContext.ActiveFile != "" {
		rightStatus = fmt.Sprintf("📁 %s", m.ideContext.ActiveFile)

		// Add selection info if available
		if m.ideContext.Selection != nil {
			sel := m.ideContext.Selection
			if sel.StartLine == sel.EndLine {
				rightStatus += fmt.Sprintf(" (L%d)", sel.StartLine)
			} else {
				rightStatus += fmt.Sprintf(" (L%d-L%d)", sel.StartLine, sel.EndLine)
			}
		}
	}

	// Create the status line with left and right alignment
	var statusLine string
	if rightStatus != "" {
		// Calculate padding needed for right alignment
		leftWidth := lipgloss.Width(leftStatus)
		rightWidth := lipgloss.Width(rightStatus)
		availableWidth := m.width - 8 // Account for padding
		paddingNeeded := availableWidth - leftWidth - rightWidth

		if paddingNeeded > 0 {
			padding := strings.Repeat(" ", paddingNeeded)
			statusLine = leftStatus + padding + rightStatus
		} else {
			// If not enough space, truncate left status
			maxLeftWidth := availableWidth - rightWidth - 3 // Leave space for "..."
			if maxLeftWidth > 0 && leftWidth > maxLeftWidth {
				leftStatus = leftStatus[:maxLeftWidth] + "..."
			}
			statusLine = leftStatus + " " + rightStatus
		}
	} else {
		statusLine = leftStatus
	}

	statusLineRendered := statusLineStyle.Render(statusLine)

	// Input section
	inputStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("63")).
		Padding(1, 2).
		Width(m.width-8).
		Margin(0, 4) // Remove top margin since status line is above

	inputField := m.inputModel.textInput.View()
	inputContent := lipgloss.JoinVertical(lipgloss.Left, inputField)

	inputSection := inputStyle.Render(inputContent)

	// History section (if available)
	var historySection string
	if len(m.inputModel.history) > 0 {
		historyStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			Padding(1, 4)

		recentCommands := "Recent commands:"
		historyItems := []string{recentCommands}

		// Show last 3 commands
		start := len(m.inputModel.history) - 3
		if start < 0 {
			start = 0
		}

		for i := start; i < len(m.inputModel.history); i++ {
			historyItems = append(historyItems, fmt.Sprintf("  • %s", m.inputModel.history[i]))
		}

		historySection = historyStyle.Render(strings.Join(historyItems, "\n"))
	}

	// Shortcuts section
	shortcutStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Padding(1, 4)

	shortcuts := shortcutStyle.Render("? for shortcuts")

	// Footer
	footerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Background(lipgloss.Color("233")).
		Padding(0, 2).
		Width(m.width)

	footer := footerStyle.Render("enter: run • ctrl+l: clear • ↑/↓: history • ctrl+c: quit")

	// Combine sections - removed config info and file section, added status line above input
	sections := []string{logo, "", statusLineRendered, inputSection}

	if historySection != "" {
		sections = append(sections, historySection)
	}

	sections = append(sections, shortcuts)

	content := lipgloss.JoinVertical(lipgloss.Left, sections...)

	// Center vertically
	availableHeight := m.height - lipgloss.Height(footer) - 2
	contentHeight := lipgloss.Height(content)
	paddingTop := (availableHeight - contentHeight) / 2
	if paddingTop < 0 {
		paddingTop = 0
	}

	paddedContent := lipgloss.NewStyle().
		PaddingTop(paddingTop).
		Width(m.width).
		Render(content)

	return lipgloss.JoinVertical(lipgloss.Left, paddedContent, footer)
}

// renderProcessing renders the processing screen
func (m *InteractiveModel) renderProcessing() string {
	// Processing header
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("39")).
		Background(lipgloss.Color("235")).
		Padding(1, 2).
		Width(m.width).
		Align(lipgloss.Center)

	header := headerStyle.Render("🤖 DEVGRU - Processing Request")

	// Spinner and message
	spinnerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("205")).
		Padding(2, 0).
		Align(lipgloss.Center)

	messageStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252")).
		Padding(1, 0).
		Align(lipgloss.Center)

	spinnerContent := spinnerStyle.Render(m.spinner.View() + " Processing...")
	message := messageStyle.Render(m.currentTask)

	// Config info during processing
	configStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Padding(2, 0).
		Align(lipgloss.Center)

	configInfo := configStyle.Render(fmt.Sprintf("Running %d workers with %s consensus",
		len(m.config.Workers),
		m.config.Consensus.Algorithm))

	// Combine content
	content := lipgloss.JoinVertical(
		lipgloss.Center,
		header,
		"",
		"",
		spinnerContent,
		message,
		"",
		configInfo,
	)

	// Center vertically
	contentHeight := lipgloss.Height(content)
	paddingTop := (m.height - contentHeight) / 2
	if paddingTop < 0 {
		paddingTop = 0
	}

	return lipgloss.NewStyle().
		PaddingTop(paddingTop).
		Width(m.width).
		Render(content)
}

// pollIDEContext polls the IDE server for context updates
func (m *InteractiveModel) pollIDEContext() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
		if m.ideServer != nil {
			context := m.ideServer.GetContext()
			// Remove debug logging for clean UI
			// if context.ActiveFile != m.ideContext.ActiveFile {
			//     fmt.Printf("DEBUG: IDE context updated - ActiveFile: %s\n", context.ActiveFile)
			// }
			return IDEContextUpdateMsg{context: context}
		}
		return IDEContextUpdateMsg{context: nil}
	})
}

// renderError renders the error screen
func (m *InteractiveModel) renderError() string {
	// Error header
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("15")).
		Background(lipgloss.Color("196")).
		Padding(1, 2).
		Width(m.width).
		Align(lipgloss.Center)

	header := headerStyle.Render("❌ ERROR")

	// Error message
	errorStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("196")).
		Padding(2, 4).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("196")).
		Width(m.width-8).
		Margin(2, 4)

	errorContent := errorStyle.Render(fmt.Sprintf("Failed to process request:\n\n%s", m.errorMessage))

	// Instructions
	instructionsStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Padding(1, 0).
		Align(lipgloss.Center)

	instructions := instructionsStyle.Render("Press any key to return to input, or Ctrl+C to quit")

	// Combine content
	content := lipgloss.JoinVertical(
		lipgloss.Center,
		header,
		"",
		errorContent,
		"",
		instructions,
	)

	// Center vertically
	contentHeight := lipgloss.Height(content)
	paddingTop := (m.height - contentHeight) / 2
	if paddingTop < 0 {
		paddingTop = 0
	}

	return lipgloss.NewStyle().
		PaddingTop(paddingTop).
		Width(m.width).
		Render(content)
}
