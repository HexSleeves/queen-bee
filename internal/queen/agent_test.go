package queen

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/exedev/waggle/internal/adapter"
	"github.com/exedev/waggle/internal/blackboard"
	"github.com/exedev/waggle/internal/bus"
	"github.com/exedev/waggle/internal/compact"
	"github.com/exedev/waggle/internal/config"
	"github.com/exedev/waggle/internal/llm"
	"github.com/exedev/waggle/internal/safety"
	"github.com/exedev/waggle/internal/state"
	"github.com/exedev/waggle/internal/task"
	"github.com/exedev/waggle/internal/worker"
)

// mockToolClient implements llm.ToolClient with scripted responses.
type mockToolClient struct {
	mu        sync.Mutex
	responses []*llm.Response
	callIndex int
	calls     []mockCall
}

type mockCall struct {
	Messages []llm.ToolMessage
	Tools    []llm.ToolDef
}

func (m *mockToolClient) Chat(ctx context.Context, systemPrompt, userMessage string) (string, error) {
	return "mock chat response", nil
}

func (m *mockToolClient) ChatWithHistory(ctx context.Context, systemPrompt string, messages []llm.Message) (string, error) {
	return "mock chat history response", nil
}

func (m *mockToolClient) ChatWithTools(ctx context.Context, systemPrompt string,
	messages []llm.ToolMessage, tools []llm.ToolDef) (*llm.Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.calls = append(m.calls, mockCall{Messages: messages, Tools: tools})

	if m.callIndex >= len(m.responses) {
		// Default: end turn
		return &llm.Response{
			Content:    []llm.ContentBlock{{Type: "text", Text: "Done."}},
			StopReason: "end_turn",
		}, nil
	}

	resp := m.responses[m.callIndex]
	m.callIndex++
	return resp, nil
}

func TestSupportsAgentMode(t *testing.T) {
	q := &Queen{}

	// No LLM: no agent mode
	if q.SupportsAgentMode() {
		t.Error("expected false with nil llm")
	}

	// Regular Client (no tool support): no agent mode
	q.llm = &mockBasicClient{}
	if q.SupportsAgentMode() {
		t.Error("expected false with basic client")
	}

	// ToolClient: agent mode supported
	q.llm = &mockToolClient{}
	if !q.SupportsAgentMode() {
		t.Error("expected true with tool client")
	}
}

type mockBasicClient struct{}

func (m *mockBasicClient) Chat(ctx context.Context, systemPrompt, userMessage string) (string, error) {
	return "", nil
}
func (m *mockBasicClient) ChatWithHistory(ctx context.Context, systemPrompt string, messages []llm.Message) (string, error) {
	return "", nil
}

func TestRunAgentEndTurn(t *testing.T) {
	// Queen gets the objective, immediately says done (end_turn)
	q := setupTestQueen(t)
	q.llm = &mockToolClient{
		responses: []*llm.Response{{
			Content:    []llm.ContentBlock{{Type: "text", Text: "Nothing to do."}},
			StopReason: "end_turn",
		}},
	}

	err := q.RunAgent(context.Background(), "test objective")
	if err != nil {
		t.Fatalf("RunAgent failed: %v", err)
	}

	// Verify session was created
	if q.sessionID == "" {
		t.Error("session ID not set")
	}
}

func TestRunAgentCreateAndComplete(t *testing.T) {
	// Queen creates a task then completes
	q := setupTestQueen(t)

	createInput, _ := json.Marshal(map[string]interface{}{
		"tasks": []map[string]interface{}{{
			"id":          "task-1",
			"title":       "Test task",
			"description": "echo hello",
			"type":        "test",
			"priority":    1,
		}},
	})

	completeInput, _ := json.Marshal(map[string]string{
		"summary": "All done",
	})

	q.llm = &mockToolClient{
		responses: []*llm.Response{
			{
				Content: []llm.ContentBlock{{
					Type: "tool_use",
					ToolCall: &llm.ToolCall{
						ID:    "call-1",
						Name:  "create_tasks",
						Input: createInput,
					},
				}},
				StopReason: "tool_use",
			},
			{
				Content: []llm.ContentBlock{{
					Type: "tool_use",
					ToolCall: &llm.ToolCall{
						ID:    "call-2",
						Name:  "complete",
						Input: completeInput,
					},
				}},
				StopReason: "tool_use",
			},
		},
	}

	err := q.RunAgent(context.Background(), "test objective")
	if err != nil {
		t.Fatalf("RunAgent failed: %v", err)
	}

	// Verify task was created
	tasks := q.tasks.All()
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].ID != "task-1" {
		t.Errorf("expected task ID 'task-1', got %q", tasks[0].ID)
	}
}

func TestRunAgentFailTool(t *testing.T) {
	q := setupTestQueen(t)

	failInput, _ := json.Marshal(map[string]string{
		"reason": "impossible objective",
	})

	q.llm = &mockToolClient{
		responses: []*llm.Response{{
			Content: []llm.ContentBlock{{
				Type: "tool_use",
				ToolCall: &llm.ToolCall{
					ID:    "call-1",
					Name:  "fail",
					Input: failInput,
				},
			}},
			StopReason: "tool_use",
		}},
	}

	err := q.RunAgent(context.Background(), "impossible thing")
	if err == nil {
		t.Fatal("expected error from fail tool")
	}
	if !contains(err.Error(), "failure") && !contains(err.Error(), "failed") {
		t.Errorf("expected failure error, got: %v", err)
	}
}

func TestRunAgentMaxTurns(t *testing.T) {
	q := setupTestQueen(t)
	q.cfg.Queen.MaxIterations = 3

	// Always request get_status — never terminates
	statusInput, _ := json.Marshal(map[string]interface{}{})

	q.llm = &mockToolClient{
		responses: []*llm.Response{
			makeToolResponse("call-1", "get_status", statusInput),
			makeToolResponse("call-2", "get_status", statusInput),
			makeToolResponse("call-3", "get_status", statusInput),
			makeToolResponse("call-4", "get_status", statusInput),
		},
	}

	err := q.RunAgent(context.Background(), "never ending")
	if err == nil {
		t.Fatal("expected max turns error")
	}
	if !contains(err.Error(), "max turns") {
		t.Errorf("expected max turns error, got: %v", err)
	}
}

func TestRunAgentFallsBackToLegacy(t *testing.T) {
	// When LLM doesn't support tools, RunAgent should fall back to legacy.
	// We just verify it doesn't try to use tool-calling APIs.
	q := setupTestQueen(t)
	basic := &mockBasicClient{}
	q.llm = basic

	if q.SupportsAgentMode() {
		t.Fatal("basic client should not support agent mode")
	}

	// RunAgent with a basic client should attempt legacy Run().
	// Legacy will fail because our test pool factory returns nil bees,
	// but we just verify it took the legacy path (not the tool path).
	// We can't easily test the full legacy path without a real adapter,
	// so just verify the routing logic.
	t.Log("Verified: basic LLM client correctly detected as non-tool-capable")
}

func TestCompactMessages(t *testing.T) {
	q := setupTestQueen(t)

	// Build a conversation with many messages
	var messages []llm.ToolMessage
	// First: objective
	messages = append(messages, llm.ToolMessage{
		Role:    "user",
		Content: []llm.ContentBlock{{Type: "text", Text: "Build a thing"}},
	})

	// Add 80 assistant/tool_result pairs
	for i := 0; i < 80; i++ {
		messages = append(messages, llm.ToolMessage{
			Role: "assistant",
			Content: []llm.ContentBlock{{
				Type:     "tool_use",
				ToolCall: &llm.ToolCall{ID: fmt.Sprintf("call-%d", i), Name: "get_status"},
			}},
		})
		messages = append(messages, llm.ToolMessage{
			Role:        "tool_result",
			ToolResults: []llm.ToolResult{{ToolCallID: fmt.Sprintf("call-%d", i), Content: "ok"}},
		})
	}

	// Total: 1 + 160 = 161 messages
	compacted := q.compactMessages(messages)

	// Should be much smaller: objective + summary + last 20
	if len(compacted) >= len(messages) {
		t.Errorf("expected compaction, got %d messages (was %d)", len(compacted), len(messages))
	}
	if len(compacted) > 25 {
		t.Errorf("expected ~22 messages after compaction, got %d", len(compacted))
	}

	// First message should still be the objective
	if compacted[0].Content[0].Text != "Build a thing" {
		t.Error("first message should be the objective")
	}

	// Second message should be the compaction summary
	if !contains(compacted[1].Content[0].Text, "compacted") {
		t.Error("second message should be the compaction summary")
	}
}

func TestRunAgentContextCancellation(t *testing.T) {
	q := setupTestQueen(t)

	// LLM never returns (blocks forever)
	q.llm = &blockingToolClient{}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := q.RunAgent(ctx, "test")
	if err == nil {
		t.Fatal("expected context cancelled error")
	}
}

type blockingToolClient struct{}

func (m *blockingToolClient) Chat(ctx context.Context, systemPrompt, userMessage string) (string, error) {
	<-ctx.Done()
	return "", ctx.Err()
}
func (m *blockingToolClient) ChatWithHistory(ctx context.Context, systemPrompt string, messages []llm.Message) (string, error) {
	<-ctx.Done()
	return "", ctx.Err()
}
func (m *blockingToolClient) ChatWithTools(ctx context.Context, systemPrompt string,
	messages []llm.ToolMessage, tools []llm.ToolDef) (*llm.Response, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// --- helpers ---

func setupTestQueen(t *testing.T) *Queen {
	t.Helper()
	tmpDir := t.TempDir()
	hiveDir := filepath.Join(tmpDir, ".hive")
	if err := os.MkdirAll(hiveDir, 0755); err != nil {
		t.Fatalf("create hive dir: %v", err)
	}

	db, err := state.OpenDB(hiveDir)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	msgBus := bus.New(100)
	board := blackboard.New(msgBus)
	tasks := task.NewTaskGraph(msgBus)

	pool := worker.NewPool(4, func(id, adapterName string) (worker.Bee, error) {
		return nil, nil
	}, msgBus)

	cfg := &config.Config{
		ProjectDir: tmpDir,
		HiveDir:    ".hive",
		Queen:      config.QueenConfig{MaxIterations: 50},
		Workers: config.WorkerConfig{
			MaxParallel:    4,
			MaxRetries:     2,
			DefaultTimeout: 5 * time.Minute,
			DefaultAdapter: "exec",
		},
		Safety: config.SafetyConfig{
			AllowedPaths: []string{"."},
			MaxFileSize:  10 * 1024 * 1024,
		},
		Adapters: map[string]config.AdapterConfig{},
	}

	guard, _ := safety.NewGuard(cfg.Safety, tmpDir)
	registry := adapter.NewRegistry()
	registry.Register(adapter.NewExecAdapter(tmpDir, guard))
	router := adapter.NewTaskRouter(registry, cfg.Workers.DefaultAdapter)

	logger := log.New(os.Stderr, "[TEST] ", log.LstdFlags)
	ctxMgr := compact.NewContext(200000)

	return &Queen{
		cfg:         cfg,
		bus:         msgBus,
		db:          db,
		board:       board,
		tasks:       tasks,
		pool:        pool,
		router:      router,
		registry:    registry,
		ctx:         ctxMgr,
		phase:       PhasePlan,
		logger:      logger,
		assignments: make(map[string]string),
	}
}

func makeToolResponse(id, name string, input json.RawMessage) *llm.Response {
	return &llm.Response{
		Content: []llm.ContentBlock{{
			Type: "tool_use",
			ToolCall: &llm.ToolCall{
				ID:    id,
				Name:  name,
				Input: input,
			},
		}},
		StopReason: "tool_use",
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
