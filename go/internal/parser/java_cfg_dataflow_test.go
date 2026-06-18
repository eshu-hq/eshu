package parser

import (
	"path/filepath"
	"reflect"
	"testing"
)

const javaDataflowFixture = `import java.sql.Statement;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.RequestParam;

class SearchController {
  @GetMapping("/search")
  String search(@RequestParam String q, Statement stmt) throws Exception {
    String sql = "select * from users where name = " + q;
    stmt.executeQuery(sql);
    return "ok";
  }
}
`

func TestJavaDataflowOffIsByteIdentical(t *testing.T) {
	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "SearchController.java")
	writeTestFile(t, filePath, javaDataflowFixture)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v", err)
	}

	off, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath (off) error = %v", err)
	}
	for _, bucket := range []string{
		"dataflow_catalog_versions",
		"dataflow_functions",
		"taint_findings",
		"interproc_findings",
		"dataflow_summaries",
		"dataflow_sources",
	} {
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

	delete(on, "dataflow_catalog_versions")
	delete(on, "dataflow_functions")
	delete(on, "taint_findings")
	delete(on, "interproc_findings")
	delete(on, "dataflow_summaries")
	delete(on, "dataflow_sources")
	if !reflect.DeepEqual(off, on) {
		t.Fatalf("enabling Java dataflow changed more than opt-in buckets")
	}
}

func TestJavaTaintSpringRequestParamToJDBCSink(t *testing.T) {
	got := parseJavaDataflowFixture(t, javaDataflowFixture)
	rows, ok := got["taint_findings"].([]map[string]any)
	if !ok {
		t.Fatalf("taint_findings bucket missing or wrong type: %T", got["taint_findings"])
	}
	if !hasJavaTaintFinding(rows, "search", "SearchController", "TAINTED", "sql") {
		t.Fatalf("expected a TAINTED sql finding in SearchController.search, got %+v", rows)
	}
}

func TestJavaTaintWildcardImportsToJDBCSink(t *testing.T) {
	src := `import java.sql.*;
import org.springframework.web.bind.annotation.*;

class SearchController {
  String search(@RequestParam String q, Statement stmt) throws Exception {
    String sql = "select " + q;
    stmt.executeQuery(sql);
    return "ok";
  }
}
`
	got := parseJavaDataflowFixture(t, src)
	rows, ok := got["taint_findings"].([]map[string]any)
	if !ok {
		t.Fatalf("taint_findings bucket missing or wrong type: %T", got["taint_findings"])
	}
	if !hasJavaTaintFinding(rows, "search", "SearchController", "TAINTED", "sql") {
		t.Fatalf("expected a TAINTED sql finding with wildcard imports, got %+v", rows)
	}
}

func TestJavaTaintTryBlockJDBCSink(t *testing.T) {
	src := `import java.sql.Statement;
import org.springframework.web.bind.annotation.RequestParam;

class SearchController {
  String search(@RequestParam String q, Statement stmt) throws Exception {
    String sql = "select " + q;
    try {
      stmt.executeQuery(sql);
    } catch (Exception ex) {
      throw ex;
    }
    return "ok";
  }
}
`
	got := parseJavaDataflowFixture(t, src)
	rows, ok := got["taint_findings"].([]map[string]any)
	if !ok {
		t.Fatalf("taint_findings bucket missing or wrong type: %T", got["taint_findings"])
	}
	if !hasJavaTaintFinding(rows, "search", "SearchController", "TAINTED", "sql") {
		t.Fatalf("expected a TAINTED sql finding inside try body, got %+v", rows)
	}
}

func TestJavaTaintIgnoresSameNamedLocalAnnotationAndSink(t *testing.T) {
	src := `import org.springframework.web.bind.annotation.RequestParam;

class Statement {
  void executeQuery(String value) {}
}

class LocalController {
  String search(@RequestParam String q, Statement stmt) {
    String sql = "select " + q;
    stmt.executeQuery(sql);
    return "ok";
  }
}
`
	got := parseJavaDataflowFixture(t, src)
	if rows, ok := got["taint_findings"].([]map[string]any); ok && len(rows) > 0 {
		t.Fatalf("expected no taint findings for local RequestParam/Statement, got %+v", rows)
	}
}

func TestJavaInterprocSummariesAndSources(t *testing.T) {
	src := `package app;

import java.sql.Statement;
import org.springframework.web.bind.annotation.RequestParam;

class SearchController {
  String search(@RequestParam String q, Statement stmt) throws Exception {
    run(q, stmt);
    return "ok";
  }

  void run(String query, Statement stmt) throws Exception {
    stmt.executeQuery(query);
  }
}
`
	got := parseJavaDataflowFixtureWithOptions(t, src, Options{EmitDataflow: true, RepositoryID: "repo-alpha"})

	summaryRows, ok := got["dataflow_summaries"].([]map[string]any)
	if !ok {
		t.Fatalf("dataflow_summaries bucket missing or wrong type: %T", got["dataflow_summaries"])
	}
	if !hasJavaSummaryParamToSink(summaryRows, "repo-alpha\u001fapp\u001fSearchController\u001frun(String,Statement)", 0, "sql") {
		t.Fatalf("expected run(String,Statement) param 0 to sql summary, got %+v", summaryRows)
	}
	if !hasJavaSummaryCallArg(summaryRows, "repo-alpha\u001fapp\u001fSearchController\u001fsearch(String,Statement)", "repo-alpha\u001fapp\u001fSearchController\u001frun(String,Statement)", 0, 0) {
		t.Fatalf("expected search(String,Statement) param 0 to run arg 0 summary, got %+v", summaryRows)
	}

	sourceRows, ok := got["dataflow_sources"].([]map[string]any)
	if !ok {
		t.Fatalf("dataflow_sources bucket missing or wrong type: %T", got["dataflow_sources"])
	}
	if !hasJavaSourceRow(sourceRows, "repo-alpha\u001fapp\u001fSearchController\u001fsearch(String,Statement)", 0, "http_request") {
		t.Fatalf("expected Spring param source row for search param 0, got %+v", sourceRows)
	}

	interprocRows, ok := got["interproc_findings"].([]map[string]any)
	if !ok {
		t.Fatalf("interproc_findings bucket missing or wrong type: %T", got["interproc_findings"])
	}
	if !hasJavaInterprocFinding(interprocRows, "repo-alpha\u001fapp\u001fSearchController\u001fsearch(String,Statement)", "repo-alpha\u001fapp\u001fSearchController\u001frun(String,Statement)", "sql") {
		t.Fatalf("expected cross-method Java sql finding, got %+v", interprocRows)
	}
}

func TestJavaDurableRowsRequirePackageIdentity(t *testing.T) {
	got := parseJavaDataflowFixtureWithOptions(t, javaDataflowFixture, Options{EmitDataflow: true, RepositoryID: "repo-alpha"})
	if _, present := got["dataflow_summaries"]; present {
		t.Fatalf("dataflow_summaries emitted without Java package identity: %+v", got["dataflow_summaries"])
	}
	if _, present := got["dataflow_sources"]; present {
		t.Fatalf("dataflow_sources emitted without Java package identity: %+v", got["dataflow_sources"])
	}
}

func parseJavaDataflowFixture(t *testing.T, src string) map[string]any {
	t.Helper()
	return parseJavaDataflowFixtureWithOptions(t, src, Options{EmitDataflow: true})
}

func parseJavaDataflowFixtureWithOptions(t *testing.T, src string, options Options) map[string]any {
	t.Helper()
	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "SearchController.java")
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

func hasJavaTaintFinding(rows []map[string]any, function, classContext, kind, sinkKind string) bool {
	for _, row := range rows {
		fn, _ := row["function_name"].(string)
		class, _ := row["class_context"].(string)
		k, _ := row["kind"].(string)
		sk, _ := row["sink_kind"].(string)
		if fn == function && class == classContext && k == kind && sk == sinkKind {
			return true
		}
	}
	return false
}

func hasJavaSummaryParamToSink(rows []map[string]any, functionID string, param int, sinkKind string) bool {
	for _, row := range rows {
		id, _ := row["function_id"].(string)
		if id != functionID {
			continue
		}
		sinks, _ := row["param_to_sink"].([]map[string]any)
		for _, sink := range sinks {
			p, _ := sink["param"].(int)
			sk, _ := sink["sink_kind"].(string)
			if p == param && sk == sinkKind {
				return true
			}
		}
	}
	return false
}

func hasJavaSummaryCallArg(rows []map[string]any, functionID string, callee string, param int, arg int) bool {
	for _, row := range rows {
		id, _ := row["function_id"].(string)
		if id != functionID {
			continue
		}
		calls, _ := row["param_to_call_arg"].([]map[string]any)
		for _, call := range calls {
			c, _ := call["callee"].(string)
			p, _ := call["param"].(int)
			a, _ := call["arg"].(int)
			if c == callee && p == param && a == arg {
				return true
			}
		}
	}
	return false
}

func hasJavaSourceRow(rows []map[string]any, functionID string, param int, kind string) bool {
	for _, row := range rows {
		id, _ := row["function_id"].(string)
		p, _ := row["param_index"].(int)
		k, _ := row["kind"].(string)
		if id == functionID && p == param && k == kind {
			return true
		}
	}
	return false
}

func hasJavaInterprocFinding(rows []map[string]any, sourceFunc string, sinkFunc string, sinkKind string) bool {
	for _, row := range rows {
		src, _ := row["source_func"].(string)
		sink, _ := row["sink_func"].(string)
		kind, _ := row["sink_kind"].(string)
		if src == sourceFunc && sink == sinkFunc && kind == sinkKind {
			return true
		}
	}
	return false
}
