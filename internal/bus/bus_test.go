package bus

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestMsgTypeConstants(t *testing.T) {
	tests := []struct {
		name     string
		msgType  MsgType
		expected string
	}{
		{"TaskCreated", MsgTaskCreated, "task.created"},
		{"TaskStatusChanged", MsgTaskStatusChanged, "task.status_changed"},
		{"TaskAssigned", MsgTaskAssigned, "task.assigned"},
		{"WorkerSpawned", MsgWorkerSpawned, "worker.spawned"},
		{"WorkerCompleted", MsgWorkerCompleted, "worker.completed"},
		{"WorkerFailed", MsgWorkerFailed, "worker.failed"},
		{"WorkerOutput", MsgWorkerOutput, "worker.output"},
		{"BlackboardUpdate", MsgBlackboardUpdate, "blackboard.update"},
		{"QueenDecision", MsgQueenDecision, "queen.decision"},
		{"QueenPlan", MsgQueenPlan, "queen.plan"},
		{"SystemError", MsgSystemError, "system.error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.msgType) != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, tt.msgType)
			}
		})
	}
}

func TestNew(t *testing.T) {
	b := New(100)
	if b == nil {
		t.Fatal("New returned nil")
	}
	if b.handlers == nil {
		t.Error("handlers map not initialized")
	}
	if b.maxHist != 100 {
		t.Errorf("Expected maxHist 100, got %d", b.maxHist)
	}

	// Test default max history
	b2 := New(0)
	if b2.maxHist != 10000 {
		t.Errorf("Expected default maxHist 10000, got %d", b2.maxHist)
	}

	b3 := New(-1)
	if b3.maxHist != 10000 {
		t.Errorf("Expected default maxHist 10000 for negative, got %d", b3.maxHist)
	}
}

func TestSubscribe(t *testing.T) {
	b := New(100)

	var received atomic.Bool
	handler := func(msg Message) {
		received.Store(true)
	}

	sub := b.Subscribe(MsgTaskCreated, handler)
	if sub == nil {
		t.Fatal("Subscribe returned nil")
	}

	// Check handler was registered
	b.mu.RLock()
	entries := b.handlers[MsgTaskCreated]
	b.mu.RUnlock()

	if len(entries) != 1 {
		t.Errorf("Expected 1 handler, got %d", len(entries))
	}
}

func TestUnsubscribe(t *testing.T) {
	b := New(100)

	var count atomic.Int32
	sub := b.Subscribe(MsgTaskCreated, func(msg Message) {
		count.Add(1)
	})

	b.Publish(Message{Type: MsgTaskCreated})
	time.Sleep(10 * time.Millisecond)
	if count.Load() != 1 {
		t.Fatalf("Expected 1 call before unsubscribe, got %d", count.Load())
	}

	sub.Unsubscribe()

	b.Publish(Message{Type: MsgTaskCreated})
	time.Sleep(10 * time.Millisecond)
	if count.Load() != 1 {
		t.Errorf("Expected no new calls after unsubscribe, got %d", count.Load())
	}
}

func TestUnsubscribeAll(t *testing.T) {
	b := New(100)

	var count atomic.Int32
	sub := b.SubscribeAll(func(msg Message) {
		count.Add(1)
	})

	b.Publish(Message{Type: MsgTaskCreated})
	time.Sleep(10 * time.Millisecond)
	if count.Load() != 1 {
		t.Fatalf("Expected 1 call before unsubscribe, got %d", count.Load())
	}

	sub.Unsubscribe()

	b.Publish(Message{Type: MsgWorkerSpawned})
	time.Sleep(10 * time.Millisecond)
	if count.Load() != 1 {
		t.Errorf("Expected no new calls after unsubscribe, got %d", count.Load())
	}
}

func TestUnsubscribeIdempotent(t *testing.T) {
	b := New(100)
	sub := b.Subscribe(MsgTaskCreated, func(msg Message) {})
	sub.Unsubscribe()
	sub.Unsubscribe() // should not panic
}

func TestSubscribeMultiple(t *testing.T) {
	b := New(100)

	var count atomic.Int32
	handler1 := func(msg Message) { count.Add(1) }
	handler2 := func(msg Message) { count.Add(1) }

	b.Subscribe(MsgTaskCreated, handler1)
	b.Subscribe(MsgTaskCreated, handler2)

	b.Publish(Message{Type: MsgTaskCreated})

	time.Sleep(10 * time.Millisecond)

	if count.Load() != 2 {
		t.Errorf("Expected both handlers called (count=2), got %d", count.Load())
	}
}

func TestSubscribeAll(t *testing.T) {
	b := New(100)

	var received atomic.Bool
	handler := func(msg Message) {
		received.Store(true)
	}

	b.SubscribeAll(handler)

	// Publish different message types
	b.Publish(Message{Type: MsgTaskCreated})
	time.Sleep(5 * time.Millisecond)

	if !received.Load() {
		t.Error("SubscribeAll handler should receive all messages")
	}

	received.Store(false)
	b.Publish(Message{Type: MsgWorkerFailed})
	time.Sleep(5 * time.Millisecond)

	if !received.Load() {
		t.Error("SubscribeAll handler should receive all message types")
	}
}

func TestPublish(t *testing.T) {
	b := New(100)

	var receivedMsg *Message
	var mu sync.Mutex

	b.Subscribe(MsgTaskCreated, func(msg Message) {
		mu.Lock()
		defer mu.Unlock()
		receivedMsg = &msg
	})

	msg := Message{
		Type:     MsgTaskCreated,
		TaskID:   "task-1",
		WorkerID: "worker-1",
		Payload:  map[string]string{"key": "value"},
		Time:     time.Now(),
	}

	b.Publish(msg)

	time.Sleep(10 * time.Millisecond)

	mu.Lock()
	if receivedMsg == nil {
		t.Fatal("Expected message to be received")
	}
	if receivedMsg.Type != MsgTaskCreated {
		t.Errorf("Expected type %s, got %s", MsgTaskCreated, receivedMsg.Type)
	}
	if receivedMsg.TaskID != "task-1" {
		t.Errorf("Expected TaskID 'task-1', got %s", receivedMsg.TaskID)
	}
	if receivedMsg.WorkerID != "worker-1" {
		t.Errorf("Expected WorkerID 'worker-1', got %s", receivedMsg.WorkerID)
	}
	mu.Unlock()
}

func TestPublishNoSubscribers(t *testing.T) {
	b := New(100)

	// Should not panic
	b.Publish(Message{Type: MsgTaskCreated})
}

func TestPublishHistory(t *testing.T) {
	b := New(5) // Max 5 messages

	// Publish 10 messages
	for i := 0; i < 10; i++ {
		b.Publish(Message{Type: MsgTaskCreated, TaskID: string(rune('a' + i))})
	}

	history := b.History(10)
	if len(history) != 5 {
		t.Errorf("Expected history capped at 5, got %d", len(history))
	}

	// Most recent messages should be kept
	// Last 5 are 'f', 'g', 'h', 'i', 'j'
	if history[0].TaskID != "f" {
		t.Errorf("Expected first history entry 'f', got %s", history[0].TaskID)
	}
	if history[4].TaskID != "j" {
		t.Errorf("Expected last history entry 'j', got %s", history[4].TaskID)
	}
}

func TestHistory(t *testing.T) {
	b := New(10)

	// Empty history
	history := b.History(5)
	if len(history) != 0 {
		t.Errorf("Expected empty history, got %d", len(history))
	}

	// Add messages
	for i := 0; i < 5; i++ {
		b.Publish(Message{Type: MsgTaskCreated, TaskID: string(rune('a' + i))})
	}

	// Get all
	history = b.History(10)
	if len(history) != 5 {
		t.Errorf("Expected 5 messages, got %d", len(history))
	}

	// Get subset
	history = b.History(3)
	if len(history) != 3 {
		t.Errorf("Expected 3 messages, got %d", len(history))
	}
	// Should be most recent 3: 'c', 'd', 'e'
	if history[0].TaskID != "c" {
		t.Errorf("Expected first 'c', got %s", history[0].TaskID)
	}

	// Get zero
	history = b.History(0)
	if len(history) != 5 {
		t.Errorf("Expected all messages when n=0, got %d", len(history))
	}

	// Get negative
	history = b.History(-1)
	if len(history) != 5 {
		t.Errorf("Expected all messages when n negative, got %d", len(history))
	}
}

func TestPublishOrder(t *testing.T) {
	b := New(100)

	var order []int
	var mu sync.Mutex

	b.Subscribe(MsgTaskCreated, func(msg Message) {
		mu.Lock()
		defer mu.Unlock()
		order = append(order, 1)
	})

	b.Subscribe(MsgTaskCreated, func(msg Message) {
		mu.Lock()
		defer mu.Unlock()
		order = append(order, 2)
	})

	b.SubscribeAll(func(msg Message) {
		mu.Lock()
		defer mu.Unlock()
		order = append(order, 3)
	})

	b.Publish(Message{Type: MsgTaskCreated})
	time.Sleep(10 * time.Millisecond)

	mu.Lock()
	if len(order) != 3 {
		t.Errorf("Expected 3 handlers called, got %d", len(order))
	}
	// Specific handlers should be called before wildcard
	if order[0] != 1 || order[1] != 2 || order[2] != 3 {
		t.Errorf("Expected order [1,2,3], got %v", order)
	}
	mu.Unlock()
}

func TestMessageStruct(t *testing.T) {
	now := time.Now()
	msg := Message{
		Type:     MsgTaskCreated,
		TaskID:   "task-1",
		WorkerID: "worker-1",
		Payload:  map[string]string{"key": "value"},
		Time:     now,
	}

	if msg.Type != MsgTaskCreated {
		t.Error("Type mismatch")
	}
	if msg.TaskID != "task-1" {
		t.Error("TaskID mismatch")
	}
	if msg.WorkerID != "worker-1" {
		t.Error("WorkerID mismatch")
	}
	if !msg.Time.Equal(now) {
		t.Error("Time mismatch")
	}
}

func TestBusConcurrency(t *testing.T) {
	b := New(1000)

	var received atomic.Int32

	// Multiple subscribers
	for i := 0; i < 10; i++ {
		b.Subscribe(MsgTaskCreated, func(msg Message) {
			received.Add(1)
		})
	}

	var wg sync.WaitGroup

	// Concurrent publishes
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			b.Publish(Message{Type: MsgTaskCreated, TaskID: string(rune('a' + id%26))})
		}(i)
	}

	wg.Wait()
	time.Sleep(50 * time.Millisecond)

	// Each message should trigger 10 handlers
	expected := int32(100 * 10)
	if received.Load() != expected {
		t.Errorf("Expected %d handler calls, got %d", expected, received.Load())
	}
}

func TestBusConcurrencySubscribeDuringPublish(t *testing.T) {
	b := New(100)

	var wg sync.WaitGroup

	// Subscribe and publish concurrently
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			b.Subscribe(MsgTaskCreated, func(msg Message) {})
		}()
		go func() {
			defer wg.Done()
			b.Publish(Message{Type: MsgTaskCreated})
		}()
	}

	wg.Wait()
}

func TestBusConcurrencyHistoryAccess(t *testing.T) {
	b := New(100)

	var wg sync.WaitGroup

	// Concurrent publishes and history reads
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func(id int) {
			defer wg.Done()
			b.Publish(Message{Type: MsgTaskCreated, TaskID: string(rune('a' + id%26))})
		}(i)
		go func() {
			defer wg.Done()
			b.History(10)
		}()
	}

	wg.Wait()
}

func TestHandlerType(t *testing.T) {
	b := New(10)

	var called bool
	handler := Handler(func(msg Message) {
		called = true
	})

	b.Subscribe(MsgTaskCreated, handler)
	b.Publish(Message{Type: MsgTaskCreated})

	time.Sleep(10 * time.Millisecond)

	if !called {
		t.Error("Handler was not called")
	}
}

// TestPanicRecovery tests that a panicking handler does not crash Publish()
func TestPanicRecovery(t *testing.T) {
	tests := []struct {
		name       string
		panicType  string
		panicValue interface{}
	}{
		{
			name:       "panic with string",
			panicType:  "string",
			panicValue: "handler panic",
		},
		{
			name:       "panic with error",
			panicType:  "error",
			panicValue: errors.New("handler error"),
		},
		{
			name:       "panic with int",
			panicType:  "int",
			panicValue: 42,
		},
		{
			name:       "panic with nil",
			panicType:  "nil",
			panicValue: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := New(10)

			var recovered bool
			b.Subscribe(MsgTaskCreated, func(msg Message) {
				panic(tt.panicValue)
			})

			// Add a recovery handler to verify Publish doesn't panic
			defer func() {
				if r := recover(); r != nil {
					recovered = true
				}
			}()

			// Publish should not panic
			b.Publish(Message{Type: MsgTaskCreated})

			// Give time for handler to execute
			time.Sleep(10 * time.Millisecond)

			if recovered {
				t.Error("Publish() should not propagate panic")
			}
		})
	}
}

// TestPanicRecoverySubsequentHandlers tests that other handlers still execute
// after one handler panics
func TestPanicRecoverySubsequentHandlers(t *testing.T) {
	tests := []struct {
		name         string
		panicIndex   int
		handlerCount int
	}{
		{
			name:         "panic in first handler",
			panicIndex:   0,
			handlerCount: 3,
		},
		{
			name:         "panic in middle handler",
			panicIndex:   1,
			handlerCount: 3,
		},
		{
			name:         "panic in last handler",
			panicIndex:   2,
			handlerCount: 3,
		},
		{
			name:         "multiple panicking handlers",
			panicIndex:   -1, // special case: multiple panics
			handlerCount: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := New(10)

			var callCount atomic.Int32
			var callOrder []int
			var mu sync.Mutex

			for i := 0; i < tt.handlerCount; i++ {
				handlerIndex := i
				b.Subscribe(MsgTaskCreated, func(msg Message) {
					mu.Lock()
					callOrder = append(callOrder, handlerIndex)
					mu.Unlock()
					callCount.Add(1)

					if tt.panicIndex == -1 && handlerIndex%2 == 0 {
						panic("panic from handler")
					} else if handlerIndex == tt.panicIndex {
						panic("panic from handler")
					}
				})
			}

			// Publish should complete without panic
			b.Publish(Message{Type: MsgTaskCreated})
			time.Sleep(20 * time.Millisecond)

			// All handlers should have been called
			if callCount.Load() != int32(tt.handlerCount) {
				t.Errorf("Expected %d handler calls, got %d", tt.handlerCount, callCount.Load())
			}

			// Verify all handlers were called in order
			mu.Lock()
			if len(callOrder) != tt.handlerCount {
				t.Errorf("Expected %d handlers in call order, got %d", tt.handlerCount, len(callOrder))
			}
			for i := 0; i < tt.handlerCount; i++ {
				if callOrder[i] != i {
					t.Errorf("Expected handler %d to be called at position %d, got %d", i, i, callOrder[i])
				}
			}
			mu.Unlock()
		})
	}
}

// TestPanicRecoveryWildcardHandlers tests panic recovery for wildcard handlers
func TestPanicRecoveryWildcardHandlers(t *testing.T) {
	b := New(10)

	var callCount atomic.Int32

	// Specific handler that panics
	b.Subscribe(MsgTaskCreated, func(msg Message) {
		callCount.Add(1)
		panic("specific handler panic")
	})

	// Wildcard handler that should still run
	b.SubscribeAll(func(msg Message) {
		callCount.Add(1)
	})

	// Another specific handler that should run
	b.Subscribe(MsgTaskCreated, func(msg Message) {
		callCount.Add(1)
	})

	// Wildcard handler that panics
	b.SubscribeAll(func(msg Message) {
		callCount.Add(1)
		panic("wildcard handler panic")
	})

	// Final wildcard handler that should run
	b.SubscribeAll(func(msg Message) {
		callCount.Add(1)
	})

	// Publish should complete without panic
	b.Publish(Message{Type: MsgTaskCreated})
	time.Sleep(20 * time.Millisecond)

	// All 5 handlers should have been called
	if callCount.Load() != 5 {
		t.Errorf("Expected 5 handler calls, got %d", callCount.Load())
	}
}

// TestPanicRecoveryBusRemainsUsable tests that the bus remains usable after a panic
func TestPanicRecoveryBusRemainsUsable(t *testing.T) {
	b := New(10)

	var callCount atomic.Int32

	// Handler that panics
	b.Subscribe(MsgTaskCreated, func(msg Message) {
		callCount.Add(1)
		panic("first handler panic")
	})

	// First publish - should not panic
	b.Publish(Message{Type: MsgTaskCreated})
	time.Sleep(10 * time.Millisecond)

	if callCount.Load() != 1 {
		t.Errorf("Expected 1 handler call after first publish, got %d", callCount.Load())
	}

	// Subscribe a new handler after the panic
	var newHandlerCalled atomic.Bool
	b.Subscribe(MsgTaskStatusChanged, func(msg Message) {
		newHandlerCalled.Store(true)
	})

	// Publish to the new message type - should work normally
	b.Publish(Message{Type: MsgTaskStatusChanged})
	time.Sleep(10 * time.Millisecond)

	if !newHandlerCalled.Load() {
		t.Error("New handler should have been called after panic recovery")
	}

	// Publish again to the original message type - should still work
	callCount.Store(0)
	b.Publish(Message{Type: MsgTaskCreated})
	time.Sleep(10 * time.Millisecond)

	if callCount.Load() != 1 {
		t.Errorf("Expected 1 handler call after second publish, got %d", callCount.Load())
	}

	// Subscribe another handler to the panicking message type
	var thirdHandlerCalled atomic.Bool
	b.Subscribe(MsgTaskCreated, func(msg Message) {
		thirdHandlerCalled.Store(true)
	})

	// Publish again - both handlers should be called
	callCount.Store(0)
	b.Publish(Message{Type: MsgTaskCreated})
	time.Sleep(10 * time.Millisecond)

	if callCount.Load() != 1 {
		t.Errorf("Expected original handler to be called, got %d", callCount.Load())
	}
	if !thirdHandlerCalled.Load() {
		t.Error("Third handler should have been called")
	}
}

// TestPanicRecoveryHistoryPreserved tests that message history is preserved after panic
func TestPanicRecoveryHistoryPreserved(t *testing.T) {
	b := New(10)

	b.Subscribe(MsgTaskCreated, func(msg Message) {
		panic("handler panic")
	})

	// Publish several messages
	for i := 0; i < 5; i++ {
		b.Publish(Message{Type: MsgTaskCreated, TaskID: string(rune('a' + i))})
	}
	time.Sleep(10 * time.Millisecond)

	// History should still be preserved
	history := b.History(10)
	if len(history) != 5 {
		t.Errorf("Expected 5 messages in history, got %d", len(history))
	}

	// Verify message content
	for i, msg := range history {
		expectedID := string(rune('a' + i))
		if msg.TaskID != expectedID {
			t.Errorf("Expected TaskID %s at position %d, got %s", expectedID, i, msg.TaskID)
		}
	}
}
