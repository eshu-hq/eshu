package parser

import (
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
)

const cfgDataflowFixture = `package handlers

func handle(req string) string {
	user := req
	if user != "" {
		user = sanitize(user)
	}
	return user
}

func sanitize(s string) string { return s }
`

// TestGoDataflowOffIsByteIdentical proves the dataflow gate is byte-identical
// when off: enabling it adds exactly the "dataflow_functions" bucket and changes
// nothing else, so existing fact contracts are untouched by default.
func TestGoDataflowOffIsByteIdentical(t *testing.T) {
	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "handlers.go")
	writeTestFile(t, filePath, cfgDataflowFixture)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v", err)
	}

	off, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath (off) error = %v", err)
	}
	if _, present := off["dataflow_functions"]; present {
		t.Fatalf("dataflow_functions present when gate off")
	}

	on, err := engine.ParsePath(repoRoot, filePath, false, Options{GoEmitDataflow: true})
	if err != nil {
		t.Fatalf("ParsePath (on) error = %v", err)
	}
	if _, present := on["dataflow_functions"]; !present {
		t.Fatalf("dataflow_functions absent when gate on")
	}

	// Removing the new bucket must reproduce the off payload exactly.
	delete(on, "dataflow_functions")
	if !reflect.DeepEqual(off, on) {
		t.Fatalf("enabling dataflow changed more than the dataflow_functions bucket")
	}
}

// TestGoDataflowEmitsReachingDefs proves the emitted bucket carries the
// reaching-definition truth for value flow through a parameter, an if-branch
// reassignment, and a merge.
func TestGoDataflowEmitsReachingDefs(t *testing.T) {
	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "handlers.go")
	writeTestFile(t, filePath, cfgDataflowFixture)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v", err)
	}
	got, err := engine.ParsePath(repoRoot, filePath, false, Options{GoEmitDataflow: true})
	if err != nil {
		t.Fatalf("ParsePath error = %v", err)
	}

	handle := dataflowFunctionByName(t, got, "handle")
	edges := defUseLineSet(t, handle)

	// Lines in cfgDataflowFixture: func handle on 3, user := req on 4,
	// if user != "" on 5, user = sanitize(user) on 6, return user on 8.
	want := map[string]bool{
		"req:3->4":  true, // user := req reads the parameter
		"user:4->5": true, // if condition reads the line-4 def
		"user:4->6": true, // sanitize(user) on the true path reads the line-4 def
		"user:4->8": true, // return user via the false path
		"user:6->8": true, // return user via the true-path reassignment
	}
	for edge := range want {
		if !edges[edge] {
			t.Fatalf("missing def->use %q in %v", edge, edges)
		}
	}
}

// dataflowFunctionByName returns the dataflow row for a function name.
func dataflowFunctionByName(t *testing.T, payload map[string]any, name string) map[string]any {
	t.Helper()
	rows, ok := payload["dataflow_functions"].([]map[string]any)
	if !ok {
		t.Fatalf("dataflow_functions bucket missing or wrong type: %T", payload["dataflow_functions"])
	}
	for _, row := range rows {
		if got, _ := row["name"].(string); got == name {
			return row
		}
	}
	t.Fatalf("dataflow row for %q not found", name)
	return nil
}

// defUseLineSet renders a function row's def->use edges as binding:defLine->useLine.
func defUseLineSet(t *testing.T, row map[string]any) map[string]bool {
	t.Helper()
	edges, ok := row["def_uses"].([]map[string]any)
	if !ok {
		t.Fatalf("def_uses missing or wrong type: %T", row["def_uses"])
	}
	out := make(map[string]bool, len(edges))
	for _, edge := range edges {
		binding, _ := edge["binding"].(string)
		defLine, _ := edge["def_line"].(int)
		useLine, _ := edge["use_line"].(int)
		out[keyDefUse(binding, defLine, useLine)] = true
	}
	return out
}

func keyDefUse(binding string, defLine, useLine int) string {
	return binding + ":" + strconv.Itoa(defLine) + "->" + strconv.Itoa(useLine)
}
