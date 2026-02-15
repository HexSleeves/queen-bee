package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	maxQueenLines  = 200
	maxLogLines    = 100
	tickInterval   = time.Second
)

type viewMode int

const (
	viewQueen  viewMode = iota
	viewWorker
)
// TaskInfo tracks task state for display.
type TaskInfo struct {
	ID       string
	Title    string
	Type     string
	Status   string
	WorkerID string
	Order    int // insertion order
}

// WorkerInfo tracks worker state for display.
type WorkerInfo struct {
	ID      string
	TaskID  string
	Status  string
	Adapter string
	Started time.Time
}

// Model is the Bubble Tea model for the Waggle TUI.
type Model struct {
	// Content
	objective  string
	queenLines []queenLine    // Queen panel lines
	tasks      []TaskInfo     // ordered task list
	taskMap    map[string]int // task ID -> index in tasks slice
	workers    map[string]*WorkerInfo

	// State
	turn      int
	maxTurn   int
	startTime time.Time
	done      bool
	success   bool
	finalMsg  string

	// UI state
	width         int
	height        int
	queenScroll   int // scroll offset for queen panel (from bottom)
	viewMode      viewMode
	viewWorkerID  string              // which worker we're viewing
	workerOutputs map[string][]string // worker ID -> output lines
	workerOrder   []string            // stable insertion-order list of worker IDs
	workerScroll  int                 // scroll offset for worker view (from bottom)
	workerTasks   map[string]string   // worker ID -> task title

	// For tick
	quitting bool
}

type queenLine struct {
	text  string
	style string // "think", "tool", "result", "error", "info"
}

// New creates a new TUI model.
func New(objective string, maxTurns int) Model {
	return Model{
		objective:     objective,
		queenLines:    []queenLine{},
		tasks:         []TaskInfo{},
		taskMap:       make(map[string]int),
		workers:       make(map[string]*WorkerInfo),
		maxTurn:       maxTurns,
		startTime:     time.Now(),
		workerOutputs: make(map[string][]string),
		workerTasks:   make(map[string]string),
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(tickCmd(), tea.WindowSize())
}

func tickCmd() tea.Cmd {
	return tea.Tick(tickInterval, func(t time.Time) tea.Msg {
		return TickMsg{}
	})
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "up", "k":
			if m.viewMode == viewWorker {
				lines := m.workerOutputs[m.viewWorkerID]
				if m.workerScroll < len(lines)-1 {
					m.workerScroll++
				}
			} else {
				if m.queenScroll < len(m.queenLines)-1 {
					m.queenScroll++
				}
			}
		case "down", "j":
			if m.viewMode == viewWorker {
				if m.workerScroll > 0 {
					m.workerScroll--
				}
			} else {
				if m.queenScroll > 0 {
					m.queenScroll--
				}
			}
		case "tab":
			m.cycleView(1)
		case "shift+tab":
			m.cycleView(-1)
		case "0":
			m.viewMode = viewQueen
			m.queenScroll = 0
		case "right", "l":
			if m.viewMode == viewWorker {
				m.cycleView(1)
			} else if len(m.workerOrder) > 0 {
				m.viewMode = viewWorker
				m.viewWorkerID = m.workerOrder[0]
				m.workerScroll = 0
			}
		case "left", "h":
			if m.viewMode == viewWorker {
				m.cycleView(-1)
			}
		default:
			// Any other key quits after done
			if m.done {
				m.quitting = true
				return m, tea.Quit
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case TickMsg:
		return m, tickCmd()

	case QueenThinkingMsg:
		m.addQueenLine(msg.Text, "think")
		m.queenScroll = 0 // auto-scroll to bottom

	case ToolCallMsg:
		line := "→ " + msg.Name
		if msg.Input != "" {
			input := msg.Input
			if len(input) > 80 {
				input = input[:80] + "..."
			}
			line += "(" + input + ")"
		}
		m.addQueenLine(line, "tool")
		m.queenScroll = 0

	case ToolResultMsg:
		style := "result"
		if msg.IsError {
			style = "error"
		}
		// Split multiline results into separate lines
		result := strings.TrimSpace(msg.Result)
		lines := strings.Split(result, "\n")
		if len(lines) > 8 {
			// Summarize very long results
			for _, l := range lines[:6] {
				m.addQueenLine("  "+strings.TrimSpace(l), style)
			}
			m.addQueenLine(fmt.Sprintf("  ... (%d more lines)", len(lines)-6), "info")
		} else {
			for _, l := range lines {
				l = strings.TrimSpace(l)
				if l != "" {
					m.addQueenLine("  "+l, style)
				}
			}
		}
		m.queenScroll = 0

	case TaskUpdateMsg:
		m.updateTask(msg)

	case WorkerUpdateMsg:
		m.updateWorker(msg)

	case TurnMsg:
		m.turn = msg.Turn
		m.maxTurn = msg.MaxTurn
		m.addQueenLine("", "info") // blank separator

	case DoneMsg:
		m.done = true
		m.success = msg.Success
		if msg.Error != "" {
			m.finalMsg = msg.Error
			m.addQueenLine("❌ "+msg.Error, "error")
		} else {
			m.finalMsg = msg.Summary
			m.addQueenLine("✅ "+msg.Summary, "info")
		}
		m.addQueenLine("", "info")
		m.addQueenLine("Press any key to exit...", "info")
		// Keep ticking so the view stays rendered
		return m, tickCmd()

	case WorkerOutputMsg:
		lines := strings.Split(msg.Output, "\n")
		m.workerOutputs[msg.WorkerID] = lines
		// Track insertion order
		found := false
		for _, id := range m.workerOrder {
			if id == msg.WorkerID {
				found = true
				break
			}
		}
		if !found {
			m.workerOrder = append(m.workerOrder, msg.WorkerID)
		}
		// Auto-scroll if viewing this worker
		if m.viewMode == viewWorker && m.viewWorkerID == msg.WorkerID {
			m.workerScroll = 0
		}

	case LogMsg:
		m.addQueenLine(msg.Text, "info")
		m.queenScroll = 0
	}

	return m, nil
}

func (m *Model) cycleView(direction int) {
	if len(m.workerOrder) == 0 {
		m.viewMode = viewQueen
		return
	}

	if m.viewMode == viewQueen {
		// Enter worker view
		m.viewMode = viewWorker
		if direction > 0 {
			m.viewWorkerID = m.workerOrder[0]
		} else {
			m.viewWorkerID = m.workerOrder[len(m.workerOrder)-1]
		}
		m.workerScroll = 0
		return
	}

	// Find current index
	idx := -1
	for i, id := range m.workerOrder {
		if id == m.viewWorkerID {
			idx = i
			break
		}
	}

	next := idx + direction
	if next < 0 || next >= len(m.workerOrder) {
		// Wrap back to queen
		m.viewMode = viewQueen
		m.queenScroll = 0
		return
	}

	m.viewWorkerID = m.workerOrder[next]
	m.workerScroll = 0
}

func (m *Model) addQueenLine(text, style string) {
	m.queenLines = append(m.queenLines, queenLine{text: text, style: style})
	if len(m.queenLines) > maxQueenLines {
		m.queenLines = m.queenLines[len(m.queenLines)-maxQueenLines:]
	}
}

func (m *Model) updateTask(msg TaskUpdateMsg) {
	if idx, ok := m.taskMap[msg.ID]; ok {
		m.tasks[idx].Status = msg.Status
		if msg.WorkerID != "" {
			m.tasks[idx].WorkerID = msg.WorkerID
		}
	} else {
		m.taskMap[msg.ID] = len(m.tasks)
		m.tasks = append(m.tasks, TaskInfo{
			ID:       msg.ID,
			Title:    msg.Title,
			Type:     msg.Type,
			Status:   msg.Status,
			WorkerID: msg.WorkerID,
			Order:    len(m.tasks),
		})
	}
}

func (m *Model) updateWorker(msg WorkerUpdateMsg) {
	if msg.Status == "done" || msg.Status == "failed" {
		delete(m.workers, msg.ID)
		return
	}
	m.workers[msg.ID] = &WorkerInfo{
		ID:      msg.ID,
		TaskID:  msg.TaskID,
		Status:  msg.Status,
		Adapter: msg.Adapter,
		Started: time.Now(),
	}
	// Track worker→task mapping for display
	if msg.TaskID != "" {
		// Look up task title
		if idx, ok := m.taskMap[msg.TaskID]; ok && idx < len(m.tasks) {
			m.workerTasks[msg.ID] = m.tasks[idx].Title
		} else {
			m.workerTasks[msg.ID] = msg.TaskID
		}
	}
}
