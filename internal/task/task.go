package task

import (
	"fmt"
	"sync"
	"time"

	"github.com/exedev/waggle/internal/bus"
)

type Status string

const (
	StatusPending   Status = "pending"
	StatusAssigned  Status = "assigned"
	StatusRunning   Status = "running"
	StatusComplete  Status = "complete"
	StatusFailed    Status = "failed"
	StatusRetrying  Status = "retrying"
	StatusCancelled Status = "cancelled"
)

type Priority int

const (
	PriorityLow      Priority = 0
	PriorityNormal   Priority = 1
	PriorityHigh     Priority = 2
	PriorityCritical Priority = 3
)

type Type string

const (
	TypeCode     Type = "code"
	TypeResearch Type = "research"
	TypeTest     Type = "test"
	TypeReview   Type = "review"
	TypeGeneric  Type = "generic"
)

type Task struct {
	ID            string            `json:"id"`
	ParentID      string            `json:"parent_id,omitempty"`
	Type          Type              `json:"type"`
	Status        Status            `json:"status"`
	Priority      Priority          `json:"priority"`
	Title         string            `json:"title"`
	Description   string            `json:"description"`
	Constraints   []string          `json:"constraints,omitempty"`
	Context       map[string]string `json:"context,omitempty"`
	AllowedPaths  []string          `json:"allowed_paths,omitempty"`
	WorkerID      string            `json:"worker_id,omitempty"`
	Result        *Result           `json:"result,omitempty"`
	MaxRetries    int               `json:"max_retries"`
	RetryCount    int               `json:"retry_count"`
	LastError     string            `json:"last_error,omitempty"`
	LastErrorType string            `json:"last_error_type,omitempty"`
	CreatedAt     time.Time         `json:"created_at"`
	StartedAt     *time.Time        `json:"started_at,omitempty"`
	CompletedAt   *time.Time        `json:"completed_at,omitempty"`
	Timeout       time.Duration     `json:"timeout,omitempty"`
	DependsOn     []string          `json:"depends_on,omitempty"`
}

type Result struct {
	Success   bool               `json:"success"`
	Output    string             `json:"output"`
	Errors    []string           `json:"errors,omitempty"`
	Artifacts map[string]string  `json:"artifacts,omitempty"`
	Metrics   map[string]float64 `json:"metrics,omitempty"`
}

// TaskGraph manages tasks and their dependencies
type TaskGraph struct {
	mu    sync.RWMutex
	tasks map[string]*Task
	bus   *bus.MessageBus
}

func NewTaskGraph(b *bus.MessageBus) *TaskGraph {
	return &TaskGraph{
		tasks: make(map[string]*Task),
		bus:   b,
	}
}

func (g *TaskGraph) Add(t *Task) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.tasks[t.ID] = t
	if g.bus != nil {
		g.bus.Publish(bus.Message{
			Type:    bus.MsgTaskCreated,
			TaskID:  t.ID,
			Payload: t,
			Time:    time.Now(),
		})
	}
}

func (g *TaskGraph) Get(id string) (*Task, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	t, ok := g.tasks[id]
	return t, ok
}

func (g *TaskGraph) UpdateStatus(id string, status Status) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	t, ok := g.tasks[id]
	if !ok {
		return fmt.Errorf("task %s not found", id)
	}
	old := t.Status
	t.Status = status
	now := time.Now()
	switch status {
	case StatusRunning:
		t.StartedAt = &now
	case StatusComplete, StatusFailed, StatusCancelled:
		t.CompletedAt = &now
	}
	if g.bus != nil {
		g.bus.Publish(bus.Message{
			Type:    bus.MsgTaskStatusChanged,
			TaskID:  id,
			Payload: map[string]Status{"old": old, "new": status},
			Time:    now,
		})
	}
	return nil
}

func (g *TaskGraph) Ready() []*Task {
	g.mu.RLock()
	defer g.mu.RUnlock()
	var ready []*Task
	for _, t := range g.tasks {
		if t.Status != StatusPending {
			continue
		}
		allDone := true
		for _, depID := range t.DependsOn {
			dep, ok := g.tasks[depID]
			if !ok || dep.Status != StatusComplete {
				allDone = false
				break
			}
		}
		if allDone {
			ready = append(ready, t)
		}
	}
	return ready
}

func (g *TaskGraph) AllComplete() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	for _, t := range g.tasks {
		if t.Status != StatusComplete && t.Status != StatusCancelled {
			return false
		}
	}
	return len(g.tasks) > 0
}

func (g *TaskGraph) All() []*Task {
	g.mu.RLock()
	defer g.mu.RUnlock()
	all := make([]*Task, 0, len(g.tasks))
	for _, t := range g.tasks {
		all = append(all, t)
	}
	return all
}

func (g *TaskGraph) Failed() []*Task {
	g.mu.RLock()
	defer g.mu.RUnlock()
	var failed []*Task
	for _, t := range g.tasks {
		if t.Status == StatusFailed {
			failed = append(failed, t)
		}
	}
	return failed
}

// DetectCycles detects circular dependencies in the task graph using DFS.
// Returns an error describing the cycle if found, or nil if no cycles exist.
func (g *TaskGraph) DetectCycles() error {
	g.mu.RLock()
	defer g.mu.RUnlock()

	// Track visited nodes (fully processed)
	visited := make(map[string]bool)
	// Track nodes in current recursion stack (being processed)
	recStack := make(map[string]bool)

	for id := range g.tasks {
		if !visited[id] {
			if cycle := g.detectCycleDFS(id, visited, recStack, []string{}); cycle != nil {
				return fmt.Errorf("circular dependency detected: %s", formatCycle(cycle))
			}
		}
	}
	return nil
}

// detectCycleDFS performs DFS from the given node to detect cycles.
// Returns the cycle path if a cycle is detected, nil otherwise.
func (g *TaskGraph) detectCycleDFS(nodeID string, visited, recStack map[string]bool, path []string) []string {
	visited[nodeID] = true
	recStack[nodeID] = true
	path = append(path, nodeID)

	task, ok := g.tasks[nodeID]
	if !ok {
		recStack[nodeID] = false
		return nil
	}

	for _, depID := range task.DependsOn {
		// Skip if dependency doesn't exist in the graph
		if _, exists := g.tasks[depID]; !exists {
			continue
		}

		if recStack[depID] {
			// Cycle found - construct the cycle path
			cycleStart := -1
			for i, id := range path {
				if id == depID {
					cycleStart = i
					break
				}
			}
			if cycleStart >= 0 {
				cycle := append(path[cycleStart:], depID)
				return cycle
			}
		}

		if !visited[depID] {
			if cycle := g.detectCycleDFS(depID, visited, recStack, path); cycle != nil {
				return cycle
			}
		}
	}

	recStack[nodeID] = false
	return nil
}

// formatCycle formats a cycle path as a string like "a -> b -> c -> a"
func formatCycle(cycle []string) string {
	if len(cycle) == 0 {
		return ""
	}
	result := cycle[0]
	for i := 1; i < len(cycle); i++ {
		result += " -> " + cycle[i]
	}
	return result
}
