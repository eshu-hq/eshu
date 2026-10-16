// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicecatalog

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// runtimeSurfaceWritePattern matches a Cypher write that creates the surface
// GraphServiceRuntimeInstanceLoader reads: a WorkloadInstance node, or a
// RUNS_ON edge out of one.
var runtimeSurfaceWritePattern = regexp.MustCompile(
	`(?:MERGE|CREATE)\s*\(\w*:WorkloadInstance\b|(?:MERGE|CREATE)\s*\([^()]*\)\s*-\s*\[\w*:RUNS_ON\b`,
)

// runtimeWriterClass is the decision for one source file that writes the
// runtime surface: either it is a producer whose queue domain the fence waits
// on, or it is excluded with a reason. An exclusion that rests on "no
// production caller" lists the symbols that must stay unreferenced, so the
// reason is checked, not just stated.
type runtimeWriterClass struct {
	producer            reducercontract.Domain
	excluded            string
	mustNotBeReferenced []string
}

// runtimeWriterClasses is the enumerated producer set of the #7816 fence, by
// source file relative to the go module root. Every file the scan finds must
// be here and every entry must still be found.
var runtimeWriterClasses = map[string]runtimeWriterClass{
	"internal/reducer/workload_materializer.go": {
		producer: reducercontract.DomainWorkloadMaterialization,
	},
	"internal/storage/cypher/canonical_relationships.go": {
		excluded: "repo_dependency shared projection (edge/writer/writer.go, evidence_source " +
			"resolver/cross-repo) adds RUNS_ON to an existing WorkloadInstance. Its queue is " +
			"shared_projection_intents, not fact_work_items, so the #7816 fence does not wait on it. " +
			"Residual: a catalog read while that RUNS_ON intent is still pending can miss a platform " +
			"row; repairing a generation committed on partial input is #8169.",
	},
	"internal/storage/cypher/canonical.go": {
		excluded: "BuildCanonicalWorkloadInstanceUpsert and BuildCanonicalRuntimePlatformUpsert " +
			"have no production caller",
		mustNotBeReferenced: []string{
			"BuildCanonicalWorkloadInstanceUpsert",
			"BuildCanonicalRuntimePlatformUpsert",
		},
	},
	"cmd/ifa/materialize_workload_endpoints.go": {
		excluded: "Ifá conformance harness seeding a test graph, not a reducer runtime path",
	},
	"internal/backendconformance/corpus_answer_truth.go": {
		excluded: "backend-conformance corpus seeder, not a reducer runtime path",
	},
	"internal/backendconformance/corpus_value_flow.go": {
		excluded: "backend-conformance corpus seeder, not a reducer runtime path",
	},
}

// goModuleRoot is the go/ module root relative to this package directory.
const goModuleRoot = "../../.."

// scanRuntimeSurfaceWriters returns every non-test Go file under root that
// writes the runtime surface, as slash paths relative to root.
func scanRuntimeSurfaceWriters(t *testing.T, root string) []string {
	t.Helper()
	var found []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == "testdata" || entry.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if runtimeSurfaceWritePattern.Match(content) {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			found = append(found, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan %s: %v", root, err)
	}
	sort.Strings(found)
	return found
}

// referencingFiles returns the non-test Go files under root, other than
// owner, that mention symbol.
func referencingFiles(t *testing.T, root, owner, symbol string) []string {
	t.Helper()
	var refs []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if filepath.ToSlash(rel) == owner {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(content), symbol) {
			refs = append(refs, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan references to %s: %v", symbol, err)
	}
	return refs
}

// runtimeProducerSetProblems compares the writers found under root with the
// classification table and the fence's producer domains.
func runtimeProducerSetProblems(
	t *testing.T,
	root string,
	classes map[string]runtimeWriterClass,
	producers []reducercontract.Domain,
) []string {
	t.Helper()
	var problems []string
	found := scanRuntimeSurfaceWriters(t, root)
	for _, path := range found {
		if _, ok := classes[path]; !ok {
			problems = append(problems, "unclassified runtime-surface writer "+path+
				": add its queue domain to runtimeProducerDomains or classify it in runtimeWriterClasses with a reason")
		}
	}
	tabled := make([]string, 0, len(classes))
	for path := range classes {
		tabled = append(tabled, path)
	}
	sort.Strings(tabled)
	fenced := map[reducercontract.Domain]bool{}
	for _, path := range tabled {
		class := classes[path]
		if !slices.Contains(found, path) {
			problems = append(problems, "stale classification "+path+": it no longer writes the runtime surface")
		}
		switch {
		case class.producer != "":
			fenced[class.producer] = true
			if !slices.Contains(producers, class.producer) {
				problems = append(problems, "producer domain "+string(class.producer)+" of "+path+" is not in runtimeProducerDomains")
			}
		case strings.TrimSpace(class.excluded) == "":
			problems = append(problems, "classification "+path+" names neither a producer domain nor an exclusion reason")
		}
		for _, symbol := range class.mustNotBeReferenced {
			if refs := referencingFiles(t, root, path, symbol); len(refs) > 0 {
				problems = append(problems, "exclusion of "+path+" claims "+symbol+" has no production caller, but "+strings.Join(refs, ", ")+" reference it")
			}
		}
	}
	for _, producer := range producers {
		if !fenced[producer] {
			problems = append(problems, "runtimeProducerDomains entry "+string(producer)+" has no classified writer")
		}
	}
	return problems
}

// TestRuntimeProducerSetCoversEveryRuntimeWriter is the #7816 drift guard
// (GREEN on the real tree): every source file that writes a WorkloadInstance
// node or RUNS_ON edge is either a fenced producer or excluded with a reason.
func TestRuntimeProducerSetCoversEveryRuntimeWriter(t *testing.T) {
	t.Parallel()

	if problems := runtimeProducerSetProblems(t, goModuleRoot, runtimeWriterClasses, RuntimeProducerDomains()); len(problems) > 0 {
		t.Fatalf("runtime producer set drifted from source:\n  %s", strings.Join(problems, "\n  "))
	}
}

// TestRuntimeProducerSetGuardFailsOnSeededDrift is the seeded RED half: a
// planted new writer, a planted caller of an excluded builder, and a producer
// domain dropped from the fence each make the guard report a problem.
func TestRuntimeProducerSetGuardFailsOnSeededDrift(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("internal/reducer/workload_materializer.go", "package reducer\nconst q = `MERGE (i:WorkloadInstance {id: row.instance_id})`\n")
	write("internal/storage/cypher/canonical.go", "package cypher\nconst q = `MERGE (i)-[rel:RUNS_ON {identity_key: 'canonical'}]->(p)`\nfunc BuildCanonicalWorkloadInstanceUpsert() {}\n")
	classes := map[string]runtimeWriterClass{
		"internal/reducer/workload_materializer.go": runtimeWriterClasses["internal/reducer/workload_materializer.go"],
		"internal/storage/cypher/canonical.go":      runtimeWriterClasses["internal/storage/cypher/canonical.go"],
	}
	producers := []reducercontract.Domain{reducercontract.DomainWorkloadMaterialization}
	if problems := runtimeProducerSetProblems(t, root, classes, producers); len(problems) > 0 {
		t.Fatalf("clean seeded tree reported problems: %v", problems)
	}

	cases := map[string]struct {
		seed      func()
		producers []reducercontract.Domain
		want      string
	}{
		"new writer": {
			seed: func() {
				write("internal/reducer/newfamily/writer.go", "package newfamily\nconst q = `UNWIND $rows AS row\nMERGE (instance:WorkloadInstance {id: row.id})`\n")
			},
			producers: producers,
			want:      "unclassified runtime-surface writer internal/reducer/newfamily/writer.go",
		},
		"excluded builder gains a caller": {
			seed: func() {
				write("cmd/reducer/wiring.go", "package main\nvar _ = cypher.BuildCanonicalWorkloadInstanceUpsert\n")
			},
			producers: producers,
			want:      "claims BuildCanonicalWorkloadInstanceUpsert has no production caller",
		},
		"producer dropped from the fence": {
			seed:      func() {},
			producers: nil,
			want:      "producer domain workload_materialization of internal/reducer/workload_materializer.go is not in runtimeProducerDomains",
		},
	}
	for name, tc := range cases {
		tc.seed()
		problems := runtimeProducerSetProblems(t, root, classes, tc.producers)
		if !slices.ContainsFunc(problems, func(p string) bool { return strings.Contains(p, tc.want) }) {
			t.Fatalf("%s: problems = %v, want one containing %q", name, problems, tc.want)
		}
	}
}
