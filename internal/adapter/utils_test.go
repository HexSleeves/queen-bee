package adapter

import (
	"strings"
	"sync"
	"testing"
)

func TestStreamWriterUnlimited(t *testing.T) {
	var mu sync.Mutex
	var buf strings.Builder
	sw := newStreamWriter(&mu, &buf, 0) // 0 = unlimited

	data := strings.Repeat("x", 10000)
	n, err := sw.Write([]byte(data))
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if n != 10000 {
		t.Errorf("expected n=10000, got %d", n)
	}
	if buf.Len() != 10000 {
		t.Errorf("expected buf len 10000, got %d", buf.Len())
	}
}

func TestStreamWriterTruncation(t *testing.T) {
	var mu sync.Mutex
	var buf strings.Builder
	sw := newStreamWriter(&mu, &buf, 100) // cap at 100 bytes

	// Write 60 bytes
	n, err := sw.Write([]byte(strings.Repeat("a", 60)))
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if n != 60 {
		t.Errorf("expected n=60, got %d", n)
	}

	// Write 80 more — should trigger truncation
	n, err = sw.Write([]byte(strings.Repeat("b", 80)))
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if n != 80 {
		t.Errorf("expected n=80 (reported), got %d", n)
	}

	output := buf.String()
	if !strings.Contains(output, truncationMarker) {
		t.Error("expected truncation marker in output")
	}
	// Should have 60 'a's + 40 'b's (remaining up to 100) + marker
	if !strings.HasPrefix(output, strings.Repeat("a", 60)+strings.Repeat("b", 40)) {
		t.Errorf("unexpected prefix: %q", output[:50])
	}

	// Further writes should be silently discarded
	n, err = sw.Write([]byte("more data"))
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if n != 9 {
		t.Errorf("expected n=9 (reported), got %d", n)
	}

	// Buffer should not have grown
	expectedLen := 100 + len(truncationMarker)
	if buf.Len() != expectedLen {
		t.Errorf("expected buf len %d, got %d", expectedLen, buf.Len())
	}
}

func TestStreamWriterExactCapHit(t *testing.T) {
	var mu sync.Mutex
	var buf strings.Builder
	sw := newStreamWriter(&mu, &buf, 50)

	// Write exactly 50 bytes — should fit
	_, _ = sw.Write([]byte(strings.Repeat("x", 50)))
	if buf.Len() != 50 {
		t.Errorf("expected 50, got %d", buf.Len())
	}

	// Next write triggers truncation
	_, _ = sw.Write([]byte("a"))
	if !strings.Contains(buf.String(), truncationMarker) {
		t.Error("expected truncation marker after exceeding cap")
	}
}
