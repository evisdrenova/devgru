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
	result   *runner.RunResult
	cursor   int
	expanded map[int]bool // Track which worker sections are expanded
	width    int
	height   int
	keys     KeyMap
}

// KeyMap defines the key bindings
type KeyMap struct {
	Up       key.Binding
	Down     key.Binding
	Expand   key.Binding
	Collapse key.Binding
	Quit     key.Binding
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

	// Footer with help
	sections = append(sections, m.renderFooter())

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
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
	content := fmt.Sprintf("%s\n\n", title)
	content += fmt.Sprintf("Algorithm: %s\n", consensus.Algorithm)
	content += fmt.Sprintf("Winner: %s\n", consensus.Winner)
	content += fmt.Sprintf("Confidence: %.2f\n", consensus.Confidence)
	content += fmt.Sprintf("Participants: %d\n\n", consensus.Participants)

	if consensus.Reasoning != "" {
		content += fmt.Sprintf("Reasoning: %s\n\n", consensus.Reasoning)
	}

	content += "Final Answer:\n"
	finalAnswer := consensus.Content
	if len(finalAnswer) > m.width-8 {
		finalAnswer = wrapText(finalAnswer, m.width-8)
	}
	content += finalAnswer

	return style.Width(m.width - 4).Render(content)
}

// renderFooter renders the help footer
func (m *ResultsModel) renderFooter() string {
	footerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")). // Dark gray
		Background(lipgloss.Color("233")).
		Padding(0, 2).
		Width(m.width - 4)

	help := "↑/↓: navigate • enter/space: expand/collapse • c: collapse all • q: quit"
	return footerStyle.Render(help)
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
