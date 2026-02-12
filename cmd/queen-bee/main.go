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
	statePath := filepath.Join(projectDir, ".hive", "state.json")
	data, err := os.ReadFile(statePath)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Println("No active hive session. Run 'queen-bee init' first.")
			return
		}
		logger.Fatalf("Read state: %v", err)
	}
	fmt.Println(string(data))
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

	// Display results
	results := q.Results()
	fmt.Println("")
	fmt.Println("══════════════════════════════════════════════════")
	fmt.Println("  ✅ Mission Complete")
	fmt.Println("══════════════════════════════════════════════════")
	if len(results) > 0 && verbose {
		fmt.Println("\n  Results:")
		for title, r := range results {
			status := "✅"
			if !r.Success {
				status = "❌"
			}
			fmt.Printf("\n  %s %s\n", status, title)
			if r.Output != "" {
				// Indent output
				for _, line := range strings.Split(strings.TrimSpace(r.Output), "\n") {
					fmt.Printf("    %s\n", line)
				}
			}
		}
	}
	fmt.Println("")
}

func cmdResume(configPath, projectDir, defaultAdapter string, maxWorkers int, verbose bool, logger *log.Logger) {
	// Load saved objective from state
	statePath := filepath.Join(projectDir, ".hive", "state.json")
	if _, err := os.Stat(statePath); os.IsNotExist(err) {
		logger.Fatal("No session to resume. Run 'queen-bee run <objective>' first.")
	}

	logger.Println("🔄 Resuming previous session...")
	// For now, read objective from state and re-run
	// The Queen's plan phase will pick up existing tasks from the store
	cmdRun("[resumed session]", configPath, projectDir, defaultAdapter, maxWorkers, "", verbose, logger)
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
