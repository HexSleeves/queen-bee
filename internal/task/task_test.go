package task

import (
	"strings"
	"testing"

	"github.com/HexSleeves/waggle/internal/bus"
)

func TestDetectCycles_NoCycleLinearChain(t *testing.T) {
	// Test case: No cycle in a simple linear dependency chain
	// A -> B -> C (A depends on B, B depends on C)
	g := NewTaskGraph(bus.New(100))

	g.Add(&Task{ID: "C"})
	g.Add(&Task{ID: "B", DependsOn: []string{"C"}})
	g.Add(&Task{ID: "A", DependsOn: []string{"B"}})

	err := g.DetectCycles()
	if err != nil {
		t.Errorf("Expected no cycle detection for linear chain, got error: %v", err)
	}
}

func TestDetectCycles_SimpleTwoNodeCycle(t *testing.T) {
	// Test case: Simple 2-node cycle (A depends on B, B depends on A)
	g := NewTaskGraph(bus.New(100))

	g.Add(&Task{ID: "A", DependsOn: []string{"B"}})
	g.Add(&Task{ID: "B", DependsOn: []string{"A"}})

	err := g.DetectCycles()
	if err == nil {
		t.Error("Expected cycle detection for 2-node cycle, got nil")
	} else {
		if !strings.Contains(err.Error(), "circular dependency detected") {
			t.Errorf("Expected error message to contain 'circular dependency detected', got: %v", err)
		}
		// Check that the cycle is properly described in the error
		if !strings.Contains(err.Error(), "A") || !strings.Contains(err.Error(), "B") {
			t.Errorf("Expected error message to mention both A and B, got: %v", err)
		}
	}
}

func TestDetectCycles_ThreeNodeCycle(t *testing.T) {
	// Test case: 3-node cycle (A->B->C->A)
	g := NewTaskGraph(bus.New(100))

	g.Add(&Task{ID: "A", DependsOn: []string{"B"}})
	g.Add(&Task{ID: "B", DependsOn: []string{"C"}})
	g.Add(&Task{ID: "C", DependsOn: []string{"A"}})

	err := g.DetectCycles()
	if err == nil {
		t.Error("Expected cycle detection for 3-node cycle, got nil")
	} else {
		if !strings.Contains(err.Error(), "circular dependency detected") {
			t.Errorf("Expected error message to contain 'circular dependency detected', got: %v", err)
		}
		// Check that all three nodes are mentioned in the error
		if !strings.Contains(err.Error(), "A") || !strings.Contains(err.Error(), "B") || !strings.Contains(err.Error(), "C") {
			t.Errorf("Expected error message to mention A, B, and C, got: %v", err)
		}
	}
}

func TestDetectCycles_SelfLoop(t *testing.T) {
	// Test case: Self-loop (A depends on A)
	g := NewTaskGraph(bus.New(100))

	g.Add(&Task{ID: "A", DependsOn: []string{"A"}})

	err := g.DetectCycles()
	if err == nil {
		t.Error("Expected cycle detection for self-loop, got nil")
	} else {
		if !strings.Contains(err.Error(), "circular dependency detected") {
			t.Errorf("Expected error message to contain 'circular dependency detected', got: %v", err)
		}
		if !strings.Contains(err.Error(), "A") {
			t.Errorf("Expected error message to mention A, got: %v", err)
		}
	}
}

func TestDetectCycles_ComplexGraphWithMultipleCycles(t *testing.T) {
	// Test case: Complex graph with multiple cycles
	// Cycle 1: A -> B -> C -> A
	// Cycle 2: D -> E -> D
	// No cycle: F -> G
	g := NewTaskGraph(bus.New(100))

	g.Add(&Task{ID: "A", DependsOn: []string{"B"}})
	g.Add(&Task{ID: "B", DependsOn: []string{"C"}})
	g.Add(&Task{ID: "C", DependsOn: []string{"A"}})
	g.Add(&Task{ID: "D", DependsOn: []string{"E"}})
	g.Add(&Task{ID: "E", DependsOn: []string{"D"}})
	g.Add(&Task{ID: "F", DependsOn: []string{"G"}})
	g.Add(&Task{ID: "G"})

	err := g.DetectCycles()
	if err == nil {
		t.Error("Expected cycle detection for complex graph with multiple cycles, got nil")
	} else {
		if !strings.Contains(err.Error(), "circular dependency detected") {
			t.Errorf("Expected error message to contain 'circular dependency detected', got: %v", err)
		}
		// The error should describe at least one of the cycles
		// It will detect the first cycle found, which could be either cycle
		cycleDesc := err.Error()
		hasCycle1 := strings.Contains(cycleDesc, "A") && strings.Contains(cycleDesc, "B") && strings.Contains(cycleDesc, "C")
		hasCycle2 := strings.Contains(cycleDesc, "D") && strings.Contains(cycleDesc, "E")
		if !hasCycle1 && !hasCycle2 {
			t.Errorf("Expected error message to describe one of the cycles (A-B-C or D-E), got: %v", err)
		}
	}
}

func TestDetectCycles_DisconnectedComponentsWithCycle(t *testing.T) {
	// Test case: Disconnected components with a cycle in one component
	// Component 1: A -> B (no cycle)
	// Component 2: C -> D -> E -> C (cycle)
	// Component 3: F (isolated, no dependencies)
	g := NewTaskGraph(bus.New(100))

	g.Add(&Task{ID: "A", DependsOn: []string{"B"}})
	g.Add(&Task{ID: "B"})
	g.Add(&Task{ID: "C", DependsOn: []string{"D"}})
	g.Add(&Task{ID: "D", DependsOn: []string{"E"}})
	g.Add(&Task{ID: "E", DependsOn: []string{"C"}})
	g.Add(&Task{ID: "F"})

	err := g.DetectCycles()
	if err == nil {
		t.Error("Expected cycle detection for disconnected components with cycle, got nil")
	} else {
		if !strings.Contains(err.Error(), "circular dependency detected") {
			t.Errorf("Expected error message to contain 'circular dependency detected', got: %v", err)
		}
		// The cycle should be in component 2 (C, D, E)
		if !strings.Contains(err.Error(), "C") || !strings.Contains(err.Error(), "D") || !strings.Contains(err.Error(), "E") {
			t.Errorf("Expected error message to mention the cycle nodes C, D, and E, got: %v", err)
		}
	}
}

func TestDetectCycles_EmptyGraph(t *testing.T) {
	// Test case: Empty graph (no tasks)
	g := NewTaskGraph(bus.New(100))

	err := g.DetectCycles()
	if err != nil {
		t.Errorf("Expected no cycle detection for empty graph, got error: %v", err)
	}
}

func TestDetectCycles_SingleNodeNoDependencies(t *testing.T) {
	// Test case: Single node with no dependencies
	g := NewTaskGraph(bus.New(100))

	g.Add(&Task{ID: "A"})

	err := g.DetectCycles()
	if err != nil {
		t.Errorf("Expected no cycle detection for single node with no dependencies, got error: %v", err)
	}
}

func TestDetectCycles_DiamondDependencyNoCycle(t *testing.T) {
	// Test case: Diamond dependency pattern (no cycle)
	//     A
	//    / \
	//   B   C
	//    \ /
	//     D
	// A depends on B and C; B and C depend on D
	g := NewTaskGraph(bus.New(100))

	g.Add(&Task{ID: "D"})
	g.Add(&Task{ID: "C", DependsOn: []string{"D"}})
	g.Add(&Task{ID: "B", DependsOn: []string{"D"}})
	g.Add(&Task{ID: "A", DependsOn: []string{"B", "C"}})

	err := g.DetectCycles()
	if err != nil {
		t.Errorf("Expected no cycle detection for diamond dependency, got error: %v", err)
	}
}

func TestDetectCycles_DependencyOnNonExistentTask(t *testing.T) {
	// Test case: Task depends on a non-existent task (should not cause cycle)
	g := NewTaskGraph(bus.New(100))

	g.Add(&Task{ID: "A", DependsOn: []string{"B"}})
	// B does not exist in the graph

	err := g.DetectCycles()
	if err != nil {
		t.Errorf("Expected no cycle detection when depending on non-existent task, got error: %v", err)
	}
}

func TestDetectCycles_LongCycle(t *testing.T) {
	// Test case: Long cycle (A->B->C->D->E->F->G->H->I->J->A)
	g := NewTaskGraph(bus.New(100))

	g.Add(&Task{ID: "A", DependsOn: []string{"B"}})
	g.Add(&Task{ID: "B", DependsOn: []string{"C"}})
	g.Add(&Task{ID: "C", DependsOn: []string{"D"}})
	g.Add(&Task{ID: "D", DependsOn: []string{"E"}})
	g.Add(&Task{ID: "E", DependsOn: []string{"F"}})
	g.Add(&Task{ID: "F", DependsOn: []string{"G"}})
	g.Add(&Task{ID: "G", DependsOn: []string{"H"}})
	g.Add(&Task{ID: "H", DependsOn: []string{"I"}})
	g.Add(&Task{ID: "I", DependsOn: []string{"J"}})
	g.Add(&Task{ID: "J", DependsOn: []string{"A"}})

	err := g.DetectCycles()
	if err == nil {
		t.Error("Expected cycle detection for long cycle, got nil")
	} else {
		if !strings.Contains(err.Error(), "circular dependency detected") {
			t.Errorf("Expected error message to contain 'circular dependency detected', got: %v", err)
		}
		// Check that the cycle contains A and J
		if !strings.Contains(err.Error(), "A") || !strings.Contains(err.Error(), "J") {
			t.Errorf("Expected error message to mention A and J, got: %v", err)
		}
	}
}

func TestDetectCycles_BranchingWithCycle(t *testing.T) {
	// Test case: Branching structure with a cycle in one branch
	//      A
	//     / \
	//    B   C
	//   /     \
	//  D       E
	//   \     /
	//    \   /
	//     \ /
	//      F
	// Where F depends on D, creating a cycle D->F->B->D
	g := NewTaskGraph(bus.New(100))

	g.Add(&Task{ID: "A", DependsOn: []string{"B", "C"}})
	g.Add(&Task{ID: "B", DependsOn: []string{"D"}})
	g.Add(&Task{ID: "C", DependsOn: []string{"E"}})
	g.Add(&Task{ID: "D", DependsOn: []string{"F"}})
	g.Add(&Task{ID: "E"})
	g.Add(&Task{ID: "F", DependsOn: []string{"B"}}) // Creates cycle: B -> D -> F -> B

	err := g.DetectCycles()
	if err == nil {
		t.Error("Expected cycle detection for branching with cycle, got nil")
	} else {
		if !strings.Contains(err.Error(), "circular dependency detected") {
			t.Errorf("Expected error message to contain 'circular dependency detected', got: %v", err)
		}
		// Check that the cycle nodes are mentioned
		if !strings.Contains(err.Error(), "B") || !strings.Contains(err.Error(), "D") || !strings.Contains(err.Error(), "F") {
			t.Errorf("Expected error message to mention B, D, and F, got: %v", err)
		}
	}
}
