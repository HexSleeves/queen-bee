package queen

import (
	"strings"
	"testing"

	"github.com/exedev/waggle/internal/task"
)

// ---------------------------------------------------------------------------
// parseReviewVerdict tests
// ---------------------------------------------------------------------------

func TestParseReviewVerdict_CleanJSON(t *testing.T) {
	input := `{"approved":true,"reason":"looks good","suggestions":[],"new_tasks":[]}`
	v, err := parseReviewVerdict(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !v.Approved {
		t.Error("expected approved=true")
	}
	if v.Reason != "looks good" {
		t.Errorf("reason=%q, want %q", v.Reason, "looks good")
	}
}

func TestParseReviewVerdict_MarkdownFences(t *testing.T) {
	input := "Here is my review:\n```json\n" +
		`{"approved":false,"reason":"missing tests","suggestions":["add unit tests"],"new_tasks":[]}` +
		"\n```\nDone."
	v, err := parseReviewVerdict(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.Approved {
		t.Error("expected approved=false")
	}
	if len(v.Suggestions) != 1 || v.Suggestions[0] != "add unit tests" {
		t.Errorf("suggestions=%v", v.Suggestions)
	}
}

func TestParseReviewVerdict_WithNewTasks(t *testing.T) {
	input := `{
		"approved": true,
		"reason": "implementation is correct",
		"suggestions": [],
		"new_tasks": [
			{
				"type": "test",
				"title": "Add integration tests",
				"description": "Cover edge cases",
				"depends_on": ["task-1"]
			}
		]
	}`
	v, err := parseReviewVerdict(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(v.NewTasks) != 1 {
		t.Fatalf("expected 1 new task, got %d", len(v.NewTasks))
	}
	nt := v.NewTasks[0]
	if nt.Type != "test" {
		t.Errorf("type=%q, want %q", nt.Type, "test")
	}
	if nt.Title != "Add integration tests" {
		t.Errorf("title=%q", nt.Title)
	}
	if len(nt.DependsOn) != 1 || nt.DependsOn[0] != "task-1" {
		t.Errorf("depends_on=%v", nt.DependsOn)
	}
}

func TestParseReviewVerdict_SurroundingText(t *testing.T) {
	input := "Sure, here is my verdict:\n" +
		`{"approved":true,"reason":"all good"}` +
		"\nLet me know if you need anything else."
	v, err := parseReviewVerdict(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !v.Approved {
		t.Error("expected approved=true")
	}
}

func TestParseReviewVerdict_NoJSON(t *testing.T) {
	_, err := parseReviewVerdict("I think it looks fine.")
	if err == nil {
		t.Fatal("expected error for input without JSON")
	}
}

func TestParseReviewVerdict_UnbalancedBraces(t *testing.T) {
	_, err := parseReviewVerdict(`{"approved": true, "reason": "ok"`)
	if err == nil {
		t.Fatal("expected error for unbalanced braces")
	}
}

// ---------------------------------------------------------------------------
// buildReviewPrompt tests
// ---------------------------------------------------------------------------

func TestBuildReviewPrompt_ContainsTaskFields(t *testing.T) {
	tk := &task.Task{
		ID:          "task-42",
		Type:        task.TypeCode,
		Title:       "Implement foo",
		Description: "Write the foo function",
		Constraints: []string{"Do not modify bar.go"},
	}
	res := &task.Result{
		Success: true,
		Output:  "Added foo() in foo.go",
	}

	prompt := buildReviewPrompt(tk, res)

	for _, want := range []string{"task-42", "code", "Implement foo", "Write the foo function", "Do not modify bar.go", "Added foo() in foo.go"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

func TestBuildReviewPrompt_NilResult(t *testing.T) {
	tk := &task.Task{
		ID:    "task-1",
		Title: "test task",
	}
	prompt := buildReviewPrompt(tk, nil)
	if !strings.Contains(prompt, "no result returned") {
		t.Error("expected nil-result placeholder")
	}
}

func TestBuildReviewPrompt_TruncatesLongOutput(t *testing.T) {
	tk := &task.Task{ID: "task-1", Title: "test"}
	long := strings.Repeat("x", maxOutputChars+500)
	res := &task.Result{Success: true, Output: long}

	prompt := buildReviewPrompt(tk, res)
	if !strings.Contains(prompt, "[truncated]") {
		t.Error("expected truncation marker")
	}
	// The prompt should not contain the full long output.
	if strings.Contains(prompt, long) {
		t.Error("output was not truncated")
	}
}

func TestBuildReviewPrompt_IncludesErrors(t *testing.T) {
	tk := &task.Task{ID: "task-1", Title: "test"}
	res := &task.Result{
		Success: false,
		Output:  "",
		Errors:  []string{"compile error: undefined var"},
	}
	prompt := buildReviewPrompt(tk, res)
	if !strings.Contains(prompt, "compile error: undefined var") {
		t.Error("expected error text in prompt")
	}
}

// ---------------------------------------------------------------------------
// findMatchingBrace tests
// ---------------------------------------------------------------------------

func TestFindMatchingBrace_Simple(t *testing.T) {
	s := `{"a": 1}`
	idx := findMatchingBrace(s, 0)
	if idx != len(s)-1 {
		t.Errorf("got %d, want %d", idx, len(s)-1)
	}
}

func TestFindMatchingBrace_Nested(t *testing.T) {
	s := `{"a": {"b": 2}}`
	idx := findMatchingBrace(s, 0)
	if idx != len(s)-1 {
		t.Errorf("got %d, want %d", idx, len(s)-1)
	}
}

func TestFindMatchingBrace_StringWithBraces(t *testing.T) {
	s := `{"a": "}{"}`
	idx := findMatchingBrace(s, 0)
	if idx != len(s)-1 {
		t.Errorf("got %d, want %d", idx, len(s)-1)
	}
}

func TestFindMatchingBrace_NotFound(t *testing.T) {
	idx := findMatchingBrace(`{"a": 1`, 0)
	if idx != -1 {
		t.Errorf("got %d, want -1", idx)
	}
}
