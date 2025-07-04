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
	ChatEntryUser       ChatEntryType = "user"
	ChatEntrySystem     ChatEntryType = "system"
	ChatEntryPlanning   ChatEntryType = "planning"
	ChatEntryResult     ChatEntryType = "result"
	ChatEntryError      ChatEntryType = "error"
	ChatEntryProcessing ChatEntryType = "processing"
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
type Block struct {
	ID        string
	Type      ChatEntryType
	Content   string
	Status    StepStatus
	Timestamp time.Time
	Data      interface{}
	ParentID  string
	Children  []Block
	IsLast    bool
}

type ChatEntry struct {
	Type      ChatEntryType
	Content   string
	Timestamp time.Time
	Data      interface{}
}

type InteractiveModel struct {
	width  int
	height int

	runner    *runner.Runner
	config    *config.Config
	ideServer *ide.Server

	blocks        []Block
	viewport      viewport.Model
	textArea      textarea.Model
	currentUserID string

	currentPrompt string
	isProcessing  bool

	ideContext *ide.IDEContext

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

	vp := viewport.New(0, 0)

	ta := textarea.New()
	ta.Placeholder = `Try "write a test for <filepath>"`
	ta.Focus()
	ta.ShowLineNumbers = false
	ta.Prompt = "> "
	ta.CharLimit = 1000
	ta.SetHeight(1)

	ta.BlurredStyle.Base = lipgloss.NewStyle()
	ta.FocusedStyle.Base = lipgloss.NewStyle()
	ta.BlurredStyle.CursorLine = lipgloss.NewStyle()
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()

	return &InteractiveModel{
		runner:     r,
		config:     cfg,
		ideServer:  ideServer,
		blocks:     []Block{},
		viewport:   vp,
		textArea:   ta,
		ideContext: &ide.IDEContext{},
		keys:       DefaultGlobalKeyMap(),
	}
}

func (m *InteractiveModel) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	inputHeight := 4

	m.viewport.Width = m.width
	m.viewport.Height = m.height - inputHeight

	content := m.buildFlowingContent()
	m.viewport.SetContent(content)

	inputArea := m.buildInputArea()

	return lipgloss.JoinVertical(
		lipgloss.Left,
		m.viewport.View(),
		inputArea,
	)
}

func (m *InteractiveModel) buildFlowingContent() string {
	var content []string

	logoStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("208")).
		Align(lipgloss.Center).
		Width(m.width).
		Padding(2, 0)

	logo := logoStyle.Render(devgruLogo)
	content = append(content, logo, "")

	for i, block := range m.blocks {
		blockContent := m.renderBlock(block, i)
		content = append(content, blockContent)

		// Don't add spacing between child blocks to keep tree connected
		if i < len(m.blocks)-1 {
			nextBlock := m.blocks[i+1]
			// Only add spacing if next block is not a child or if current block is not a parent
			if nextBlock.ParentID == "" && block.ParentID == "" {
				content = append(content, "")
			}
		}
	}

	return strings.Join(content, "\n")
}

func (m *InteractiveModel) buildStatusLine() string {
	var statusLeft string
	if m.ideServer != nil && m.ideServer.IsConnected() {
		statusLeft = fmt.Sprintf("Connected • Workers: %d", len(m.config.Workers))
	} else {
		statusLeft = "Not Connected"
	}

	var statusRight string
	if m.ideContext.ActiveFile != "" {
		statusRight = fmt.Sprintf("📁 %s", m.ideContext.ActiveFile)
	}

	if statusLeft == "" && statusRight == "" {
		return ""
	}

	leftW := lipgloss.Width(statusLeft)
	rightW := lipgloss.Width(statusRight)
	filler := m.width - 4 - leftW - rightW
	if filler < 0 {
		filler = 0
	}

	statusLine := statusLeft + strings.Repeat(" ", filler) + statusRight

	statusStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Width(m.width).
		Padding(0, 1)

	return statusStyle.Render(statusLine)
}

func (m *InteractiveModel) buildInputArea() string {

	statusLine := m.buildStatusLine()

	inputStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("63")).
		Width(m.width-2).
		Padding(0, 1)

	inputContent := m.textArea.View()
	inputSection := inputStyle.Render(inputContent)

	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Padding(0, 1)

	help := helpStyle.Render("enter: submit • ctrl+l: clear • ↑/↓: scroll • ctrl+c: quit")

	return lipgloss.JoinVertical(lipgloss.Left, statusLine, inputSection, help)
}

func (m *InteractiveModel) renderBlock(block Block, index int) string {
	timestamp := block.Timestamp.Format("15:04:05")

	// Determine tree prefix based on parent relationship
	var treePrefix string
	if block.ParentID != "" {
		if block.IsLast {
			treePrefix = "└─ "
		} else {
			treePrefix = "├─ "
		}
	}

	switch block.Type {
	case ChatEntryUser:
		// User input block - distinctive styling
		style := lipgloss.NewStyle().
			Foreground(lipgloss.Color("39")).
			Bold(true).
			Padding(0, 1)

		content := fmt.Sprintf("> %s", block.Content)
		return style.Render(content)

	case ChatEntryPlanning:
		// Planning step block with tree structure
		var style lipgloss.Style
		if block.Status == StatusComplete {
			style = lipgloss.NewStyle().
				Foreground(lipgloss.Color("46")). // Green for completed
				Padding(0, 1)
		} else if block.Status == StatusError {
			style = lipgloss.NewStyle().
				Foreground(lipgloss.Color("196")). // Red for error
				Padding(0, 1)
		} else {
			style = lipgloss.NewStyle().
				Foreground(lipgloss.Color("214")). // Orange for working
				Padding(0, 1)
		}

		icon := m.getStatusIcon(block.Status)
		content := fmt.Sprintf("%s%s %s", treePrefix, icon, block.Content)
		return style.Render(content)

	case ChatEntryResult:
		// Result block with border and tree structure if it has a parent
		style := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("46")).
			Padding(1).
			Width(m.width - 4)

		var content string
		if block.ParentID != "" {
			content = fmt.Sprintf("%s✅ %s", treePrefix, block.Content)
		} else {
			content = fmt.Sprintf("✅ %s", block.Content)
		}
		return style.Render(content)

	case ChatEntryError:
		// Error block with distinctive styling and tree structure if it has a parent
		style := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("196")).
			Padding(1).
			Width(m.width - 4)

		var content string
		if block.ParentID != "" {
			content = fmt.Sprintf("%s%s", treePrefix, block.Content)
		} else {
			content = block.Content
		}
		return style.Render(content)

	case ChatEntrySystem:
		// System message
		style := lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			Italic(true).
			Padding(0, 1)

		return style.Render(fmt.Sprintf("• %s", block.Content))

	default:
		// Default block styling
		style := lipgloss.NewStyle().
			Padding(0, 1)

		content := fmt.Sprintf("%s[%s] %s", treePrefix, timestamp, block.Content)
		return style.Render(content)
	}
}

func (m *InteractiveModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.textArea.SetWidth(msg.Width - 6)
		return m, nil

	case PlanningStepMsg:
		// Add planning step as a child of the current user prompt
		stepID := fmt.Sprintf("step_%d_%d", len(m.blocks), time.Now().UnixNano())
		m.addBlockAsChild(Block{
			ID:        stepID,
			Type:      ChatEntryPlanning,
			Content:   msg.Step,
			Status:    msg.Status,
			Timestamp: time.Now(),
			Data:      msg,
			ParentID:  m.currentUserID,
		})
		return m, nil

	case PlanningCompleteMsg:
		if msg.err != nil {
			m.addBlockAsChild(Block{
				ID:        fmt.Sprintf("error_%d", len(m.blocks)),
				Type:      ChatEntryError,
				Content:   fmt.Sprintf("❌ Planning failed: %s", msg.err.Error()),
				Timestamp: time.Now(),
				ParentID:  m.currentUserID,
				IsLast:    true,
			})
			m.isProcessing = false
		} else {
			// Add the final plan block as child
			planContent := m.formatPlanResult(msg.plan)
			m.addBlockAsChild(Block{
				ID:        fmt.Sprintf("plan_%d", len(m.blocks)),
				Type:      ChatEntryPlanning,
				Content:   planContent,
				Status:    StatusComplete,
				Timestamp: time.Now(),
				Data:      msg.plan,
				ParentID:  m.currentUserID,
			})

			// Auto-execute the plan
			cmds = append(cmds, m.executePlan())
		}
		return m, tea.Batch(cmds...)

	case RunCompleteMsg:
		m.isProcessing = false
		if msg.err != nil {
			m.addBlockAsChild(Block{
				ID:        fmt.Sprintf("error_%d", len(m.blocks)),
				Type:      ChatEntryError,
				Content:   fmt.Sprintf("❌ Execution failed: %s", msg.err.Error()),
				Timestamp: time.Now(),
				ParentID:  m.currentUserID,
				IsLast:    true,
			})
		} else {
			// Add execution result block as child
			resultContent := m.formatRunResult(msg.result)
			m.addBlockAsChild(Block{
				ID:        fmt.Sprintf("result_%d", len(m.blocks)),
				Type:      ChatEntryResult,
				Content:   resultContent,
				Timestamp: time.Now(),
				Data:      msg.result,
				ParentID:  m.currentUserID,
				IsLast:    true,
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
					// Create a new user block
					userID := fmt.Sprintf("user_%d", len(m.blocks))
					m.currentUserID = userID

					m.addBlock(Block{
						ID:        userID,
						Type:      ChatEntryUser,
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
			// Clear all blocks
			m.blocks = []Block{}
			m.currentUserID = ""
			m.isProcessing = false
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

// Block management methods

func (m *InteractiveModel) addBlock(block Block) {
	m.blocks = append(m.blocks, block)
	// Auto-scroll to bottom to show new content
	m.viewport.GotoBottom()
}

func (m *InteractiveModel) addBlockAsChild(block Block) {
	// Mark previous child as not last if this is a new child
	m.updateLastChildStatus(block.ParentID)

	m.blocks = append(m.blocks, block)
	// Auto-scroll to bottom to show new content
	m.viewport.GotoBottom()
}

func (m *InteractiveModel) updateLastChildStatus(parentID string) {
	// Find the last child of this parent and mark it as not last
	for i := len(m.blocks) - 1; i >= 0; i-- {
		if m.blocks[i].ParentID == parentID {
			m.blocks[i].IsLast = false
			break
		}
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
