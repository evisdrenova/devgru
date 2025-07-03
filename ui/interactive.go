package ui

import (
	_ "embed"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/evisdrenova/devgru/internal/config"
	"github.com/evisdrenova/devgru/internal/ide"
	"github.com/evisdrenova/devgru/internal/provider"
	"github.com/evisdrenova/devgru/internal/runner"
)

//go:embed devgru_logo.txt
var devgruLogo string

// AppState represents the current state of the application (kept for compatibility)
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

type PlanStepType string

const (
	PlanStepRead   PlanStepType = "read"
	PlanStepUpdate PlanStepType = "update"
	PlanStepCreate PlanStepType = "create"
	PlanStepDelete PlanStepType = "delete"
)

type ChatEntryType string

const (
	ChatEntryUser     ChatEntryType = "user"
	ChatEntrySystem   ChatEntryType = "system"
	ChatEntryPlanning ChatEntryType = "planning"
	ChatEntryResult   ChatEntryType = "result"
	ChatEntryError    ChatEntryType = "error"
)

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

// ChatEntry represents a single entry in the chat history
type ChatEntry struct {
	Type      ChatEntryType
	Content   string
	Timestamp time.Time
	Data      interface{} // Store additional data like PlanResult, RunResult, etc.
}

// InteractiveModel represents the main interactive application model
type InteractiveModel struct {
	width  int
	height int

	// Dependencies
	runner    *runner.Runner
	config    *config.Config
	ideServer *ide.Server

	// Chat history and viewport
	chatHistory []ChatEntry
	viewport    viewport.Model
	textArea    textarea.Model

	// Current state for tracking ongoing operations
	currentPrompt string
	isProcessing  bool

	// IDE context from VS Code
	ideContext *ide.IDEContext

	// Key bindings
	keys GlobalKeyMap
}

// GlobalKeyMap defines global key bindings
type GlobalKeyMap struct {
	Submit key.Binding
	Clear  key.Binding
	Quit   key.Binding
	Up     key.Binding
	Down   key.Binding
}

func DefaultGlobalKeyMap() GlobalKeyMap {
	return GlobalKeyMap{
		Submit: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "submit"),
		),
		Clear: key.NewBinding(
			key.WithKeys("ctrl+l"),
			key.WithHelp("ctrl+l", "clear"),
		),
		Quit: key.NewBinding(
			key.WithKeys("ctrl+c"),
			key.WithHelp("ctrl+c", "quit"),
		),
		Up: key.NewBinding(
			key.WithKeys("up"),
			key.WithHelp("↑", "scroll up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down"),
			key.WithHelp("↓", "scroll down"),
		),
	}
}

func (m *InteractiveModel) Init() tea.Cmd {
	return tea.Batch(
		m.pollIDEContext(),
	)
}

func NewInteractiveModel(r *runner.Runner, cfg *config.Config, ideServer *ide.Server) *InteractiveModel {
	// Create viewport for chat history
	vp := viewport.New(0, 0)
	vp.Style = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("63")).
		Padding(1)

	// Create textarea for input
	ta := textarea.New()
	ta.Placeholder = `Try "write a test for <filepath>"`
	ta.Focus()
	ta.CharLimit = 1000
	ta.SetHeight(3)

	// initial welcome message
	chatHistory := []ChatEntry{
		{
			Type:      "system",
			Content:   "🤖 Welcome to DevGru Interactive Chat!",
			Timestamp: time.Now(),
		},
	}

	return &InteractiveModel{
		runner:      r,
		config:      cfg,
		ideServer:   ideServer,
		chatHistory: chatHistory,
		viewport:    vp,
		textArea:    ta,
		ideContext:  &ide.IDEContext{},
		keys:        DefaultGlobalKeyMap(),
	}
}

func (m *InteractiveModel) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	// Status bar
	statusStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Background(lipgloss.Color("235")).
		Padding(0, 2).
		Width(m.width)

	var statusLeft string
	if m.ideServer != nil && m.ideServer.IsConnected() {
		statusLeft = fmt.Sprintf("✅ VS Code Connected • Workers: %d", len(m.config.Workers))
	} else {
		statusLeft = fmt.Sprintf("🔌 VS Code Ready • Workers: %d", len(m.config.Workers))
	}

	var statusRight string
	if m.ideContext.ActiveFile != "" {
		statusRight = fmt.Sprintf("📁 %s", m.ideContext.ActiveFile)
	}

	// Create status line
	statusLine := statusLeft
	if statusRight != "" {
		padding := m.width - lipgloss.Width(statusLeft) - lipgloss.Width(statusRight) - 4
		if padding > 0 {
			statusLine += strings.Repeat(" ", padding) + statusRight
		}
	}

	status := statusStyle.Render(statusLine)

	// Chat viewport
	chatView := m.viewport.View()

	// Input area
	inputStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("63")).
		Padding(0, 1).
		Width(m.width-4).
		Margin(0, 2)

	var inputPrompt string
	if m.isProcessing {
		inputPrompt = "🔄 Processing..."
	} else {
		inputPrompt = "Enter your request:"
	}

	inputContent := lipgloss.JoinVertical(
		lipgloss.Left,
		inputPrompt,
		m.textArea.View(),
	)

	inputSection := inputStyle.Render(inputContent)

	// Help line
	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Padding(0, 2)

	help := helpStyle.Render("enter: submit • ctrl+l: clear • ↑/↓: scroll • ctrl+c: quit")

	// Combine all sections
	return lipgloss.JoinVertical(
		lipgloss.Left,
		status,
		chatView,
		inputSection,
		help,
	)
}

func (m *InteractiveModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		// Reserve space for input area (6 lines) and status bar (2 lines)
		viewportHeight := msg.Height - 8
		m.viewport.Width = msg.Width - 4
		m.viewport.Height = viewportHeight

		m.textArea.SetWidth(msg.Width - 4)

		// Update viewport content with new width
		m.updateViewportContent()
		return m, nil

	case PlanningStepMsg:
		// Append planning step to chat
		m.addChatEntry(ChatEntry{
			Type:      "planning",
			Content:   fmt.Sprintf("%s %s", m.getStatusIcon(msg.Status), msg.Step),
			Timestamp: time.Now(),
			Data:      msg,
		})
		return m, nil

	case PlanningCompleteMsg:
		if msg.err != nil {
			m.addChatEntry(ChatEntry{
				Type:      "error",
				Content:   fmt.Sprintf("❌ Planning failed: %s", msg.err.Error()),
				Timestamp: time.Now(),
			})
			m.isProcessing = false
		} else {
			// Add the final plan to chat
			planContent := m.formatPlanResult(msg.plan)
			m.addChatEntry(ChatEntry{
				Type:      "planning",
				Content:   planContent,
				Timestamp: time.Now(),
				Data:      msg.plan,
			})

			// Auto-execute the plan
			cmds = append(cmds, m.executePlan())
		}
		return m, tea.Batch(cmds...)

	case RunCompleteMsg:
		m.isProcessing = false
		if msg.err != nil {
			m.addChatEntry(ChatEntry{
				Type:      "error",
				Content:   fmt.Sprintf("❌ Execution failed: %s", msg.err.Error()),
				Timestamp: time.Now(),
			})
		} else {
			// Add execution result to chat
			resultContent := m.formatRunResult(msg.result)
			m.addChatEntry(ChatEntry{
				Type:      "result",
				Content:   resultContent,
				Timestamp: time.Now(),
				Data:      msg.result,
			})
		}
		return m, nil

	case IDEContextUpdateMsg:
		if msg.context != nil {
			m.ideContext = msg.context
		}
		return m, m.pollIDEContext()

	case tea.KeyMsg:
		// Handle key bindings
		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit

		case key.Matches(msg, m.keys.Submit):
			if !m.isProcessing {
				input := strings.TrimSpace(m.textArea.Value())
				if input != "" {
					// Add user message to chat
					m.addChatEntry(ChatEntry{
						Type:      "user",
						Content:   input,
						Timestamp: time.Now(),
					})

					// Clear input
					m.textArea.SetValue("")
					m.currentPrompt = input
					m.isProcessing = true

					// Start processing
					return m, m.startPlanning(input)
				}
			}
			return m, nil

		case key.Matches(msg, m.keys.Clear):
			m.chatHistory = []ChatEntry{
				{
					Type:      "system",
					Content:   "Chat cleared.",
					Timestamp: time.Now(),
				},
			}
			m.updateViewportContent()
			return m, nil

		case key.Matches(msg, m.keys.Up):
			m.viewport.LineUp(1)
			return m, nil

		case key.Matches(msg, m.keys.Down):
			m.viewport.LineDown(1)
			return m, nil
		}
	}

	// Update textarea
	m.textArea, cmd = m.textArea.Update(msg)
	cmds = append(cmds, cmd)

	// Update viewport
	m.viewport, cmd = m.viewport.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

// Helper methods

func (m *InteractiveModel) addChatEntry(entry ChatEntry) {
	m.chatHistory = append(m.chatHistory, entry)
	m.updateViewportContent()
	// Auto-scroll to bottom
	m.viewport.GotoBottom()
}

func (m *InteractiveModel) updateViewportContent() {
	var content []string

	for _, entry := range m.chatHistory {
		content = append(content, m.formatChatEntry(entry))
		content = append(content, "") // Add spacing between entries
	}

	m.viewport.SetContent(strings.Join(content, "\n"))
}

func (m *InteractiveModel) formatChatEntry(entry ChatEntry) string {
	timestamp := entry.Timestamp.Format("15:04:05")

	switch entry.Type {
	case "user":
		style := lipgloss.NewStyle().
			Foreground(lipgloss.Color("39")).
			Bold(true)
		return style.Render(fmt.Sprintf("[%s] You: %s", timestamp, entry.Content))

	case "system":
		style := lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			Italic(true)
		return style.Render(fmt.Sprintf("[%s] %s", timestamp, entry.Content))

	case "planning":
		style := lipgloss.NewStyle().
			Foreground(lipgloss.Color("214"))
		return style.Render(fmt.Sprintf("[%s] %s", timestamp, entry.Content))

	case "result":
		style := lipgloss.NewStyle().
			Foreground(lipgloss.Color("46"))
		return style.Render(fmt.Sprintf("[%s] ✅ %s", timestamp, entry.Content))

	case "error":
		style := lipgloss.NewStyle().
			Foreground(lipgloss.Color("196"))
		return style.Render(fmt.Sprintf("[%s] %s", timestamp, entry.Content))

	default:
		return fmt.Sprintf("[%s] %s", timestamp, entry.Content)
	}
}

func (m *InteractiveModel) getStatusIcon(status StepStatus) string {
	switch status {
	case StatusWorking:
		return "🔄"
	case StatusComplete:
		return "✅"
	case StatusError:
		return "❌"
	default:
		return "•"
	}
}

func (m *InteractiveModel) formatPlanResult(plan *PlanResult) string {
	content := "🎯 PROPOSED PLAN\n\n" + plan.FinalPlan

	if len(plan.Steps) > 0 {
		content += "\n\nSteps:"
		for _, step := range plan.Steps {
			content += fmt.Sprintf("\n%d. %s", step.Number, step.Title)
		}
	}

	content += fmt.Sprintf("\n\nConfidence: %.1f%%", plan.Confidence*100)
	content += "\n\n⚡ Executing plan..."

	return content
}

func (m *InteractiveModel) formatRunResult(result *runner.RunResult) string {
	content := fmt.Sprintf("Execution completed in %v", result.TotalDuration)
	content += fmt.Sprintf("\nTokens used: %d", result.TotalTokens)
	content += fmt.Sprintf("\nEstimated cost: $%.6f", result.EstimatedCost)

	if len(result.Workers) > 0 {
		content += "\n\nResults:"
		for _, worker := range result.Workers {
			if worker.Error != nil {
				content += fmt.Sprintf("\n❌ %s: %s", worker.WorkerID, worker.Error.Error())
			} else {
				// Truncate long content for display
				workerContent := worker.Content
				if len(workerContent) > 200 {
					workerContent = workerContent[:200] + "..."
				}
				content += fmt.Sprintf("\n✅ %s: %s", worker.WorkerID, workerContent)
			}
		}
	}

	return content
}

// Planning and execution methods (keep your existing logic)
func (m *InteractiveModel) startPlanning(prompt string) tea.Cmd {
	return tea.Batch(
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
				Status:      StatusComplete,
			}
		},
		func() tea.Msg {
			time.Sleep(1 * time.Second)
			return PlanningStepMsg{
				Step:        "Consulting AI workers",
				Description: fmt.Sprintf("Getting plans from %d workers", len(m.config.Workers)),
				Status:      StatusWorking,
			}
		},
		func() tea.Msg {
			time.Sleep(2 * time.Second)
			return PlanningStepMsg{
				Step:        "✅ Worker plans received",
				Description: "All workers have submitted their plans",
				Status:      StatusComplete,
			}
		},
		m.runPlanningProcess(prompt),
	)
}

func (m *InteractiveModel) runPlanningProcess(prompt string) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(3 * time.Second)

		finalPlan := &PlanResult{
			FinalPlan: m.generateMockPlan(prompt),
			Steps: []PlanStep{
				{Number: 1, Title: "Read current implementation", Type: PlanStepRead},
				{Number: 2, Title: "Identify changes needed", Type: PlanStepUpdate},
				{Number: 3, Title: "Implement changes", Type: PlanStepUpdate},
				{Number: 4, Title: "Test changes", Type: PlanStepRead},
			},
			SelectedPlan: "claude-3-5-sonnet",
			Confidence:   0.87,
			Reasoning:    "Selected plan due to comprehensive analysis",
		}

		return PlanningCompleteMsg{plan: finalPlan}
	}
}

func (m *InteractiveModel) generateMockPlan(prompt string) string {
	return fmt.Sprintf(`Analysis of request: "%s"

Implementation approach:
1. Read current implementation
2. Identify required changes  
3. Implement modifications
4. Test functionality

Target file: %s`, prompt, m.ideContext.ActiveFile)
}

func (m *InteractiveModel) executePlan() tea.Cmd {
	return func() tea.Msg {
		time.Sleep(2 * time.Second)

		result := &runner.RunResult{
			Success:       true,
			TotalDuration: time.Second * 2,
			TotalTokens:   2500,
			EstimatedCost: 0.004500,
			Workers: []runner.WorkerResult{
				{
					WorkerID: "plan-executor",
					Content:  "Plan executed successfully. Code has been updated according to the specifications.",
					Stats: &provider.Stats{
						Model:         "claude-3-5-sonnet",
						Duration:      time.Second * 2,
						EstimatedCost: 0.004500,
					},
				},
			},
		}

		return RunCompleteMsg{result: result, err: nil}
	}
}

func (m *InteractiveModel) pollIDEContext() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
		if m.ideServer != nil {
			context := m.ideServer.GetContext()
			return IDEContextUpdateMsg{context: context}
		}
		return IDEContextUpdateMsg{context: nil}
	})
}
