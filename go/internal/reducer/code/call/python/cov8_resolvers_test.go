// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package python

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/shared"
)

// cov8PythonFile builds one parsed-file envelope.
func cov8PythonFile(repo, relative string, fileData map[string]any) facts.Envelope {
	return facts.Envelope{
		FactKind: "file",
		Payload: map[string]any{
			"repo_id":          repo,
			"relative_path":    relative,
			"parsed_file_data": fileData,
		},
	}
}

// cov8PythonIndex builds the worker/base fixture: Worker(Base) defines
// run, Child(Worker) inherits it, and Lone defines nothing shared.
func cov8PythonIndex() shared.EntityIndex {
	fileData := map[string]any{
		"path": "pkg/worker.py",
		"classes": []any{
			map[string]any{
				"name": "Worker", "uid": "entity-worker",
				"bases": []any{"Base"}, "line_number": 1, "end_line": 10,
			},
			map[string]any{
				"name": "Child", "uid": "entity-child",
				"bases": []any{"Worker"}, "line_number": 12, "end_line": 20,
			},
		},
		"functions": []any{
			map[string]any{
				"name": "run", "uid": "entity-worker-run",
				"class_context": "Worker", "line_number": 2, "end_line": 4,
			},
		},
	}
	return shared.BuildEntityIndex([]facts.Envelope{
		cov8PythonFile("repo-py", "pkg/worker.py", fileData),
	})
}

// TestCov8PythonResolversListShape pins the single declared-base resolver.
func TestCov8PythonResolversListShape(t *testing.T) {
	t.Parallel()

	resolvers := Resolvers()
	if len(resolvers) != 1 {
		t.Fatalf("len(Resolvers()) = %d, want 1", len(resolvers))
	}
	if resolvers[0].Phase != shared.PhaseBeforeRepoFallback || resolvers[0].Resolve == nil {
		t.Errorf("Resolvers()[0] = %+v, want the before-fallback resolver", resolvers[0])
	}
}

// TestCov8PythonDeclaredBaseCallee pins declared-base resolution: a
// direct class method binds, an inherited method walks the bases, and
// ambiguous or unprovable calls stay empty.
func TestCov8PythonDeclaredBaseCallee(t *testing.T) {
	t.Parallel()

	index := cov8PythonIndex()
	resolve := Resolvers()[0].Resolve

	direct := shared.ResolveContext{
		Index: index, RepositoryID: "repo-py",
		Call:     map[string]any{"name": "run", "full_name": "Worker.run"},
		Language: "python",
	}
	entityID, file, method := resolve(direct)
	if entityID != "entity-worker-run" || file != "pkg/worker.py" ||
		method != codeprovenance.MethodTypeInferred {
		t.Errorf("Resolve(direct) = %q, %q, %q; want entity-worker-run via type inference",
			entityID, file, method)
	}

	inherited := direct
	inherited.Call = map[string]any{"name": "run", "full_name": "Child.run"}
	if entityID, _, _ := resolve(inherited); entityID != "entity-worker-run" {
		t.Errorf("Resolve(inherited) = %q, want entity-worker-run", entityID)
	}

	for name, call := range map[string]map[string]any{
		"unqualified":    {"name": "run", "full_name": "run"},
		"lowercase":      {"name": "run", "full_name": "mod.run"},
		"unknown class":  {"name": "run", "full_name": "Ghost.run"},
		"unknown method": {"name": "stop", "full_name": "Worker.stop"},
	} {
		guarded := direct
		guarded.Call = call
		if entityID, _, _ := resolve(guarded); entityID != "" {
			t.Errorf("Resolve(%s) = %q, want empty", name, entityID)
		}
	}
	noRepo := direct
	noRepo.RepositoryID = ""
	if entityID, _, _ := resolve(noRepo); entityID != "" {
		t.Errorf("Resolve(no repo) = %q, want empty", entityID)
	}
}

// TestCov8PythonAmbiguousDirectStaysEmpty pins the ambiguity fail-closed:
// when two receiver spellings match different entities, the call
// resolves to nothing rather than guessing.
func TestCov8PythonAmbiguousDirectStaysEmpty(t *testing.T) {
	t.Parallel()

	fileData := map[string]any{
		"path": "pkg/amb.py",
		"functions": []any{
			map[string]any{
				"name": "run", "full_name": "mod.Worker.run",
				"uid": "entity-mod-run", "line_number": 1, "end_line": 2,
			},
			map[string]any{
				"name": "run", "uid": "entity-worker-run",
				"class_context": "Worker", "line_number": 5, "end_line": 6,
			},
		},
	}
	index := shared.BuildEntityIndex([]facts.Envelope{
		cov8PythonFile("repo-py", "pkg/amb.py", fileData),
	})
	ctx := shared.ResolveContext{
		Index: index, RepositoryID: "repo-py",
		Call:     map[string]any{"name": "run", "full_name": "mod.Worker.run"},
		Language: "python",
	}
	if entityID, _, _ := Resolvers()[0].Resolve(ctx); entityID != "" {
		t.Errorf("Resolve(ambiguous) = %q, want empty", entityID)
	}
}

// TestCov8PythonInheritedWalkTerminates pins base walking: a cyclic
// hierarchy terminates with no resolution instead of looping.
func TestCov8PythonInheritedWalkTerminates(t *testing.T) {
	t.Parallel()

	fileData := map[string]any{
		"path": "pkg/cycle.py",
		"classes": []any{
			map[string]any{
				"name": "A", "uid": "entity-a",
				"bases": []any{"B"}, "line_number": 1, "end_line": 4,
			},
			map[string]any{
				"name": "B", "uid": "entity-b",
				"bases": []any{"A"}, "line_number": 6, "end_line": 9,
			},
		},
	}
	index := shared.BuildEntityIndex([]facts.Envelope{
		cov8PythonFile("repo-py", "pkg/cycle.py", fileData),
	})
	ctx := shared.ResolveContext{
		Index: index, RepositoryID: "repo-py",
		Call:     map[string]any{"name": "run", "full_name": "A.run"},
		Language: "python",
	}
	if entityID, _, _ := Resolvers()[0].Resolve(ctx); entityID != "" {
		t.Errorf("Resolve(cyclic, no method) = %q, want empty", entityID)
	}
}

// TestCov8PythonQualifiedClassMethod pins receiver parsing: only
// Capitalized.receiver shapes qualify, with surrounding space tolerated.
func TestCov8PythonQualifiedClassMethod(t *testing.T) {
	t.Parallel()

	receiver, method, ok := pythonQualifiedClassMethod(map[string]any{"full_name": "  Worker.run  "})
	if !ok || receiver != "Worker" || method != "run" {
		t.Errorf("qualified = %q, %q, %v; want Worker, run, true", receiver, method, ok)
	}
	for name, fullName := range map[string]string{
		"bare name":     "run",
		"lowercase":     "worker.run",
		"trailing dot":  "Worker.",
		"leading dot":   ".run",
		"empty":         "",
		"dotted no cap": "a.b.helper",
	} {
		if _, _, ok := pythonQualifiedClassMethod(map[string]any{"full_name": fullName}); ok {
			t.Errorf("qualified(%s=%q) = true, want false", name, fullName)
		}
	}
}

// TestCov8PythonClassBaseCandidateNames pins base spelling expansion: a
// dotted base contributes both the qualified and trailing names.
func TestCov8PythonClassBaseCandidateNames(t *testing.T) {
	t.Parallel()

	if got := pythonClassBaseCandidateNames(""); len(got) != 0 {
		t.Errorf("bases(empty) = %v, want nil", got)
	}
	if got := pythonClassBaseCandidateNames("Worker"); len(got) != 1 || got[0] != "Worker" {
		t.Errorf("bases(Worker) = %v, want [Worker]", got)
	}
	got := pythonClassBaseCandidateNames("mod.Worker")
	if len(got) != 2 || got[0] != "mod.Worker" || got[1] != "Worker" {
		t.Errorf("bases(mod.Worker) = %v, want [mod.Worker Worker]", got)
	}
}

// TestCov8UniquePythonResolvedEntity pins the cardinality gate: exactly
// one match resolves, zero or many resolve to nothing.
func TestCov8UniquePythonResolvedEntity(t *testing.T) {
	t.Parallel()

	if got := uniquePythonResolvedEntity(map[string]struct{}{}); got != "" {
		t.Errorf("unique(empty) = %q, want empty", got)
	}
	if got := uniquePythonResolvedEntity(map[string]struct{}{"e1": {}}); got != "e1" {
		t.Errorf("unique(one) = %q, want e1", got)
	}
	if got := uniquePythonResolvedEntity(map[string]struct{}{"e1": {}, "e2": {}}); got != "" {
		t.Errorf("unique(two) = %q, want empty", got)
	}
	if got := resolvePythonClassMethod(shared.ResolveContext{}, "", "m"); got != "" {
		t.Errorf("classMethod(no repo) = %q, want empty", got)
	}
}
