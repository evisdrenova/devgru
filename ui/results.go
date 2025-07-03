package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/evisdrenova/devgru/internal/runner"
)

// AppState represents the current state of the application
type AppState int

const (
	StateWelcome AppState = iota
	StateInput
	StateResults
)

// Model represents the main application model
type Model struct {
	state  AppState
	width  int
	height int

	// Welcome screen
	welcomeModel *WelcomeModel

	// Input screen
	inputModel *InputModel

	// Results screen
	resultsModel *ResultsModel

	// Global key bindings
	keys GlobalKeyMap
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

// WelcomeModel represents the welcome/startup screen
type WelcomeModel struct {
	cursor   int
	options  []string
	selected int
	keys     WelcomeKeyMap
}

type WelcomeKeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Enter  key.Binding
	Number key.Binding
}

func DefaultWelcomeKeyMap() WelcomeKeyMap {
	return WelcomeKeyMap{
		Up: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑/k", "move up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓/j", "move down"),
		),
		Enter: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "select"),
		),
		Number: key.NewBinding(
			key.WithKeys("1", "2", "3", "4"),
			key.WithHelp("1-4", "quick select"),
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
	}
}

// NewModel creates a new main application model
func NewModel() *Model {
	// Create welcome model
	welcomeModel := &WelcomeModel{
		cursor: 0,
		options: []string{
			"Run /init to create a CLAUDE.md file with instructions for Claude",
			"Use Claude to help with file analysis, editing, bash commands and git",
			"Be as specific as you would with another engineer for the best results",
			"✓ Run /terminal-setup to set up terminal integration",
		},
		keys: DefaultWelcomeKeyMap(),
	}

	// Create input model
	ti := textinput.New()
	ti.Placeholder = `Try "write a test for <filepath>"`
	ti.Focus()
	ti.CharLimit = 500
	ti.Width = 80

	inputModel := &InputModel{
		textInput: ti,
		history:   []string{},
		keys:      DefaultInputKeyMap(),
	}

	return &Model{
		state:        StateWelcome,
		welcomeModel: welcomeModel,
		inputModel:   inputModel,
		keys:         DefaultGlobalKeyMap(),
	}
}

// Init implements bubbletea.Model
func (m *Model) Init() tea.Cmd {
	return textinput.Blink
}

// Update implements bubbletea.Model
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

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

	case tea.KeyMsg:
		// Global key handling
		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, m.keys.Back):
			if m.state == StateResults {
				m.state = StateInput
				return m, nil
			} else if m.state == StateInput {
				m.state = StateWelcome
				return m, nil
			}
		}

		// State-specific key handling
		switch m.state {
		case StateWelcome:
			return m.updateWelcome(msg)
		case StateInput:
			return m.updateInput(msg)
		case StateResults:
			if m.resultsModel != nil {
				var updatedModel tea.Model
				updatedModel, cmd = m.resultsModel.Update(msg)
				m.resultsModel = updatedModel.(*ResultsModel)
				return m, cmd
			}
		}
	}

	return m, cmd
}

// updateWelcome handles welcome screen updates
func (m *Model) updateWelcome(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.welcomeModel.keys.Up):
		if m.welcomeModel.cursor > 0 {
			m.welcomeModel.cursor--
		}
	case key.Matches(msg, m.welcomeModel.keys.Down):
		if m.welcomeModel.cursor < len(m.welcomeModel.options)-1 {
			m.welcomeModel.cursor++
		}
	case key.Matches(msg, m.welcomeModel.keys.Enter):
		m.state = StateInput
		return m, nil
	case key.Matches(msg, m.welcomeModel.keys.Number):
		if num := msg.String(); num >= "1" && num <= "4" {
			m.state = StateInput
			return m, nil
		}
	}
	return m, nil
}

// updateInput handles input screen updates
func (m *Model) updateInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch {
	case key.Matches(msg, m.inputModel.keys.Submit):
		// Simulate command execution
		input := m.inputModel.textInput.Value()
		if input != "" {
			// Add to history
			m.inputModel.history = append(m.inputModel.history, input)

			// Create mock result for demonstration
			result := m.createMockResult(input)
			m.resultsModel = NewResultsModel(result)
			m.state = StateResults

			// Clear input
			m.inputModel.textInput.SetValue("")
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

// View implements bubbletea.Model
func (m *Model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	switch m.state {
	case StateWelcome:
		return m.renderWelcome()
	case StateInput:
		return m.renderInput()
	case StateResults:
		if m.resultsModel != nil {
			return m.resultsModel.View()
		}
		return "No results to display"
	}

	return "Unknown state"
}

// renderWelcome renders the welcome screen
func (m *Model) renderWelcome() string {
	// Header
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("39")).
		Background(lipgloss.Color("235")).
		Padding(1, 2).
		Width(m.width).
		Align(lipgloss.Center)

	header := headerStyle.Render("🚀 DEVGRU - AI-Powered Development Assistant")

	// Title
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("214")).
		Padding(2, 0).
		Align(lipgloss.Center)

	title := titleStyle.Render("Tips for getting started:")

	// Options
	var options []string
	for i, option := range m.welcomeModel.options {
		style := lipgloss.NewStyle().
			Padding(0, 4).
			Foreground(lipgloss.Color("252"))

		if i == m.welcomeModel.cursor {
			style = style.
				Bold(true).
				Foreground(lipgloss.Color("15")).
				Background(lipgloss.Color("63"))
		}

		prefix := fmt.Sprintf("%d. ", i+1)
		options = append(options, style.Render(prefix+option))
	}

	optionsContent := lipgloss.JoinVertical(lipgloss.Left, options...)

	// Tip
	tipStyle := lipgloss.NewStyle().
		Italic(true).
		Foreground(lipgloss.Color("241")).
		Padding(2, 4)

	tip := tipStyle.Render("💡 Tip: Start with small features or bug fixes, tell Claude to propose a plan, and verify its suggested edits")

	// Footer
	footerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Background(lipgloss.Color("233")).
		Padding(1, 2).
		Width(m.width).
		Align(lipgloss.Center)

	footer := footerStyle.Render("↑/↓: navigate • enter: continue • 1-4: quick select • ctrl+c: quit")

	// Combine all sections
	content := lipgloss.JoinVertical(
		lipgloss.Center,
		header,
		"",
		title,
		"",
		optionsContent,
		"",
		tip,
	)

	// Center the content vertically
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

// renderInput renders the input screen
func (m *Model) renderInput() string {
	// Header
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("39")).
		Background(lipgloss.Color("235")).
		Padding(1, 2).
		Width(m.width).
		Align(lipgloss.Center)

	header := headerStyle.Render("🤖 Claude Development Assistant")

	// Input section
	inputStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("63")).
		Padding(1, 2).
		Width(m.width-8).
		Margin(2, 4)

	promptStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("214")).
		MarginBottom(1)

	prompt := promptStyle.Render("> 🔥")
	inputField := m.inputModel.textInput.View()
	inputContent := lipgloss.JoinVertical(lipgloss.Left, prompt, inputField)

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

	shortcuts := shortcutStyle.Render("💡 for shortcuts")

	// Footer
	footerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Background(lipgloss.Color("233")).
		Padding(1, 2).
		Width(m.width).
		Align(lipgloss.Center)

	footer := footerStyle.Render("enter: run • ctrl+l: clear • ↑/↓: history • esc: back • ctrl+c: quit")

	// Combine sections
	sections := []string{header, "", inputSection}

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

// createMockResult creates a mock result for demonstration
func (m *Model) createMockResult(input string) *runner.RunResult {
	return &runner.RunResult{
		Success:       true,
		TotalDuration: time.Second * 2,
		TotalTokens:   1500,
		EstimatedCost: 0.002500,
		Workers: []runner.WorkerResult{
			{
				WorkerID: "claude-3-5-sonnet",
				Content:  fmt.Sprintf("Here's a comprehensive response to your request: %s\n\nI'll help you implement this feature with proper error handling, tests, and documentation. Let me break this down into manageable steps...", input),
				Stats: &runner.WorkerStats{
					Model:         "claude-3-5-sonnet-20241022",
					Duration:      time.Millisecond * 1800,
					EstimatedCost: 0.002000,
				},
				TokensUsed: &runner.TokenUsage{
					TotalTokens: 1200,
				},
				JudgeResults: []runner.JudgeResult{
					{
						JudgeID:  "quality-judge",
						Score:    8,
						Duration: time.Millisecond * 200,
						Reason:   "Comprehensive response with good structure and clear explanation of the implementation approach.",
					},
					{
						JudgeID:  "accuracy-judge",
						Score:    9,
						Duration: time.Millisecond * 150,
						Reason:   "Technically accurate and follows best practices. Includes proper error handling.",
					},
				},
				AverageScore: 8.5,
			},
			{
				WorkerID: "gpt-4",
				Content:  fmt.Sprintf("Alternative approach for: %s\n\nI suggest a slightly different implementation that focuses on performance and maintainability...", input),
				Stats: &runner.WorkerStats{
					Model:         "gpt-4-turbo",
					Duration:      time.Millisecond * 1500,
					EstimatedCost: 0.001800,
				},
				TokensUsed: &runner.TokenUsage{
					TotalTokens: 900,
				},
				JudgeResults: []runner.JudgeResult{
					{
						JudgeID:  "quality-judge",
						Score:    7,
						Duration: time.Millisecond * 180,
						Reason:   "Good alternative approach but lacks some implementation details.",
					},
				},
				AverageScore: 7.0,
			},
		},
		Consensus: &runner.ConsensusResult{
			Algorithm:    "weighted-scoring",
			Winner:       "claude-3-5-sonnet",
			Confidence:   0.85,
			Participants: 2,
			Reasoning:    "Claude 3.5 Sonnet provided a more comprehensive response with better structure and more detailed implementation guidance. The response included proper error handling and testing considerations.",
			Content:      fmt.Sprintf("Based on the analysis, here's the recommended approach for: %s\n\nThe winning solution combines the best aspects of both responses, emphasizing comprehensive implementation with proper testing and documentation.", input),
		},
	}
}

// SetResult allows setting a result externally
func (m *Model) SetResult(result *runner.RunResult) {
	m.resultsModel = NewResultsModel(result)
	m.state = StateResults
}

// GetCurrentState returns the current application state
func (m *Model) GetCurrentState() AppState {
	return m.state
}
