package bus

import (
	"log"
	"sync"
	"sync/atomic"
	"time"
)

type MsgType string

const (
	MsgTaskCreated       MsgType = "task.created"
	MsgTaskStatusChanged MsgType = "task.status_changed"
	MsgTaskAssigned      MsgType = "task.assigned"
	MsgWorkerSpawned     MsgType = "worker.spawned"
	MsgWorkerCompleted   MsgType = "worker.completed"
	MsgWorkerFailed      MsgType = "worker.failed"
	MsgWorkerOutput      MsgType = "worker.output"
	MsgBlackboardUpdate  MsgType = "blackboard.update"
	MsgQueenDecision     MsgType = "queen.decision"
	MsgQueenPlan         MsgType = "queen.plan"
	MsgSystemError       MsgType = "system.error"
)

type Message struct {
	Type     MsgType     `json:"type"`
	TaskID   string      `json:"task_id,omitempty"`
	WorkerID string      `json:"worker_id,omitempty"`
	Payload  interface{} `json:"payload,omitempty"`
	Time     time.Time   `json:"time"`
}

type Handler func(msg Message)

// Subscription is a handle returned by Subscribe that can be used to unsubscribe.
type Subscription struct {
	id      uint64
	msgType MsgType
	bus     *MessageBus
}

// Unsubscribe removes this handler from the bus.
func (s *Subscription) Unsubscribe() {
	if s.bus != nil {
		s.bus.unsubscribe(s.msgType, s.id)
	}
}

type handlerEntry struct {
	id uint64
	fn Handler
}

type MessageBus struct {
	mu       sync.RWMutex
	handlers map[MsgType][]handlerEntry
	history  []Message
	maxHist  int
	nextID   atomic.Uint64
}

func New(maxHistory int) *MessageBus {
	if maxHistory <= 0 {
		maxHistory = 10000
	}
	return &MessageBus{
		handlers: make(map[MsgType][]handlerEntry),
		maxHist:  maxHistory,
	}
}

// Subscribe registers a handler for a specific message type.
// Returns a Subscription that can be used to unsubscribe.
func (b *MessageBus) Subscribe(msgType MsgType, h Handler) *Subscription {
	id := b.nextID.Add(1)
	b.mu.Lock()
	b.handlers[msgType] = append(b.handlers[msgType], handlerEntry{id: id, fn: h})
	b.mu.Unlock()
	return &Subscription{id: id, msgType: msgType, bus: b}
}

// SubscribeAll registers a handler that receives all message types.
// Returns a Subscription that can be used to unsubscribe.
func (b *MessageBus) SubscribeAll(h Handler) *Subscription {
	return b.Subscribe("*", h)
}

func (b *MessageBus) unsubscribe(msgType MsgType, id uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	entries := b.handlers[msgType]
	for i, e := range entries {
		if e.id == id {
			b.handlers[msgType] = append(entries[:i], entries[i+1:]...)
			return
		}
	}
}

func (b *MessageBus) Publish(msg Message) {
	b.mu.Lock()
	b.history = append(b.history, msg)
	if len(b.history) > b.maxHist {
		trimmed := make([]Message, b.maxHist)
		copy(trimmed, b.history[len(b.history)-b.maxHist:])
		b.history = trimmed
	}
	// Copy handlers under lock
	specific := make([]Handler, len(b.handlers[msg.Type]))
	for i, e := range b.handlers[msg.Type] {
		specific[i] = e.fn
	}
	wildcard := make([]Handler, len(b.handlers["*"]))
	for i, e := range b.handlers["*"] {
		wildcard[i] = e.fn
	}
	b.mu.Unlock()

	for _, h := range specific {
		func(handler Handler) {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[MessageBus] Handler panicked for message type %s: %v", msg.Type, r)
				}
			}()
			handler(msg)
		}(h)
	}
	for _, h := range wildcard {
		func(handler Handler) {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[MessageBus] Wildcard handler panicked for message type %s: %v", msg.Type, r)
				}
			}()
			handler(msg)
		}(h)
	}
}

func (b *MessageBus) History(n int) []Message {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if n <= 0 || n > len(b.history) {
		n = len(b.history)
	}
	start := len(b.history) - n
	result := make([]Message, n)
	copy(result, b.history[start:])
	return result
}
