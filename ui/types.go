package ui

import (
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/evisdrenova/devgru/internal/config"
	"github.com/evisdrenova/devgru/internal/ide"
	"github.com/evisdrenova/devgru/internal/runner"
)

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

// Use the runner types instead of duplicating
type PlanStepType = runner.PlanStepType

const (
	PlanStepRead   = runner.PlanStepRead
	PlanStepUpdate = runner.PlanStepUpdate
	PlanStepCreate = runner.PlanStepCreate
	PlanStepDelete = runner.PlanStepDelete
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
	plan *runner.PlanResult
	err  error
}

// Use the runner types instead of duplicating
type PlanResult = runner.PlanResult
type PlanStep = runner.PlanStep

type WorkerPlan struct {
	WorkerID string
	Model    string
	Plan     string
	Score    float64
}

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

	currentPrompt   string
	isProcessing    bool
	processingSteps map[string]int

	ideContext *ide.IDEContext

	keys GlobalKeyMap
}

type GlobalKeyMap struct {
	Submit key.Binding
	Clear  key.Binding
	Quit   key.Binding
	Up     key.Binding
	Down   key.Binding
}
