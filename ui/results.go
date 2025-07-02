package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/evisdrenova/devgru/internal/runner"
)

// ResultsModel represents the TUI for displaying run results
type ResultsModel struct {
	result       *runner.RunResult
	cursor       int
	expanded     map[int]bool // Track which worker sections are expanded
	width        int
	height       int
	keys         KeyMap
	scrollOffset int // Track vertical scroll position
	totalHeight  int // Total height of all content
}

// KeyMap defines the key bindings
type KeyMap struct {
	Up         key.Binding
	Down       key.Binding
	Expand     key.Binding
	Collapse   key.Binding
	ScrollUp   key.Binding
	ScrollDown key.Binding
	PageUp     key.Binding
	PageDown   key.Binding
	Quit       key.Binding
}

// DefaultKeyMap returns the default key bindings
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Up: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑/k", "move up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓/j", "move down"),
		),
		Expand: key.NewBinding(
			key.WithKeys("enter", " "),
			key.WithHelp("enter/space", "expand/collapse"),
		),
		Collapse: key.NewBinding(
			key.WithKeys("c"),
			key.WithHelp("c", "collapse all"),
		),
		ScrollUp: key.NewBinding(
			key.WithKeys("shift+up", "K"),
			key.WithHelp("shift+↑/K", "scroll up"),
		),
		ScrollDown: key.NewBinding(
			key.WithKeys("shift+down", "J"),
			key.WithHelp("shift+↓/J", "scroll down"),
		),
		PageUp: key.NewBinding(
			key.WithKeys("pgup", "ctrl+u"),
			key.WithHelp("pgup/ctrl+u", "page up"),
		),
		PageDown: key.NewBinding(
			key.WithKeys("pgdown", "ctrl+d"),
			key.WithHelp("pgdn/ctrl+d", "page down"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
	}
}

// NewResultsModel creates a new results model
func NewResultsModel(result *runner.RunResult) *ResultsModel {
	expanded := make(map[int]bool)
	// Start with first worker expanded by default
	if len(result.Workers) > 0 {
		expanded[0] = true
	}

	return &ResultsModel{
		result:   result,
		cursor:   0,
		expanded: expanded,
		keys:     DefaultKeyMap(),
	}
}

// Init implements bubbletea.Model
func (m *ResultsModel) Init() tea.Cmd {
	return nil
}

// Update implements bubbletea.Model
func (m *ResultsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit

		case key.Matches(msg, m.keys.Up):
			if m.cursor > 0 {
				m.cursor--
			}

		case key.Matches(msg, m.keys.Down):
			maxCursor := len(m.result.Workers) - 1
			if m.result.Consensus != nil {
				maxCursor++ // Include consensus section
			}
			if m.cursor < maxCursor {
				m.cursor++
			}

		case key.Matches(msg, m.keys.Expand):
			// Only expand/collapse worker sections (not consensus)
			if m.cursor < len(m.result.Workers) {
				m.expanded[m.cursor] = !m.expanded[m.cursor]
			}

		case key.Matches(msg, m.keys.Collapse):
			// Collapse all worker sections
			for i := range m.result.Workers {
				m.expanded[i] = false
			}

		case key.Matches(msg, m.keys.ScrollUp):
			if m.scrollOffset > 0 {
				m.scrollOffset--
			}

		case key.Matches(msg, m.keys.ScrollDown):
			maxScroll := m.totalHeight - m.height + 3 // Leave room for footer
			if maxScroll > 0 && m.scrollOffset < maxScroll {
				m.scrollOffset++
			}

		case key.Matches(msg, m.keys.PageUp):
			pageSize := m.height / 3
			m.scrollOffset -= pageSize
			if m.scrollOffset < 0 {
				m.scrollOffset = 0
			}

		case key.Matches(msg, m.keys.PageDown):
			pageSize := m.height / 3
			maxScroll := m.totalHeight - m.height + 3
			m.scrollOffset += pageSize
			if maxScroll > 0 && m.scrollOffset > maxScroll {
				m.scrollOffset = maxScroll
			}
		}
	}

	return m, nil
}

// View implements bubbletea.Model
func (m *ResultsModel) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	var sections []string

	// Header
	sections = append(sections, m.renderHeader())

	// Worker responses
	for i, worker := range m.result.Workers {
		sections = append(sections, m.renderWorker(i, worker))
	}

	// Consensus
	if m.result.Consensus != nil {
		sections = append(sections, m.renderConsensus())
	}

	// Join all content
	content := lipgloss.JoinVertical(lipgloss.Left, sections...)

	// Calculate total height for scrolling
	contentLines := strings.Split(content, "\n")
	m.totalHeight = len(contentLines)

	// Apply scrolling
	if m.scrollOffset > 0 {
		if m.scrollOffset >= len(contentLines) {
			contentLines = []string{}
		} else {
			contentLines = contentLines[m.scrollOffset:]
		}
	}

	// Limit to viewport height (leave space for footer)
	viewportHeight := m.height - 2 // Reserve space for footer
	if len(contentLines) > viewportHeight {
		contentLines = contentLines[:viewportHeight]
	}

	scrolledContent := strings.Join(contentLines, "\n")

	// Add footer with help and scroll indicator
	footer := m.renderFooter()

	return lipgloss.JoinVertical(lipgloss.Left, scrolledContent, footer)
}

// renderHeader renders the summary header
func (m *ResultsModel) renderHeader() string {
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("39")). // Bright blue
		Background(lipgloss.Color("235")).
		Padding(1, 2).
		Width(m.width - 4).
		Align(lipgloss.Center)

	successIcon := "✅"
	if !m.result.Success {
		successIcon = "❌"
	}

	content := fmt.Sprintf("%s DEVGRU RESULTS %s\n", successIcon, successIcon)
	content += fmt.Sprintf("Duration: %v • Tokens: %d • Cost: $%.6f",
		m.result.TotalDuration.Round(time.Millisecond),
		m.result.TotalTokens,
		m.result.EstimatedCost)

	return headerStyle.Render(content)
}

// renderWorker renders a single worker section
func (m *ResultsModel) renderWorker(index int, worker runner.WorkerResult) string {
	isSelected := m.cursor == index
	isExpanded := m.expanded[index]

	// Determine styling based on state
	var headerStyle lipgloss.Style
	if isSelected {
		headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("15")). // White
			Background(lipgloss.Color("63")). // Purple
			Padding(0, 2)
	} else {
		headerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("247")). // Light gray
			Background(lipgloss.Color("236")).
			Padding(0, 2)
	}

	// Status icon and basic info
	statusIcon := "✅"
	statusColor := lipgloss.Color("46") // Green
	if worker.Error != nil {
		statusIcon = "❌"
		statusColor = lipgloss.Color("196") // Red
	}

	// Expansion indicator
	expandIcon := "▶"
	if isExpanded {
		expandIcon = "▼"
	}

	// Build header line
	headerText := fmt.Sprintf("%s %s %s", expandIcon, statusIcon, worker.WorkerID)
	if worker.Stats != nil {
		headerText += fmt.Sprintf(" (%s, %v)",
			worker.Stats.Model,
			worker.Stats.Duration.Round(time.Millisecond))
	}

	if worker.TokensUsed != nil {
		headerText += fmt.Sprintf(" • %d tokens • $%.6f",
			worker.TokensUsed.TotalTokens,
			worker.Stats.EstimatedCost)
	}

	// Add average score if available
	if len(worker.JudgeResults) > 0 {
		headerText += fmt.Sprintf(" • Score: %.1f/10", worker.AverageScore)
	}

	header := headerStyle.Width(m.width - 4).Render(headerText)

	// If not expanded, just return header
	if !isExpanded {
		return header
	}

	// Expanded content
	contentStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252")).
		Background(lipgloss.Color("234")).
		Padding(1, 2).
		Width(m.width - 8).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(statusColor)

	var content string
	if worker.Error != nil {
		content = fmt.Sprintf("Error: %v", worker.Error)
	} else {
		content = worker.Content

		// Add judge results if available
		if len(worker.JudgeResults) > 0 {
			content += "\n\n" + m.renderJudgeResults(worker.JudgeResults, worker.AverageScore)
		}

		// Wrap long content
		if len(content) > m.width-12 {
			content = wrapText(content, m.width-12)
		}
	}

	expandedContent := contentStyle.Render(content)

	return lipgloss.JoinVertical(lipgloss.Left, header, expandedContent)
}

// renderConsensus renders the consensus section
func (m *ResultsModel) renderConsensus() string {
	consensus := m.result.Consensus
	isSelected := m.cursor == len(m.result.Workers)

	var style lipgloss.Style
	if isSelected {
		style = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("15")).  // White
			Background(lipgloss.Color("202")). // Orange
			Padding(1, 2)
	} else {
		style = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("214")). // Yellow
			Background(lipgloss.Color("237")).
			Padding(1, 2)
	}

	title := "🏆 CONSENSUS RESULT"
	var content strings.Builder
	content.WriteString(fmt.Sprintf("%s\n\n", title))

	// Basic info
	content.WriteString(fmt.Sprintf("Algorithm: %s\n", consensus.Algorithm))
	content.WriteString(fmt.Sprintf("Winner: %s\n", consensus.Winner))
	content.WriteString(fmt.Sprintf("Confidence: %.2f\n", consensus.Confidence))
	content.WriteString(fmt.Sprintf("Participants: %d\n", consensus.Participants))

	if consensus.Reasoning != "" {
		content.WriteString("\nReasoning:\n")
		wrappedReasoning := wrapText(consensus.Reasoning, m.width-8)
		content.WriteString(wrappedReasoning)
		content.WriteString("\n")
	}

	content.WriteString("\nFinal Answer:\n")

	// Word wrap the final answer to prevent horizontal scrolling
	finalAnswer := consensus.Content
	if len(finalAnswer) > 0 {
		wrappedAnswer := wrapText(finalAnswer, m.width-8)
		content.WriteString(wrappedAnswer)
	}

	return style.Width(m.width - 4).Render(content.String())
}

// renderFooter renders the help footer with scroll indicators
func (m *ResultsModel) renderFooter() string {
	footerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")). // Dark gray
		Background(lipgloss.Color("233")).
		Padding(0, 2).
		Width(m.width - 4)

	// Build help text
	help := "↑/↓: navigate • enter/space: expand/collapse • c: collapse all"

	// Add scroll indicators if content is scrollable
	maxScroll := m.totalHeight - m.height + 3
	if maxScroll > 0 {
		help += " • Shift+↑/↓: scroll • PgUp/PgDn: page"

		// Add scroll position indicator
		scrollPercent := float64(m.scrollOffset) / float64(maxScroll) * 100
		help += fmt.Sprintf(" • Scroll: %d%% (%d/%d)", int(scrollPercent), m.scrollOffset, maxScroll)
	}

	help += " • q: quit"

	return footerStyle.Render(help)
}

// renderJudgeResults renders the judge evaluation results
func (m *ResultsModel) renderJudgeResults(judgeResults []runner.JudgeResult, averageScore float64) string {
	if len(judgeResults) == 0 {
		return ""
	}

	judgeStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("214")). // Yellow
		Bold(true)

	var content strings.Builder
	content.WriteString(judgeStyle.Render("📊 JUDGE EVALUATIONS"))
	content.WriteString(fmt.Sprintf(" (Average: %.1f/10)\n", averageScore))

	for _, result := range judgeResults {
		scoreColor := lipgloss.Color("196") // Red
		if result.Score >= 7 {
			scoreColor = lipgloss.Color("46") // Green
		} else if result.Score >= 5 {
			scoreColor = lipgloss.Color("214") // Yellow
		}

		scoreStyle := lipgloss.NewStyle().Foreground(scoreColor).Bold(true)

		content.WriteString(fmt.Sprintf("• %s: ", result.JudgeID))
		content.WriteString(scoreStyle.Render(fmt.Sprintf("%d/10", result.Score)))
		content.WriteString(fmt.Sprintf(" (%v)\n", result.Duration.Round(time.Millisecond)))

		if result.Reason != "" {
			// Wrap the reason text
			wrappedReason := wrapText(result.Reason, m.width-16)
			lines := strings.Split(wrappedReason, "\n")
			for _, line := range lines {
				content.WriteString(fmt.Sprintf("  %s\n", line))
			}
		}
		content.WriteString("\n")
	}

	return content.String()
}

// wrapText wraps text to the specified width
func wrapText(text string, width int) string {
	if width <= 0 {
		return text
	}

	words := strings.Fields(text)
	if len(words) == 0 {
		return text
	}

	var lines []string
	var currentLine strings.Builder

	for _, word := range words {
		// If adding this word would exceed width, start new line
		if currentLine.Len() > 0 && currentLine.Len()+1+len(word) > width {
			lines = append(lines, currentLine.String())
			currentLine.Reset()
		}

		if currentLine.Len() > 0 {
			currentLine.WriteString(" ")
		}
		currentLine.WriteString(word)
	}

	if currentLine.Len() > 0 {
		lines = append(lines, currentLine.String())
	}

	return strings.Join(lines, "\n")
}
