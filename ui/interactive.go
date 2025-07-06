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
	"github.com/evisdrenova/devgru/internal/runner"
)

//go:embed devgru_logo.txt
var devgruLogo string

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
		m.tickTimer(),
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
		runner:          r,
		config:          cfg,
		ideServer:       ideServer,
		blocks:          []Block{},
		viewport:        vp,
		textArea:        ta,
		ideContext:      &ide.IDEContext{},
		keys:            DefaultGlobalKeyMap(),
		processingSteps: make(map[string]int),
		lastTimerUpdate: time.Now(),
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
		blockContent := m.renderBlock(block)
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

func (m *InteractiveModel) renderBlock(block Block) string {
	timestamp := block.Timestamp.Format("15:04:05")

	treePrefix := "• "

	switch block.Type {
	case BlockEntryUser:
		style := lipgloss.NewStyle().
			Foreground(lipgloss.Color("39")).
			Bold(true).
			Padding(0, 1)

		content := fmt.Sprintf("> %s", block.Content)
		return style.Render(content)

	case BlockEntryPlanning:
		var style lipgloss.Style

		switch block.Status {
		case StatusComplete:
			style = lipgloss.NewStyle().
				Foreground(lipgloss.Color("28")). // Green
				Padding(0, 1)
		case StatusError:
			style = lipgloss.NewStyle().
				Foreground(lipgloss.Color("196")). // Red
				Padding(0, 1)

		default:
			style = lipgloss.NewStyle().
				Foreground(lipgloss.Color("214")). // Orange
				Padding(0, 1)
		}

		icon := m.getStatusIcon(block.Status)
		timer := m.getTimerDisplay(block)
		content := fmt.Sprintf("%s%s %s%s", treePrefix, block.Content, icon, timer)
		return style.Render(content)

	case BlockEntryResult:
		// Result block with border and tree structure if it has a parent
		style := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("28")).
			Padding(1).
			Width(m.width - 4)

		var content string
		if block.ParentID != "" {
			content = fmt.Sprintf("%s%s ✓", treePrefix, block.Content)
		} else {
			content = fmt.Sprintf("%s ✓", block.Content)
		}
		return style.Render(content)

	case BlockEntryError:
		// Error block with distinctive styling and tree structure if it has a parent
		style := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("196")).
			Padding(1).
			Width(m.width - 4)

		var content string
		if block.ParentID != "" {
			content = fmt.Sprintf("%s%s ✗", treePrefix, block.Content)
		} else {
			content = fmt.Sprintf("%s ✗", block.Content)
		}
		return style.Render(content)

	case BlockEntrySystem:
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
		// Check if this step already exists and update it in place
		stepKey := msg.Step
		if existingIndex, exists := m.processingSteps[stepKey]; exists {
			// Update existing block
			if existingIndex < len(m.blocks) {
				// Set start time if transitioning to working status
				if m.blocks[existingIndex].Status != StatusWorking && msg.Status == StatusWorking {
					m.blocks[existingIndex].StartTime = time.Now()
				}
				m.blocks[existingIndex].Status = msg.Status
				// Update content based on status and step type
				switch stepKey {
				case "analyze":
					if msg.Status == StatusWorking {
						m.blocks[existingIndex].Content = "Analyzing request"
					} else {
						m.blocks[existingIndex].Content = "Request analyzed"
					}
				case "generate":
					if msg.Status == StatusWorking {
						m.blocks[existingIndex].Content = "Generating detailed plan"
					} else {
						m.blocks[existingIndex].Content = "Plan generated"
					}
				}
			}
		} else {
			// Add new planning step as a child of the current user prompt
			stepID := fmt.Sprintf("step_%d_%d", len(m.blocks), time.Now().UnixNano())
			m.processingSteps[stepKey] = len(m.blocks)

			var content string
			switch stepKey {
			case "analyze":
				content = "Analyzing request"
			case "generate":
				content = "Generating detailed plan"
			default:
				content = msg.Step
			}

			m.addBlockAsChild(Block{
				ID:        stepID,
				Type:      BlockEntryPlanning,
				Content:   content,
				Status:    msg.Status,
				Timestamp: time.Now(),
				Data:      msg,
				ParentID:  m.currentUserID,
				StartTime: time.Now(),
			})
		}
		return m, nil

	case PlanningCompleteMsg:
		if msg.err != nil {
			m.addBlockAsChild(Block{
				ID:        fmt.Sprintf("error_%d", len(m.blocks)),
				Type:      BlockEntryError,
				Content:   fmt.Sprintf("Planning failed: %s", msg.err.Error()),
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
				Type:      BlockEntryPlanning,
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
				Type:      BlockEntryError,
				Content:   fmt.Sprintf("Execution failed: %s", msg.err.Error()),
				Timestamp: time.Now(),
				ParentID:  m.currentUserID,
				IsLast:    true,
			})
		} else {
			// Add execution result block as child
			resultContent := m.formatRunResult(msg.result)
			m.addBlockAsChild(Block{
				ID:        fmt.Sprintf("result_%d", len(m.blocks)),
				Type:      BlockEntryResult,
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
						Type:      BlockEntryUser,
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
			m.processingSteps = make(map[string]int)
			m.isProcessing = false
			m.lastTimerUpdate = time.Now()
			return m, nil

		case key.Matches(msg, m.keys.Up):
			m.viewport.ScrollUp(1)
			return m, nil

		case key.Matches(msg, m.keys.Down):
			m.viewport.ScrollDown(1)
			return m, nil
		}
	}

	m.textArea, cmd = m.textArea.Update(msg)
	cmds = append(cmds, cmd)

	m.viewport, cmd = m.viewport.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

func (m *InteractiveModel) addBlock(block Block) {
	m.blocks = append(m.blocks, block)
	m.viewport.GotoBottom()
}

func (m *InteractiveModel) addBlockAsChild(block Block) {
	m.updateLastChildStatus(block.ParentID)

	m.blocks = append(m.blocks, block)
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

func (m *InteractiveModel) formatPlanResult(plan *runner.PlanResult) string {
	var content string

	// Show the actual plan reasoning/content
	if plan.Reasoning != "" {
		content += plan.Reasoning
	}

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
	var content string

	if len(result.Workers) > 0 {
		content += "\n\nResults:"
		for _, worker := range result.Workers {
			if worker.Error != nil {
				content += fmt.Sprintf("\n✗ %s: %s", worker.WorkerID, worker.Error.Error())
			} else {
				// Truncate long content for display
				workerContent := worker.Content
				if len(workerContent) > 200 {
					workerContent = workerContent[:200] + "..."
				}
				content += fmt.Sprintf("\n✓ %s: %s", worker.WorkerID, workerContent)
			}
		}
	}

	return content
}

func (m *InteractiveModel) startPlanning(prompt string) tea.Cmd {
	return tea.Batch(
		// First step: Analyzing request
		func() tea.Msg {
			return PlanningStepMsg{
				Step:        "analyze",
				Description: "Understanding the context and requirements",
				Status:      StatusWorking,
			}
		},
		m.runPlanningProcess(),
	)
}

func (m *InteractiveModel) getStatusIcon(status StepStatus) string {
	switch status {
	case StatusWorking:
		return "⠋"
	case StatusComplete:
		return "✓"
	case StatusError:
		return "✗"
	default:
		return "•"
	}
}

func (m *InteractiveModel) runPlanningProcess() tea.Cmd {
	return tea.Sequence(
		// Complete the analyze step
		tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
			return PlanningStepMsg{
				Step:        "analyze",
				Description: "Context and requirements understood",
				Status:      StatusComplete,
			}
		}),
		// Start the generate step
		tea.Tick(600*time.Millisecond, func(t time.Time) tea.Msg {
			return PlanningStepMsg{
				Step:        "generate",
				Description: "Generating detailed plan",
				Status:      StatusWorking,
			}
		}),
		// Actually generate the plan
		func() tea.Msg {
			plan, err := m.runner.GeneratePlan(m.currentPrompt, m.ideContext)
			if err != nil {
				return PlanningCompleteMsg{plan: nil, err: err}
			}
			return PlanningCompleteMsg{plan: plan}
		},
	)
}

func (m *InteractiveModel) executePlan() tea.Cmd {
	return func() tea.Msg {
		// Get the latest plan from the last PlanningCompleteMsg
		var plan *runner.PlanResult
		for i := len(m.blocks) - 1; i >= 0; i-- {
			if m.blocks[i].Type == BlockEntryPlanning && m.blocks[i].Data != nil {
				if planResult, ok := m.blocks[i].Data.(*runner.PlanResult); ok {
					plan = planResult
					break
				}
			}
		}

		if plan == nil {
			return RunCompleteMsg{result: nil, err: fmt.Errorf("no plan found to execute")}
		}

		result, err := m.runner.ExecutePlan(plan, m.ideContext)
		return RunCompleteMsg{result: result, err: err}
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

func (m *InteractiveModel) tickTimer() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return TimerUpdateMsg{timestamp: t}
	})
}

func (m *InteractiveModel) updateTimers() {
	now := time.Now()
	for i := range m.blocks {
		if m.blocks[i].Status == StatusWorking && !m.blocks[i].StartTime.IsZero() {
			m.blocks[i].Duration = now.Sub(m.blocks[i].StartTime)
		}
	}
}

func (m *InteractiveModel) getTimerDisplay(block Block) string {
	if block.Status == StatusWorking && !block.StartTime.IsZero() {
		duration := block.Duration
		if duration.Seconds() < 60 {
			return fmt.Sprintf(" (%.1fs)", duration.Seconds())
		} else {
			return fmt.Sprintf(" (%.0fm %.0fs)", duration.Minutes(), duration.Seconds()-60*duration.Minutes())
		}
	}
	return ""
}
