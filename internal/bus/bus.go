package bus

import (
	"log"
	"sync"
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

type MessageBus struct {
	mu       sync.RWMutex
	handlers map[MsgType][]Handler
	history  []Message
	maxHist  int
}

func New(maxHistory int) *MessageBus {
	if maxHistory <= 0 {
		maxHistory = 10000
	}
	return &MessageBus{
		handlers: make(map[MsgType][]Handler),
		maxHist:  maxHistory,
	}
}

func (b *MessageBus) Subscribe(msgType MsgType, h Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[msgType] = append(b.handlers[msgType], h)
}

func (b *MessageBus) SubscribeAll(h Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers["*"] = append(b.handlers["*"], h)
}

func (b *MessageBus) Publish(msg Message) {
	b.mu.Lock()
	b.history = append(b.history, msg)
	if len(b.history) > b.maxHist {
		// Copy to a new slice to release the old backing array
		trimmed := make([]Message, b.maxHist)
		copy(trimmed, b.history[len(b.history)-b.maxHist:])
		b.history = trimmed
	}
	// Copy handlers under lock
	specific := make([]Handler, len(b.handlers[msg.Type]))
	copy(specific, b.handlers[msg.Type])
	wildcard := make([]Handler, len(b.handlers["*"]))
	copy(wildcard, b.handlers["*"])
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
