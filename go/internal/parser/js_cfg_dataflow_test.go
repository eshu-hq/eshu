package parser

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// jsDataflowFixture exercises both an intraprocedural flow (req.body into
// db.query within view) and an interprocedural flow (req passed into runQuery,
// whose parameter reaches a db.query sink).
const jsDataflowFixture = `function view(req: Request, db) {
	const q = req.body;
	db.query(q);
	runQuery(db, req);
}

function runQuery(db, q) {
	db.query(q);
}
`

// TestJSDataflowOffIsByteIdentical proves the value-flow gate is byte-identical
// when off for the TypeScript/JavaScript adapter: enabling it adds exactly the
// opt-in buckets and changes nothing else.
func TestJSDataflowOffIsByteIdentical(t *testing.T) {
	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "views.ts")
	writeTestFile(t, filePath, jsDataflowFixture)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v", err)
	}

	off, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath (off) error = %v", err)
	}
	for _, bucket := range []string{"dataflow_catalog_versions", "dataflow_functions", "taint_findings", "interproc_findings"} {
		if _, present := off[bucket]; present {
			t.Fatalf("%s present when gate off", bucket)
		}
	}

	on, err := engine.ParsePath(repoRoot, filePath, false, Options{EmitDataflow: true})
	if err != nil {
		t.Fatalf("ParsePath (on) error = %v", err)
	}
	if _, present := on["dataflow_functions"]; !present {
		t.Fatalf("dataflow_functions absent when gate on")
	}

	delete(on, "dataflow_functions")
	delete(on, "taint_findings")
	delete(on, "interproc_findings")
	delete(on, "dataflow_catalog_versions")
	if !reflect.DeepEqual(off, on) {
		t.Fatalf("enabling dataflow changed more than the opt-in buckets")
	}
}

// TestJSTaintSourceToSQLSink proves the intraprocedural taint bucket reports a
// request parameter reaching db.query as a TAINTED sql finding, labeled with the
// output language.
func TestJSTaintSourceToSQLSink(t *testing.T) {
	got := parseJSDataflowFixture(t, jsDataflowFixture)
	rows, ok := got["taint_findings"].([]map[string]any)
	if !ok {
		t.Fatalf("taint_findings bucket missing or wrong type: %T", got["taint_findings"])
	}
	found := false
	for _, row := range rows {
		fn, _ := row["function_name"].(string)
		kind, _ := row["kind"].(string)
		sinkKind, _ := row["sink_kind"].(string)
		lang, _ := row["lang"].(string)
		if fn == "view" && kind == "TAINTED" && sinkKind == "sql" && lang == "typescript" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a TAINTED sql finding in view labeled typescript, got %+v", rows)
	}
}

// TestJSInterprocFindingAcrossFunctions proves the interprocedural bucket reports
// a request parameter in view reaching a db.query sink in the runQuery callee.
func TestJSInterprocFindingAcrossFunctions(t *testing.T) {
	got := parseJSDataflowFixture(t, jsDataflowFixture)
	rows, ok := got["interproc_findings"].([]map[string]any)
	if !ok {
		t.Fatalf("interproc_findings bucket missing or wrong type: %T", got["interproc_findings"])
	}
	found := false
	for _, row := range rows {
		srcFn, _ := row["source_func"].(string)
		sinkFn, _ := row["sink_func"].(string)
		sinkKind, _ := row["sink_kind"].(string)
		if strings.Contains(srcFn, "view") && strings.Contains(sinkFn, "runQuery") && sinkKind == "sql" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an interprocedural view->runQuery sql finding, got %+v", rows)
	}
}

// TestJSInterprocFunctionIDsIncludeRepositoryID proves JS/TS value-flow
// identities carry stable repository identity when emitted for durable summary
// persistence.
func TestJSInterprocFunctionIDsIncludeRepositoryID(t *testing.T) {
	got := parseJSDataflowFixtureWithOptions(t, jsDataflowFixture, Options{EmitDataflow: true, RepositoryID: "repo-alpha"})
	rows, ok := got["interproc_findings"].([]map[string]any)
	if !ok {
		t.Fatalf("interproc_findings bucket missing or wrong type: %T", got["interproc_findings"])
	}
	for _, row := range rows {
		sourceFunc, _ := row["source_func"].(string)
		sinkFunc, _ := row["sink_func"].(string)
		if !strings.HasPrefix(sourceFunc, "repo-alpha\x1f") || !strings.HasPrefix(sinkFunc, "repo-alpha\x1f") {
			t.Fatalf("interproc FunctionIDs must include repo-alpha, got %+v", row)
		}
	}
}

// TestJSTaintInClassMethod proves intraprocedural taint is emitted for a class
// method and carries the enclosing class name as class_context.
func TestJSTaintInClassMethod(t *testing.T) {
	got := parseJSDataflowFixture(t, "class Repo {\n"+
		"\trun(req: Request, db) {\n"+
		"\t\tdb.query(req.body);\n"+
		"\t}\n"+
		"}\n")
	rows, ok := got["taint_findings"].([]map[string]any)
	if !ok {
		t.Fatalf("taint_findings bucket missing or wrong type: %T", got["taint_findings"])
	}
	found := false
	for _, row := range rows {
		fn, _ := row["function_name"].(string)
		class, _ := row["class_context"].(string)
		sinkKind, _ := row["sink_kind"].(string)
		if fn == "run" && class == "Repo" && sinkKind == "sql" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a TAINTED sql finding in Repo.run with class_context, got %+v", rows)
	}
}

// parseJSDataflowFixture writes a TypeScript fixture and parses it with the
// value-flow gate enabled.
func parseJSDataflowFixture(t *testing.T, src string) map[string]any {
	t.Helper()
	return parseJSDataflowFixtureWithOptions(t, src, Options{EmitDataflow: true})
}

func parseJSDataflowFixtureWithOptions(t *testing.T, src string, options Options) map[string]any {
	t.Helper()
	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "views.ts")
	writeTestFile(t, filePath, src)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v", err)
	}
	got, err := engine.ParsePath(repoRoot, filePath, false, options)
	if err != nil {
		t.Fatalf("ParsePath error = %v", err)
	}
	return got
}
