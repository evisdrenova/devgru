package ui

import (
	_ "embed"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/evisdrenova/devgru/internal/config"
	"github.com/evisdrenova/devgru/internal/ide"
	"github.com/evisdrenova/devgru/internal/provider"
	"github.com/evisdrenova/devgru/internal/runner"
)

//go:embed devgru_logo.txt
var devgruLogo string

// AppState represents the current state of the application
type AppState int

type StepStatus string

const (
	StateInput AppState = iota
	StatePlanning
	StateResults
	StateError
)
const (
	StatusWorking  StepStatus = "working"
	StatusComplete StepStatus = "complete"
	StatusError    StepStatus = "error"
)

// PlanningStepMsg is emitted as each sub-step of the plan runs.
type PlanningStepMsg struct {
	Step        string     `json:"step"`
	Description string     `json:"description"`
	Status      StepStatus `json:"status"`
}

type PlanningCompleteMsg struct {
	plan *PlanResult
	err  error
}

type PlanResult struct {
	FinalPlan    string
	Steps        []PlanStep
	SelectedPlan string
	Confidence   float64
	Reasoning    string
}

type PlanStepType string

const (
	PlanStepRead   PlanStepType = "read"
	PlanStepUpdate PlanStepType = "update"
	PlanStepCreate PlanStepType = "create"
	PlanStepDelete PlanStepType = "delete"
)

type PlanStep struct {
	Number      int
	Title       string
	Description string
	Type        PlanStepType
	Files       []string
}

type WorkerPlan struct {
	WorkerID string
	Model    string
	Plan     string
	Score    float64
}

// Other messages
type RunCompleteMsg struct {
	result *runner.RunResult
	err    error
}

type IDEContextUpdateMsg struct {
	context *ide.IDEContext
}

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

	// Planning state
	currentPrompt string
	planningSteps []PlanningStepMsg
	finalPlan     *PlanResult

	// Results screen
	resultsModel *ResultsModel

	// Error state
	errorMessage string

	// IDE context from VS Code
	ideContext *ide.IDEContext

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

// InputModel represents the command input screen
type InputModel struct {
	textArea textarea.Model
	history  []string
	cursor   int
	keys     InputKeyMap
	width    int
	height   int
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

func NewInteractiveModel(r *runner.Runner, cfg *config.Config, ideServer *ide.Server) *InteractiveModel {
	ti := textarea.New()
	ti.Placeholder = `Try "write a test for <filepath>"`
	ti.Focus()
	ti.CharLimit = 1000

	inputModel := &InputModel{
		textArea: ti,
		history:  []string{},
		keys:     DefaultInputKeyMap(),
		width:    80,
		height:   24,
	}

	return &InteractiveModel{
		state:      StateInput,
		runner:     r,
		config:     cfg,
		ideServer:  ideServer,
		inputModel: inputModel,
		ideContext: &ide.IDEContext{},
		keys:       DefaultGlobalKeyMap(),
	}
}

func (m *InteractiveModel) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		m.pollIDEContext(), // Start polling IDE context
	)
}

func (m *InteractiveModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		m.inputModel.textArea.SetWidth(msg.Width - 4)

		return m, nil

	case PlanningStepMsg:
		m.planningSteps = append(m.planningSteps, msg)
		return m, nil

	case PlanningCompleteMsg:
		if msg.err != nil {
			m.state = StateError
			m.errorMessage = msg.err.Error()
		} else {
			m.finalPlan = msg.plan
			// Stay in planning state to show the final plan
		}
		return m, nil

	case RunCompleteMsg:
		if msg.err != nil {
			m.state = StateError
			m.errorMessage = msg.err.Error()
		} else {
			m.resultsModel = NewResultsModel(msg.result)
			m.state = StateResults
		}
		return m, nil

	case tea.KeyMsg:
		// Global key handling
		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, m.keys.Back):
			if m.state == StateResults || m.state == StateError {
				m.state = StateInput
				return m, nil
			} else if m.state == StatePlanning {
				m.state = StateInput
				m.planningSteps = nil
				m.finalPlan = nil
				m.currentPrompt = ""
				return m, nil
			}
		}

		// State-specific key handling
		switch m.state {
		case StateInput:
			return m.updateInput(msg)
		case StatePlanning:
			return m.updatePlanning(msg)
		case StateResults:
			if m.resultsModel != nil {
				var updatedModel tea.Model
				updatedModel, cmd = m.resultsModel.Update(msg)
				m.resultsModel = updatedModel.(*ResultsModel)
				return m, cmd
			}
		case StateError:
			m.state = StateInput
			return m, nil
		}

	case IDEContextUpdateMsg:
		if msg.context != nil {
			m.ideContext = msg.context
		}
		return m, m.pollIDEContext()
	}

	// Update input model if we're in input state
	if m.state == StateInput {
		m.inputModel.textArea, cmd = m.inputModel.textArea.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

// updatePlanning handles planning state input
func (m *InteractiveModel) updatePlanning(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
		if m.finalPlan != nil {
			// User confirmed the plan, now execute it
			return m, m.executePlan()
		}
	case key.Matches(msg, key.NewBinding(key.WithKeys("r"))):
		// Regenerate plan
		m.planningSteps = nil
		m.finalPlan = nil
		return m, m.startPlanning(m.currentPrompt)
	}
	return m, nil
}

// updateInput handles input screen updates
func (m *InteractiveModel) updateInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch {
	case key.Matches(msg, m.inputModel.keys.Submit):
		input := m.inputModel.textArea.Value()
		if input != "" {
			m.inputModel.history = append(m.inputModel.history, input)
			m.currentPrompt = input
			m.state = StatePlanning
			m.planningSteps = nil
			m.finalPlan = nil
			m.inputModel.textArea.SetValue("")

			// Start the planning process with simulated steps
			return m, tea.Batch(
				func() tea.Msg {
					return PlanningStepMsg{
						Step:        "Analyzing request",
						Description: "Understanding the context and requirements",
						Status:      StatusWorking,
					}
				},
				func() tea.Msg {
					time.Sleep(500 * time.Millisecond)
					return PlanningStepMsg{
						Step:        "✅ Request analyzed",
						Description: "Context and requirements understood",
						Status:      "complete",
					}
				},
				func() tea.Msg {
					time.Sleep(1 * time.Second)
					return PlanningStepMsg{
						Step:        "Consulting AI workers",
						Description: fmt.Sprintf("Getting plans from %d workers", len(m.config.Workers)),
						Status:      "working",
					}
				},
				func() tea.Msg {
					time.Sleep(2 * time.Second)
					return PlanningStepMsg{
						Step:        "✅ Worker plans received",
						Description: "All workers have submitted their plans",
						Status:      "complete",
					}
				},
				func() tea.Msg {
					time.Sleep(3 * time.Second)
					return PlanningStepMsg{
						Step:        "Evaluating plans",
						Description: "Judges are reviewing and scoring each plan",
						Status:      "working",
					}
				},
				func() tea.Msg {
					time.Sleep(4 * time.Second)
					return PlanningStepMsg{
						Step:        "✅ Plan evaluation complete",
						Description: "Best plan selected based on judge scores",
						Status:      "complete",
					}
				},
				m.runPlanningProcess(input),
			)
		}
		return m, nil

	case key.Matches(msg, m.inputModel.keys.Clear):
		m.inputModel.textArea.SetValue("")
		return m, nil

	case key.Matches(msg, m.inputModel.keys.Up):
		if len(m.inputModel.history) > 0 && m.inputModel.cursor < len(m.inputModel.history) {
			m.inputModel.cursor++
			idx := len(m.inputModel.history) - m.inputModel.cursor
			m.inputModel.textArea.SetValue(m.inputModel.history[idx])
		}
		return m, nil

	case key.Matches(msg, m.inputModel.keys.Down):
		if m.inputModel.cursor > 0 {
			m.inputModel.cursor--
			if m.inputModel.cursor == 0 {
				m.inputModel.textArea.SetValue("")
			} else {
				idx := len(m.inputModel.history) - m.inputModel.cursor
				m.inputModel.textArea.SetValue(m.inputModel.history[idx])
			}
		}
		return m, nil
	}

	m.inputModel.textArea, cmd = m.inputModel.textArea.Update(msg)
	return m, cmd
}

// View implements bubbletea.Model
func (m *InteractiveModel) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	switch m.state {
	case StateInput:
		return m.renderInput()
	case StatePlanning:
		return m.renderPlanning()
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
	// Logo section
	logoStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("208")). // Orange fire color
		Align(lipgloss.Center).
		Width(m.width).
		Padding(1, 0)

	logo := logoStyle.Render(devgruLogo)

	// Status line above input - VS Code status + Workers info on left, Current file on right
	statusLineStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Padding(0, 4).
		Width(m.width)

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

	inputField := m.inputModel.textArea.View()
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

// renderPlanning renders the planning state
func (m *InteractiveModel) renderPlanning() string {
	if m.width == 0 {
		return "Loading..."
	}

	var sections []string

	// Show the original prompt
	promptStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("39")).
		Padding(0, 2)

	promptSection := promptStyle.Render(fmt.Sprintf("> %s", m.currentPrompt))
	sections = append(sections, promptSection)

	// Show file context if available
	if m.ideContext.ActiveFile != "" {
		contextStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			Padding(0, 4)

		contextText := fmt.Sprintf("📁 Selected file: %s", m.ideContext.ActiveFile)
		if m.ideContext.Selection != nil {
			sel := m.ideContext.Selection
			if sel.StartLine == sel.EndLine {
				contextText += fmt.Sprintf(" (L%d)", sel.StartLine)
			} else {
				contextText += fmt.Sprintf(" (L%d-L%d)", sel.StartLine, sel.EndLine)
			}
		}

		contextSection := contextStyle.Render(contextText)
		sections = append(sections, contextSection, "")
	}

	// Show planning steps
	for _, step := range m.planningSteps {
		stepStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("252")).
			Padding(0, 2)

		var icon string
		switch step.Status {
		case "working":
			icon = "🔄"
		case "complete":
			icon = "✅"
		case "error":
			icon = "❌"
		}

		stepText := fmt.Sprintf("%s %s", icon, step.Step)
		if step.Description != "" {
			stepText += fmt.Sprintf("\n   %s", step.Description)
		}

		stepSection := stepStyle.Render(stepText)
		sections = append(sections, stepSection)
	}

	// Show final plan if available
	if m.finalPlan != nil {
		planStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("214")).
			Background(lipgloss.Color("237")).
			Padding(1, 2).
			Margin(1, 0).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("214"))

		planHeader := "🎯 PROPOSED PLAN"
		planContent := fmt.Sprintf("%s\n\n%s", planHeader, m.finalPlan.FinalPlan)

		if len(m.finalPlan.Steps) > 0 {
			planContent += "\n\nSteps:"
			for _, step := range m.finalPlan.Steps {
				planContent += fmt.Sprintf("\n%d. %s", step.Number, step.Title)
				if step.Description != "" {
					planContent += fmt.Sprintf("\n   %s", step.Description)
				}
			}
		}

		planContent += fmt.Sprintf("\n\nConfidence: %.1f%%", m.finalPlan.Confidence*100)

		planSection := planStyle.Render(planContent)
		sections = append(sections, "", planSection)

		// Show action options
		actionsStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			Padding(1, 2)

		actions := "Press Enter to execute plan • r to regenerate • Esc to cancel"
		actionsSection := actionsStyle.Render(actions)
		sections = append(sections, actionsSection)
	}

	content := lipgloss.JoinVertical(lipgloss.Left, sections...)
	return content
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

// pollIDEContext polls the IDE server for context updates
func (m *InteractiveModel) pollIDEContext() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
		if m.ideServer != nil {
			context := m.ideServer.GetContext()
			return IDEContextUpdateMsg{context: context}
		}
		return IDEContextUpdateMsg{context: nil}
	})
}

// Planning process methods
func (m *InteractiveModel) startPlanning(prompt string) tea.Cmd {
	return func() tea.Msg {
		return PlanningStepMsg{
			Step:        "Analyzing request",
			Description: "Understanding the context and requirements",
			Status:      "working",
		}
	}
}

func (m *InteractiveModel) runPlanningProcess(prompt string) tea.Cmd {
	return func() tea.Msg {
		// Simulate the planning process
		time.Sleep(5 * time.Second)

		// Generate mock plan
		finalPlan := &PlanResult{
			FinalPlan: m.generateMockPlan(prompt),
			Steps: []PlanStep{
				{Number: 1, Title: "Read current implementation", Description: "Examine the selected file/function", Type: "read"},
				{Number: 2, Title: "Identify changes needed", Description: "Determine specific modifications required", Type: "update"},
				{Number: 3, Title: "Implement changes", Description: "Make targeted code modifications", Type: "update"},
				{Number: 4, Title: "Test changes", Description: "Verify functionality works as expected", Type: "read"},
			},
			SelectedPlan: "claude-3-5-sonnet",
			Confidence:   0.87,
			Reasoning:    "Selected plan due to comprehensive analysis and clear step-by-step approach",
		}

		return PlanningCompleteMsg{plan: finalPlan}
	}
}

func (m *InteractiveModel) generateMockPlan(prompt string) string {
	return fmt.Sprintf(`## Analysis
The request "%s" requires updating code functionality.

## Implementation Plan
1. **Read current implementation**
   - Examine the selected file/function
   - Understand current behavior and structure

2. **Identify changes needed**
   - Determine specific modifications required
   - Consider impact on existing functionality

3. **Implement changes**
   - Make targeted code modifications
   - Ensure compatibility with existing code

4. **Test changes**
   - Verify functionality works as expected
   - Run relevant tests

## Files to modify
- %s

## Considerations
- Maintain backward compatibility
- Follow existing code patterns
- Consider performance implications`, prompt, m.ideContext.ActiveFile)
}

func (m *InteractiveModel) executePlan() tea.Cmd {
	return func() tea.Msg {
		// Create a mock result showing the plan execution
		result := &runner.RunResult{
			Success:       true,
			TotalDuration: time.Second * 3,
			TotalTokens:   2500,
			EstimatedCost: 0.004500,
			Workers: []runner.WorkerResult{
				{
					WorkerID: "plan-executor",
					Content:  "Plan execution started. Implementation in progress...",
					Stats: &provider.Stats{ // Changed from runner.WorkerStats to provider.Stats
						Model:         "claude-3-5-sonnet",
						Duration:      time.Second * 3,
						EstimatedCost: 0.004500,
					},
				},
			},
		}

		return RunCompleteMsg{result: result, err: nil}
	}
}
