package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/exedev/queen-bee/internal/config"
	"github.com/exedev/queen-bee/internal/queen"
	"github.com/exedev/queen-bee/internal/state"
	"github.com/exedev/queen-bee/internal/task"
)

const version = "0.1.0"

const usage = `Queen Bee - Agent Orchestration System v%s

Usage:
  queen-bee run <objective>      Run the queen with an objective
  queen-bee status               Show status of current hive session
  queen-bee resume               Resume an interrupted session
  queen-bee init                 Initialize a .hive directory
  queen-bee config               Show current configuration
  queen-bee version              Show version
  queen-bee help                 Show this help

Examples:
  queen-bee run "Refactor the auth module to use JWT tokens"
  queen-bee run "Add comprehensive tests for the API layer"
  queen-bee --config queen.json run "Build a REST API"
  queen-bee --adapter exec run "go test ./..."
  queen-bee --tasks tasks.json run "Execute planned tasks"

Options:
  --config <path>    Path to config file (default: queen.json)
  --project <path>   Project directory (default: current directory)
  --adapter <name>   Default adapter: claude-code, codex, opencode, exec
  --workers <n>      Max parallel workers (default: 4)
  --tasks <path>     Load pre-defined tasks from a JSON file
  --verbose          Verbose logging
`

func main() {
	logger := log.New(os.Stderr, "", log.LstdFlags)

	args := os.Args[1:]
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, usage, version)
		os.Exit(1)
	}

	// Parse flags
	configPath := "queen.json"
	projectDir := "."
	defaultAdapter := ""
	maxWorkers := 0
	tasksFile := ""
	verbose := false

	var positional []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--config":
			if i+1 < len(args) {
				i++
				configPath = args[i]
			}
		case "--project":
			if i+1 < len(args) {
				i++
				projectDir = args[i]
			}
		case "--adapter":
			if i+1 < len(args) {
				i++
				defaultAdapter = args[i]
			}
		case "--workers":
			if i+1 < len(args) {
				i++
				fmt.Sscanf(args[i], "%d", &maxWorkers)
			}
		case "--tasks":
			if i+1 < len(args) {
				i++
				tasksFile = args[i]
			}
		case "--verbose", "-v":
			verbose = true
		default:
			positional = append(positional, args[i])
		}
	}

	if len(positional) == 0 {
		fmt.Fprintf(os.Stderr, usage, version)
		os.Exit(1)
	}

	cmd := positional[0]

	switch cmd {
	case "version":
		fmt.Printf("queen-bee v%s\n", version)
		return

	case "help", "--help", "-h":
		fmt.Printf(usage, version)
		return

	case "init":
		cmdInit(projectDir, configPath, logger)
		return

	case "config":
		cmdConfig(configPath, logger)
		return

	case "status":
		cmdStatus(projectDir, logger)
		return

	case "run":
		if len(positional) < 2 {
			logger.Fatal("Usage: queen-bee run <objective>")
		}
		objective := strings.Join(positional[1:], " ")
		cmdRun(objective, configPath, projectDir, defaultAdapter, maxWorkers, tasksFile, verbose, logger)
		return

	case "resume":
		cmdResume(configPath, projectDir, defaultAdapter, maxWorkers, verbose, logger)
		return

	default:
		// Treat as implicit "run" if it's not a known command
		objective := strings.Join(positional, " ")
		cmdRun(objective, configPath, projectDir, defaultAdapter, maxWorkers, tasksFile, verbose, logger)
	}
}

func loadConfig(configPath, projectDir, defaultAdapter string, maxWorkers int) *config.Config {
	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("Load config: %v", err)
	}

	if projectDir != "." {
		cfg.ProjectDir = projectDir
	}
	if defaultAdapter != "" {
		cfg.Workers.DefaultAdapter = defaultAdapter
	}
	if maxWorkers > 0 {
		cfg.Workers.MaxParallel = maxWorkers
	}

	return cfg
}

func cmdInit(projectDir, configPath string, logger *log.Logger) {
	hiveDir := filepath.Join(projectDir, ".hive")
	if err := os.MkdirAll(hiveDir, 0755); err != nil {
		logger.Fatalf("Create .hive: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.ProjectDir = projectDir
	if err := cfg.Save(configPath); err != nil {
		logger.Fatalf("Save config: %v", err)
	}

	logger.Printf("✅ Initialized hive at %s", hiveDir)
	logger.Printf("✅ Config saved to %s", configPath)
}

func cmdConfig(configPath string, logger *log.Logger) {
	cfg, err := config.Load(configPath)
	if err != nil {
		logger.Fatalf("Load config: %v", err)
	}

	fmt.Printf("Configuration (%s):\n", configPath)
	fmt.Printf("  Project Dir:     %s\n", cfg.ProjectDir)
	fmt.Printf("  Hive Dir:        %s\n", cfg.HiveDir)
	fmt.Printf("  Queen Model:     %s (%s)\n", cfg.Queen.Model, cfg.Queen.Provider)
	fmt.Printf("  Max Workers:     %d\n", cfg.Workers.MaxParallel)
	fmt.Printf("  Default Adapter: %s\n", cfg.Workers.DefaultAdapter)
	fmt.Printf("  Max Retries:     %d\n", cfg.Workers.MaxRetries)
	fmt.Printf("  Worker Timeout:  %v\n", cfg.Workers.DefaultTimeout)
	fmt.Printf("  Available Adapters:\n")
	for name, a := range cfg.Adapters {
		fmt.Printf("    - %s: %s %v\n", name, a.Command, a.Args)
	}
}

func cmdStatus(projectDir string, logger *log.Logger) {
	hiveDir := filepath.Join(projectDir, ".hive")

	if _, err := os.Stat(hiveDir); os.IsNotExist(err) {
		logger.Println("No active hive session. Run 'queen-bee init' first.")
		return
	}

	dbPath := filepath.Join(hiveDir, "hive.db")
	logPath := filepath.Join(hiveDir, "log.jsonl")

	// Try SQLite DB first; fall back to JSONL if DB doesn't exist
	if _, err := os.Stat(dbPath); err == nil {
		cmdStatusDB(hiveDir, logger)
		return
	}

	// Fallback: parse JSONL (legacy sessions)
	if _, err := os.Stat(logPath); err == nil {
		cmdStatusJSONL(logPath, logger)
		return
	}

	fmt.Println("Hive initialized but no sessions run yet.")
}

// cmdStatusDB reads status from the SQLite database.
func cmdStatusDB(hiveDir string, logger *log.Logger) {
	db, err := state.OpenDB(hiveDir)
	if err != nil {
		logger.Fatalf("Open DB: %v", err)
	}
	defer db.Close()

	session, err := db.LatestSession()
	if err != nil {
		fmt.Println("Hive initialized but no sessions run yet.")
		return
	}

	counts, err := db.CountTasksByStatus(session.ID)
	if err != nil {
		logger.Fatalf("Count tasks: %v", err)
	}

	tasks, err := db.GetTasks(session.ID)
	if err != nil {
		logger.Fatalf("Get tasks: %v", err)
	}

	eventCount, err := db.EventCount(session.ID)
	if err != nil {
		logger.Fatalf("Event count: %v", err)
	}

	total := 0
	for _, c := range counts {
		total += c
	}

	fmt.Println("")
	fmt.Println("══════════════════════════════════════════════════")
	fmt.Println("  🐝 Queen Bee — Session Status")
	fmt.Println("══════════════════════════════════════════════════")
	fmt.Printf("  Session:    %s\n", session.ID)
	fmt.Printf("  Objective:  %s\n", session.Objective)
	fmt.Printf("  Status:     %s\n", session.Status)
	fmt.Printf("  Started:    %s\n", session.CreatedAt)
	fmt.Printf("  Updated:    %s\n", session.UpdatedAt)
	fmt.Printf("  Events:     %d\n", eventCount)
	fmt.Println("")
	fmt.Printf("  Tasks: %d total\n", total)
	for _, st := range []string{"complete", "running", "pending", "failed", "cancelled", "retrying"} {
		if c, ok := counts[st]; ok && c > 0 {
			icon := statusIcon(st)
			fmt.Printf("    %s %-10s %d\n", icon, st, c)
		}
	}
	fmt.Println("")

	if len(tasks) > 0 {
		fmt.Println("  Task Details:")
		for _, t := range tasks {
			icon := statusIcon(t.Status)
			worker := ""
			if t.WorkerID != nil && *t.WorkerID != "" {
				worker = fmt.Sprintf(" (worker: %s)", *t.WorkerID)
			}
			fmt.Printf("    %s [%s] %s%s\n", icon, t.Type, t.Title, worker)
		}
		fmt.Println("")
	}
}

// cmdStatusJSONL is the legacy fallback that parses log.jsonl.
func cmdStatusJSONL(logPath string, logger *log.Logger) {
	data, err := os.ReadFile(logPath)
	if err != nil {
		logger.Fatalf("Read log: %v", err)
	}

	// Parse events from JSONL
	var objective string
	taskNames := make(map[string]string)  // id -> title
	taskStatus := make(map[string]string) // id -> status
	taskTypes := make(map[string]string)  // id -> type
	var lastPhase string
	var startTime string
	var eventCount int

	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		eventCount++

		var ev struct {
			Type string          `json:"type"`
			Ts   string          `json:"ts"`
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}

		switch ev.Type {
		case "queen.start":
			var d struct {
				Objective string `json:"objective"`
			}
			json.Unmarshal(ev.Data, &d)
			objective = d.Objective
			startTime = ev.Ts

		case "task.created":
			var d struct {
				Payload struct {
					ID    string `json:"id"`
					Title string `json:"title"`
					Type  string `json:"type"`
				} `json:"payload"`
			}
			json.Unmarshal(ev.Data, &d)
			if d.Payload.ID != "" {
				taskNames[d.Payload.ID] = d.Payload.Title
				taskTypes[d.Payload.ID] = d.Payload.Type
				if _, ok := taskStatus[d.Payload.ID]; !ok {
					taskStatus[d.Payload.ID] = "pending"
				}
			}

		case "task.status_changed":
			var d struct {
				TaskID  string            `json:"task_id"`
				Payload map[string]string `json:"payload"`
			}
			json.Unmarshal(ev.Data, &d)
			if d.TaskID != "" {
				if newSt, ok := d.Payload["new"]; ok {
					taskStatus[d.TaskID] = newSt
				}
			}

		case "queen.plan":
			lastPhase = "plan"
		case "queen.delegate":
			lastPhase = "delegate"
		case "queen.done":
			lastPhase = "done"
		case "queen.failed":
			lastPhase = "failed"
		}
	}

	if objective == "" {
		fmt.Println("Hive initialized but no sessions run yet.")
		return
	}

	// Count statuses
	counts := make(map[string]int)
	for _, s := range taskStatus {
		counts[s]++
	}
	total := len(taskStatus)

	fmt.Println("")
	fmt.Println("══════════════════════════════════════════════════")
	fmt.Println("  🐝 Queen Bee — Session Status (legacy)")
	fmt.Println("══════════════════════════════════════════════════")
	fmt.Printf("  Objective:  %s\n", objective)
	fmt.Printf("  Started:    %s\n", startTime)
	fmt.Printf("  Events:     %d\n", eventCount)
	if lastPhase != "" {
		fmt.Printf("  Last Phase: %s\n", lastPhase)
	}
	fmt.Println("")
	fmt.Printf("  Tasks: %d total\n", total)
	for _, st := range []string{"complete", "running", "pending", "failed", "cancelled", "retrying"} {
		if c, ok := counts[st]; ok && c > 0 {
			icon := statusIcon(st)
			fmt.Printf("    %s %-10s %d\n", icon, st, c)
		}
	}
	fmt.Println("")

	// List tasks
	fmt.Println("  Task Details:")
	for id, title := range taskNames {
		st := taskStatus[id]
		icon := statusIcon(st)
		tp := taskTypes[id]
		fmt.Printf("    %s [%s] %s\n", icon, tp, title)
	}
	fmt.Println("")
}

func statusIcon(st string) string {
	switch st {
	case "complete":
		return "✅"
	case "running":
		return "🔄"
	case "pending":
		return "⏳"
	case "failed":
		return "❌"
	case "cancelled":
		return "⛔"
	case "retrying":
		return "🔁"
	default:
		return "❓"
	}
}

func cmdRun(objective, configPath, projectDir, defaultAdapter string, maxWorkers int, tasksFile string, verbose bool, logger *log.Logger) {
	cfg := loadConfig(configPath, projectDir, defaultAdapter, maxWorkers)

	if verbose {
		logger.SetFlags(log.LstdFlags | log.Lmicroseconds)
	}

	fmt.Println("")
	fmt.Println("══════════════════════════════════════════════════")
	fmt.Println("  🐝 Queen Bee - Agent Orchestration System")
	fmt.Println("══════════════════════════════════════════════════")
	fmt.Printf("  Objective: %s\n", objective)
	fmt.Printf("  Adapter:   %s\n", cfg.Workers.DefaultAdapter)
	fmt.Printf("  Workers:   %d max parallel\n", cfg.Workers.MaxParallel)
	fmt.Println("")

	q, err := queen.New(cfg, logger)
	if err != nil {
		logger.Fatalf("Init queen: %v", err)
	}
	defer q.Close()

	// Load pre-defined tasks if provided
	if tasksFile != "" {
		tasks, err := loadTasksFile(tasksFile, cfg)
		if err != nil {
			logger.Fatalf("Load tasks file: %v", err)
		}
		q.SetTasks(tasks)
		logger.Printf("Loaded %d tasks from %s", len(tasks), tasksFile)
	}

	// Handle graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigs
		logger.Println("\n⛔ Received shutdown signal, gracefully stopping...")
		cancel()
	}()

	if err := q.Run(ctx, objective); err != nil {
		logger.Fatalf("❌ Queen failed: %v", err)
	}

	// The Queen's printReport() handles the full output display.
	// Just print a closing line.
	fmt.Println("")
	fmt.Println("══════════════════════════════════════════════════")
	fmt.Println("  ✅ Mission Complete")
	fmt.Println("══════════════════════════════════════════════════")
}

func cmdResume(configPath, projectDir, defaultAdapter string, maxWorkers int, verbose bool, logger *log.Logger) {
	hiveDir := filepath.Join(projectDir, ".hive")
	dbPath := filepath.Join(hiveDir, "hive.db")

	// Check if hive and database exist
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		logger.Fatal("No session to resume. Run 'queen-bee run <objective>' first.")
	}

	// Open the database and find the latest resumable session
	db, err := state.OpenDB(hiveDir)
	if err != nil {
		logger.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	session, err := db.FindResumableSession()
	if err != nil {
		logger.Fatal("No interrupted session found to resume. Run 'queen-bee run <objective>' to start a new session.")
	}

	logger.Printf("🔄 Resuming session: %s", session.ID)
	logger.Printf("   Objective: %s", session.Objective)
	logger.Printf("   Status: %s", session.Status)

	// Load configuration
	cfg := loadConfig(configPath, projectDir, defaultAdapter, maxWorkers)

	if verbose {
		logger.SetFlags(log.LstdFlags | log.Lmicroseconds)
	}

	fmt.Println("")
	fmt.Println("══════════════════════════════════════════════════")
	fmt.Println("  🐝 Queen Bee - Agent Orchestration System")
	fmt.Println("  🔄 Resuming Interrupted Session")
	fmt.Println("══════════════════════════════════════════════════")
	fmt.Printf("  Session ID: %s\n", session.ID)
	fmt.Printf("  Objective: %s\n", session.Objective)
	fmt.Printf("  Adapter:   %s\n", cfg.Workers.DefaultAdapter)
	fmt.Printf("  Workers:   %d max parallel\n", cfg.Workers.MaxParallel)
	fmt.Println("")

	// Create queen instance
	q, err := queen.New(cfg, logger)
	if err != nil {
		logger.Fatalf("Init queen: %v", err)
	}
	defer q.Close()

	// Resume the session - this loads all tasks from the database
	objective, err := q.ResumeSession(session.ID)
	if err != nil {
		logger.Fatalf("Failed to resume session: %v", err)
	}

	// Handle graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigs
		logger.Println("\n⛔ Received shutdown signal, gracefully stopping...")
		cancel()
	}()

	// Run with the resumed session (objective is already set by ResumeSession)
	if err := q.Run(ctx, objective); err != nil {
		logger.Fatalf("❌ Queen failed: %v", err)
	}

	fmt.Println("")
	fmt.Println("══════════════════════════════════════════════════")
	fmt.Println("  ✅ Mission Complete")
	fmt.Println("══════════════════════════════════════════════════")
}

func loadTasksFile(path string, cfg *config.Config) ([]*task.Task, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var rawTasks []struct {
		ID          string   `json:"id"`
		Type        string   `json:"type"`
		Title       string   `json:"title"`
		Description string   `json:"description"`
		Priority    int      `json:"priority"`
		DependsOn   []string `json:"depends_on"`
		MaxRetries  int      `json:"max_retries"`
	}

	if err := json.Unmarshal(data, &rawTasks); err != nil {
		return nil, fmt.Errorf("parse tasks JSON: %w", err)
	}

	tasks := make([]*task.Task, 0, len(rawTasks))
	for _, rt := range rawTasks {
		t := &task.Task{
			ID:          rt.ID,
			Type:        task.Type(rt.Type),
			Status:      task.StatusPending,
			Priority:    task.Priority(rt.Priority),
			Title:       rt.Title,
			Description: rt.Description,
			DependsOn:   rt.DependsOn,
			MaxRetries:  rt.MaxRetries,
			CreatedAt:   time.Now(),
			Timeout:     cfg.Workers.DefaultTimeout,
		}
		if t.MaxRetries == 0 {
			t.MaxRetries = cfg.Workers.MaxRetries
		}
		if t.ID == "" {
			t.ID = fmt.Sprintf("task-%d", time.Now().UnixNano())
		}
		tasks = append(tasks, t)
	}

	return tasks, nil
}
