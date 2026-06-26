// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"testing"
)

func TestCSharpTaintFromBody(t *testing.T) {
	src := `using Microsoft.AspNetCore.Mvc;
using System.Data.SqlClient;

namespace Demo {
  public class BodyController : ControllerBase {
    public string Create([FromBody] string payload, SqlCommand cmd) {
      cmd.ExecuteReader(payload);
      return "ok";
    }
  }
}
`
	got := parseCSharpDataflowFixture(t, src)
	rows, ok := got["taint_findings"].([]map[string]any)
	if !ok {
		t.Fatalf("taint_findings bucket missing or wrong type: %T", got["taint_findings"])
	}
	if !hasCSharpTaintFinding(rows, "Create", "BodyController", "TAINTED", "sql") {
		t.Fatalf("expected a TAINTED sql finding in BodyController.Create, got %+v", rows)
	}
	if len(rows) != 1 {
		t.Fatalf("expected exactly one taint finding, got %d: %+v", len(rows), rows)
	}
}

func TestCSharpTaintFromRoute(t *testing.T) {
	src := `using Microsoft.AspNetCore.Mvc;
using System.Data.SqlClient;

namespace Demo {
  public class RouteController : ControllerBase {
    public string Get([FromRoute] string id, SqlCommand cmd) {
      cmd.ExecuteReader(id);
      return "ok";
    }
  }
}
`
	got := parseCSharpDataflowFixture(t, src)
	rows, ok := got["taint_findings"].([]map[string]any)
	if !ok {
		t.Fatalf("taint_findings bucket missing or wrong type: %T", got["taint_findings"])
	}
	if !hasCSharpTaintFinding(rows, "Get", "RouteController", "TAINTED", "sql") {
		t.Fatalf("expected a TAINTED sql finding in RouteController.Get, got %+v", rows)
	}
	if len(rows) != 1 {
		t.Fatalf("expected exactly one taint finding, got %d: %+v", len(rows), rows)
	}
}

func TestCSharpTaintFromForm(t *testing.T) {
	src := `using Microsoft.AspNetCore.Mvc;
using System.Data.SqlClient;

namespace Demo {
  public class FormController : ControllerBase {
    public string Post([FromForm] string name, SqlCommand cmd) {
      cmd.ExecuteReader(name);
      return "ok";
    }
  }
}
`
	got := parseCSharpDataflowFixture(t, src)
	rows, ok := got["taint_findings"].([]map[string]any)
	if !ok {
		t.Fatalf("taint_findings bucket missing or wrong type: %T", got["taint_findings"])
	}
	if !hasCSharpTaintFinding(rows, "Post", "FormController", "TAINTED", "sql") {
		t.Fatalf("expected a TAINTED sql finding in FormController.Post, got %+v", rows)
	}
	if len(rows) != 1 {
		t.Fatalf("expected exactly one taint finding, got %d: %+v", len(rows), rows)
	}
}

func TestCSharpTaintExecuteNonQuery(t *testing.T) {
	src := `using Microsoft.AspNetCore.Mvc;
using System.Data.SqlClient;

namespace Demo {
  public class SearchController : ControllerBase {
    public string Search([FromQuery] string q, SqlCommand cmd) {
      cmd.ExecuteNonQuery(q);
      return "ok";
    }
  }
}
`
	got := parseCSharpDataflowFixture(t, src)
	rows, ok := got["taint_findings"].([]map[string]any)
	if !ok {
		t.Fatalf("taint_findings bucket missing or wrong type: %T", got["taint_findings"])
	}
	if !hasCSharpTaintFinding(rows, "Search", "SearchController", "TAINTED", "sql") {
		t.Fatalf("expected a TAINTED sql finding via ExecuteNonQuery in SearchController.Search, got %+v", rows)
	}
	if len(rows) != 1 {
		t.Fatalf("expected exactly one taint finding, got %d: %+v", len(rows), rows)
	}
}

func TestCSharpTaintExecuteScalar(t *testing.T) {
	src := `using Microsoft.AspNetCore.Mvc;
using System.Data.SqlClient;

namespace Demo {
  public class SearchController : ControllerBase {
    public string Search([FromQuery] string q, SqlCommand cmd) {
      cmd.ExecuteScalar(q);
      return "ok";
    }
  }
}
`
	got := parseCSharpDataflowFixture(t, src)
	rows, ok := got["taint_findings"].([]map[string]any)
	if !ok {
		t.Fatalf("taint_findings bucket missing or wrong type: %T", got["taint_findings"])
	}
	if !hasCSharpTaintFinding(rows, "Search", "SearchController", "TAINTED", "sql") {
		t.Fatalf("expected a TAINTED sql finding via ExecuteScalar in SearchController.Search, got %+v", rows)
	}
	if len(rows) != 1 {
		t.Fatalf("expected exactly one taint finding, got %d: %+v", len(rows), rows)
	}
}

func TestCSharpTaintProcessStart(t *testing.T) {
	src := `using Microsoft.AspNetCore.Mvc;
using System.Diagnostics;

namespace Demo {
  public class ProcessController : ControllerBase {
    public string Run([FromQuery] string input) {
      Process proc = new Process();
      proc.Start(input);
      return "ok";
    }
  }
}
`
	got := parseCSharpDataflowFixture(t, src)
	rows, ok := got["taint_findings"].([]map[string]any)
	if !ok {
		t.Fatalf("taint_findings bucket missing or wrong type: %T", got["taint_findings"])
	}
	if !hasCSharpTaintFinding(rows, "Run", "ProcessController", "TAINTED", "command_injection") {
		t.Fatalf("expected a TAINTED command_injection finding in ProcessController.Run, got %+v", rows)
	}
	if len(rows) != 1 {
		t.Fatalf("expected exactly one taint finding, got %d: %+v", len(rows), rows)
	}
}

func TestCSharpTaintMicrosoftDataSqlClient(t *testing.T) {
	src := `using Microsoft.AspNetCore.Mvc;
using Microsoft.Data.SqlClient;

namespace Demo {
  public class SearchController : ControllerBase {
    public string Search([FromQuery] string q, SqlCommand cmd) {
      cmd.ExecuteReader(q);
      return "ok";
    }
  }
}
`
	got := parseCSharpDataflowFixture(t, src)
	rows, ok := got["taint_findings"].([]map[string]any)
	if !ok {
		t.Fatalf("taint_findings bucket missing or wrong type: %T", got["taint_findings"])
	}
	if !hasCSharpTaintFinding(rows, "Search", "SearchController", "TAINTED", "sql") {
		t.Fatalf("expected a TAINTED sql finding with Microsoft.Data.SqlClient in SearchController.Search, got %+v", rows)
	}
	if len(rows) != 1 {
		t.Fatalf("expected exactly one taint finding, got %d: %+v", len(rows), rows)
	}
}

func TestCSharpTaintRejectsVarLocal(t *testing.T) {
	src := `using Microsoft.AspNetCore.Mvc;
using System.Data.SqlClient;

namespace Demo {
  public class SearchController : ControllerBase {
    public string Search([FromQuery] string q) {
      var cmd = new SqlCommand();
      cmd.ExecuteReader(q);
      return "ok";
    }
  }
}
`
	got := parseCSharpDataflowFixture(t, src)
	rows, _ := got["taint_findings"].([]map[string]any)
	if len(rows) > 0 {
		t.Fatalf("expected no taint findings for var-typed SqlCommand, got %+v", rows)
	}
}

func TestCSharpDeadCodeRootTestAttributes(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "TestAttributes.cs")
	writeTestFile(t, sourcePath, `using Xunit;
using NUnit.Framework;
using Microsoft.VisualStudio.TestTools.UnitTesting;

public sealed class TestClass {
    [Fact]
    public void FactMethod() {}

    [Theory]
    public void TheoryMethod() {}

    [Test]
    public void NUnitTestMethod() {}

    [TestMethod]
    public void MSTestMethod() {}

    [SetUp]
    public void NUnitSetUp() {}

    [TearDown]
    public void NUnitTearDown() {}

    [OneTimeSetUp]
    public void NUnitOneTimeSetUp() {}

    [OneTimeTearDown]
    public void NUnitOneTimeTearDown() {}
}
`)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath error = %v", err)
	}

	for _, tc := range []struct{ name, class string }{
		{"FactMethod", "TestClass"},
		{"TheoryMethod", "TestClass"},
		{"NUnitTestMethod", "TestClass"},
		{"MSTestMethod", "TestClass"},
		{"NUnitSetUp", "TestClass"},
		{"NUnitTearDown", "TestClass"},
		{"NUnitOneTimeSetUp", "TestClass"},
		{"NUnitOneTimeTearDown", "TestClass"},
	} {
		assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, tc.name, tc.class), "dead_code_root_kinds", "csharp.test_method")
	}
}

func TestCSharpDeadCodeRootSerializationCallbacks(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "SerializationCallbacks.cs")
	writeTestFile(t, sourcePath, `using System.Runtime.Serialization;

public sealed class SerializedState {
    [OnSerializing]
    private void OnSerializingCallback(StreamingContext context) {}

    [OnSerialized]
    private void OnSerializedCallback(StreamingContext context) {}

    [OnDeserializing]
    private void OnDeserializingCallback(StreamingContext context) {}

    [OnDeserialized]
    private void OnDeserializedCallback(StreamingContext context) {}
}
`)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath error = %v", err)
	}

	for _, tc := range []struct{ name, class string }{
		{"OnSerializingCallback", "SerializedState"},
		{"OnSerializedCallback", "SerializedState"},
		{"OnDeserializingCallback", "SerializedState"},
		{"OnDeserializedCallback", "SerializedState"},
	} {
		assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, tc.name, tc.class), "dead_code_root_kinds", "csharp.serialization_callback")
	}
}
