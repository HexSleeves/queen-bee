package llm

import "testing"

func TestNewClient(t *testing.T) {
	c := NewClient("test-key", "")
	if c == nil {
		t.Fatal("NewClient returned nil")
	}
	if c.model != "claude-sonnet-4-20250514" {
		t.Fatalf("expected default model, got %q", c.model)
	}
}

func TestNewClientCustomModel(t *testing.T) {
	c := NewClient("test-key", "claude-haiku-3-20240307")
	if c.model != "claude-haiku-3-20240307" {
		t.Fatalf("expected custom model, got %q", c.model)
	}
}

func TestMessage(t *testing.T) {
	m := Message{Role: "user", Content: "hello"}
	if m.Role != "user" || m.Content != "hello" {
		t.Fatal("Message fields not set correctly")
	}
}

func TestToSDKMessages(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello"},
	}
	sdkMsgs := toSDKMessages(msgs)
	if len(sdkMsgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(sdkMsgs))
	}
	if sdkMsgs[0].Role != "user" {
		t.Fatalf("expected user role, got %q", sdkMsgs[0].Role)
	}
	if sdkMsgs[1].Role != "assistant" {
		t.Fatalf("expected assistant role, got %q", sdkMsgs[1].Role)
	}
}
