// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"reflect"
	"testing"
)

const csharpDataflowFixture = `using Microsoft.AspNetCore.Mvc;
using System.Data.SqlClient;

namespace Demo {
  public class SearchController : ControllerBase {
    [HttpGet]
    public string Search([FromQuery] string q, SqlCommand cmd) {
      string sql = "select * from users where name = " + q;
      cmd.ExecuteReader(sql);
      return "ok";
    }
  }
}
`

func TestCSharpDataflowOffIsByteIdentical(t *testing.T) {
	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "SearchController.cs")
	writeTestFile(t, filePath, csharpDataflowFixture)

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
		t.Fatalf("enabling C# dataflow changed more than opt-in buckets")
	}
}

func TestCSharpTaintFromQueryToADONetSink(t *testing.T) {
	got := parseCSharpDataflowFixture(t, csharpDataflowFixture)
	rows, ok := got["taint_findings"].([]map[string]any)
	if !ok {
		t.Fatalf("taint_findings bucket missing or wrong type: %T", got["taint_findings"])
	}
	if !hasCSharpTaintFinding(rows, "Search", "SearchController", "TAINTED", "sql") {
		t.Fatalf("expected a TAINTED sql finding in SearchController.Search, got %+v", rows)
	}
	if len(rows) != 1 {
		t.Fatalf("expected exactly one taint finding, got %d: %+v", len(rows), rows)
	}
}

func TestCSharpTaintIgnoresSameNamedLocalSourceAndSink(t *testing.T) {
	// No AspNetCore.Mvc using -> [FromQuery] is not a source. No ADO.NET using and
	// SqlCommand is a locally-declared class -> ExecuteReader is not a sink.
	src := `namespace Demo {
  public class SqlCommand {
    public string ExecuteReader(string value) { return value; }
  }

  public class LocalController {
    public string Search([FromQuery] string q, SqlCommand cmd) {
      string sql = "select " + q;
      cmd.ExecuteReader(sql);
      return "ok";
    }
  }
}
`
	got := parseCSharpDataflowFixture(t, src)
	if rows, ok := got["taint_findings"].([]map[string]any); ok && len(rows) > 0 {
		t.Fatalf("expected no taint findings for local FromQuery/SqlCommand, got %+v", rows)
	}
}

func TestCSharpTaintRequiresAspNetUsingForSource(t *testing.T) {
	// ADO.NET sink using present, but no AspNetCore.Mvc using -> [FromQuery] is
	// not a recognized source, so no source->sink flow.
	src := `using System.Data.SqlClient;

namespace Demo {
  public class SearchController {
    public string Search([FromQuery] string q, SqlCommand cmd) {
      string sql = "select " + q;
      cmd.ExecuteReader(sql);
      return "ok";
    }
  }
}
`
	got := parseCSharpDataflowFixture(t, src)
	if rows, ok := got["taint_findings"].([]map[string]any); ok && len(rows) > 0 {
		t.Fatalf("expected no taint findings without AspNetCore.Mvc using, got %+v", rows)
	}
}

func TestCSharpInterprocSummariesAndSources(t *testing.T) {
	src := `using Microsoft.AspNetCore.Mvc;
using System.Data.SqlClient;

namespace App {
  public class SearchController {
    public string Search([FromQuery] string q, SqlCommand cmd) {
      Run(q, cmd);
      return "ok";
    }

    public void Run(string query, SqlCommand cmd) {
      cmd.ExecuteReader(query);
    }
  }
}
`
	got := parseCSharpDataflowFixtureWithOptions(t, src, Options{EmitDataflow: true, RepositoryID: "repo-alpha"})

	summaryRows, ok := got["dataflow_summaries"].([]map[string]any)
	if !ok {
		t.Fatalf("dataflow_summaries bucket missing or wrong type: %T", got["dataflow_summaries"])
	}
	if !hasCSharpSummaryParamToSink(summaryRows, "repo-alpha\x1fApp\x1fSearchController\x1fRun(string,SqlCommand)", 0, "sql") {
		t.Fatalf("expected Run(string,SqlCommand) param 0 to sql summary, got %+v", summaryRows)
	}

	sourceRows, ok := got["dataflow_sources"].([]map[string]any)
	if !ok {
		t.Fatalf("dataflow_sources bucket missing or wrong type: %T", got["dataflow_sources"])
	}
	if !hasCSharpSourceRow(sourceRows, "repo-alpha\x1fApp\x1fSearchController\x1fSearch(string,SqlCommand)", 0, "http_request") {
		t.Fatalf("expected FromQuery param source row for Search param 0, got %+v", sourceRows)
	}

	interprocRows, ok := got["interproc_findings"].([]map[string]any)
	if !ok {
		t.Fatalf("interproc_findings bucket missing or wrong type: %T", got["interproc_findings"])
	}
	if !hasCSharpInterprocFinding(interprocRows, "repo-alpha\x1fApp\x1fSearchController\x1fSearch(string,SqlCommand)", "repo-alpha\x1fApp\x1fSearchController\x1fRun(string,SqlCommand)", "sql") {
		t.Fatalf("expected cross-method C# sql finding, got %+v", interprocRows)
	}
}

func parseCSharpDataflowFixture(t *testing.T, src string) map[string]any {
	t.Helper()
	return parseCSharpDataflowFixtureWithOptions(t, src, Options{EmitDataflow: true})
}

func parseCSharpDataflowFixtureWithOptions(t *testing.T, src string, options Options) map[string]any {
	t.Helper()
	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "SearchController.cs")
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

func hasCSharpTaintFinding(rows []map[string]any, function, classContext, kind, sinkKind string) bool {
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

func hasCSharpSummaryParamToSink(rows []map[string]any, functionID string, param int, sinkKind string) bool {
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

func hasCSharpSourceRow(rows []map[string]any, functionID string, param int, kind string) bool {
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

func hasCSharpInterprocFinding(rows []map[string]any, sourceFunc string, sinkFunc string, sinkKind string) bool {
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
