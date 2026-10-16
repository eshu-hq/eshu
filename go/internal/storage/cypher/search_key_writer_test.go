// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/semantic"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/infra/search"
)

// infraSearchKeyWriterMat holds one infra-label entity (TerraformVariable),
// one code entity (Function), and one row of each Terraform state family, so a
// single Write exercises every canonical statement that sets a searched field.
func infraSearchKeyWriterMat() canonical.CanonicalMaterialization {
	return canonical.CanonicalMaterialization{
		ScopeID:      "scope-7245",
		GenerationID: "gen-7245",
		RepoID:       "repo-7245",
		Repository:   &canonical.RepositoryRow{RepoID: "repo-7245", Name: "r", Path: "/repos/r"},
		Files: []canonical.FileRow{{
			Path: "/repos/r/main.tf", RelativePath: "main.tf", Name: "main.tf",
			Language: "hcl", RepoID: "repo-7245", DirPath: "/repos/r",
		}},
		Entities: []canonical.EntityRow{
			{EntityID: "tfvar-1", Label: "TerraformVariable", EntityName: "region", FilePath: "/repos/r/main.tf", RelativePath: "main.tf", StartLine: 1, EndLine: 3, Language: "hcl", RepoID: "repo-7245"},
			{EntityID: "fn-1", Label: "Function", EntityName: "handler", FilePath: "/repos/r/main.tf", RelativePath: "main.tf", StartLine: 5, EndLine: 9, Language: "hcl", RepoID: "repo-7245"},
		},
		TerraformStateResources: []canonical.TerraformStateResourceRow{{UID: "tfsr-1", Address: "aws_s3_bucket.logs", ResourceType: "aws_s3_bucket"}},
		TerraformStateModules:   []canonical.TerraformStateModuleRow{{UID: "tfsm-1", ModuleAddress: "module.app"}},
		TerraformStateOutputs:   []canonical.TerraformStateOutputRow{{UID: "tfso-1", Name: "bucket"}},
	}
}

// infraSearchKeyCanonicalModes are the three entity-statement shapes the
// canonical writer can take: the default, inline containment (NornicDB), and
// inline containment batched across files.
var infraSearchKeyCanonicalModes = map[string]func(*CanonicalNodeWriter) *CanonicalNodeWriter{
	"default": func(w *CanonicalNodeWriter) *CanonicalNodeWriter { return w },
	"inline":  func(w *CanonicalNodeWriter) *CanonicalNodeWriter { return w.WithEntityContainmentInEntityUpsert() },
	"inline_batched": func(w *CanonicalNodeWriter) *CanonicalNodeWriter {
		return w.WithEntityContainmentInEntityUpsert().WithBatchedEntityContainmentInEntityUpsert()
	},
}

// infraSearchKeyAliases names the node variable each writer statement MERGEs.
var infraSearchKeyAliases = map[string]string{
	"TerraformVariable": "n", "TerraformStateResource": "r", "TerraformModule": "m", "TerraformOutput": "o",
}

// infraSearchKeyUpserts returns the statements of calls that MERGE an infra
// search label, keyed by a stable description, plus the code-label upserts.
func infraSearchKeyUpserts(t *testing.T, calls []Statement) (infra map[string]string, code []string) {
	t.Helper()
	infra = map[string]string{}
	for _, call := range calls {
		label, _ := call.Parameters[StatementMetadataEntityLabelKey].(string)
		if call.Operation != OperationCanonicalUpsert {
			continue
		}
		switch {
		case graph.IsInfraSearchKeyLabel(label):
			if strings.Contains(call.Cypher, "MERGE ("+infraSearchKeyAliases[label]+":"+label+" {uid") {
				infra[label] = call.Cypher
			}
		case label == "Function" && strings.Contains(call.Cypher, "MERGE (n:Function {uid"):
			code = append(code, call.Cypher)
		}
	}
	return infra, code
}

// TestCanonicalWriterAppendsInfraSearchKeyOnNeo4jOnly is the writer half of
// the dialect contract for every canonical entity mode: the Neo4j dialect (and
// the default constructor) ends every infra-label upsert with the shared
// clause for its node variable, the NornicDB dialect emits the statement text
// it emitted before the key existed, and a code label is never keyed.
func TestCanonicalWriterAppendsInfraSearchKeyOnNeo4jOnly(t *testing.T) {
	t.Parallel()

	aliases := infraSearchKeyAliases
	for name, mode := range infraSearchKeyCanonicalModes {
		for _, tc := range []struct {
			name   string
			writer func(Executor) *CanonicalNodeWriter
			keyed  bool
		}{
			{"default_constructor", func(e Executor) *CanonicalNodeWriter { return NewCanonicalNodeWriter(e, 500, nil) }, true},
			{"neo4j", func(e Executor) *CanonicalNodeWriter {
				return NewCanonicalNodeWriterWithDialect(e, 500, nil, search.On)
			}, true},
			{"nornicdb", func(e Executor) *CanonicalNodeWriter {
				return NewCanonicalNodeWriterWithDialect(e, 500, nil, search.Off)
			}, false},
		} {
			exec := &recordingExecutor{}
			if err := mode(tc.writer(exec)).Write(context.Background(), infraSearchKeyWriterMat()); err != nil {
				t.Fatalf("%s/%s Write() error = %v", name, tc.name, err)
			}
			infra, code := infraSearchKeyUpserts(t, exec.calls)
			if len(infra) != len(aliases) || len(code) == 0 {
				t.Fatalf("%s/%s captured infra upserts %v and %d code upserts, want %d infra labels and a Function upsert", name, tc.name, keysOf(infra), len(code), len(aliases))
			}
			for label, cypher := range infra {
				suffix := "\n" + search.Clause(aliases[label])
				if got := strings.HasSuffix(cypher, suffix); got != tc.keyed {
					t.Errorf("%s/%s %s upsert ends with the key clause = %v, want %v:\n%s", name, tc.name, label, got, tc.keyed, cypher)
				}
				if !tc.keyed && strings.Contains(cypher, graph.InfraSearchKeyProperty) {
					t.Errorf("%s/%s NornicDB %s upsert mentions %s", name, tc.name, label, graph.InfraSearchKeyProperty)
				}
				if tc.keyed {
					if refs, _ := graph.UnanalyzedIndexWrites(cypher); len(refs) > 0 {
						t.Errorf("%s/%s keyed %s upsert is unreadable to the index-key guard: %+v", name, tc.name, label, refs)
					}
				}
			}
			for _, cypher := range code {
				if strings.Contains(cypher, graph.InfraSearchKeyProperty) {
					t.Errorf("%s/%s Function upsert carries the infra search key:\n%s", name, tc.name, cypher)
				}
			}
		}
	}
}

// infraSearchKeyNornicDBGoldens pins, per writer statement, the SHA-256 of the
// text the NornicDB dialect emits. Each value was computed from the statement
// constant on origin/main at 261ca79b8, before #7245 (git show of
// canonical_node_cypher.go, tfstate_canonical_writer.go,
// cloud_resource_node_writer.go, and ec2_instance_node_writer.go, with %s
// filled by TerraformVariable), so a NornicDB statement that drifts from main
// fails here.
var infraSearchKeyNornicDBGoldens = map[string]string{
	"canonical/default/TerraformVariable":        "2bdec555e4dbaefa13d56bdd61cb684fa3647034207325c9e5bb583c5ac31d1d",
	"canonical/inline/TerraformVariable":         "2351870d9149971c85d75181e09f8143c5d58c8fa56a1cab17890f448deb6b72",
	"canonical/inline_batched/TerraformVariable": "ac6413409f387b180872858f56b9bdc5d7ed842f28794c27548d85df7bf42958",
	"canonical/TerraformStateResource":           "c711b09c522b7d2aa83e34aacb4606273be1ba8d5ddb58e0be28c661227a9688",
	"canonical/TerraformModule":                  "7f5541cbe0cecdc3d9f6f692d6f5f115661e0bfdce91bbb86fee48eac46d645f",
	"canonical/TerraformOutput":                  "07a15ad2d3d13e9d23200ff526f3d8831acf1507f68cb0127a68fca092df917f",
	"cloud_resource":                             "733703b08b79e1e1006830393bb69912d70c635cb19ab555d06fd1603d0e6a63",
	"ec2_instance":                               "6cf550675ec03363a2ec37fdc83c13a5d33cd6d294a0e3bd909596101ebce8c7",
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// TestNornicDBWriterStatementsMatchMain is the golden half: every writer that
// sets a searched field emits, under search.Off, the exact bytes it emitted on
// main.
func TestNornicDBWriterStatementsMatchMain(t *testing.T) {
	t.Parallel()

	got := map[string]string{}
	for name, mode := range infraSearchKeyCanonicalModes {
		exec := &recordingExecutor{}
		if err := mode(NewCanonicalNodeWriterWithDialect(exec, 500, nil, search.Off)).Write(context.Background(), infraSearchKeyWriterMat()); err != nil {
			t.Fatalf("%s Write() error = %v", name, err)
		}
		infra, _ := infraSearchKeyUpserts(t, exec.calls)
		got["canonical/"+name+"/TerraformVariable"] = sha256Hex(infra["TerraformVariable"])
		for _, label := range []string{"TerraformStateResource", "TerraformModule", "TerraformOutput"} {
			got["canonical/"+label] = sha256Hex(infra[label])
		}
	}
	cloudExec := &recordingExecutor{}
	if err := NewCloudResourceNodeWriterWithDialect(cloudExec, 500, search.Off).WriteCloudResourceNodes(context.Background(), []map[string]any{{"uid": "u1", "name": "n"}}, "reducer/aws"); err != nil {
		t.Fatalf("WriteCloudResourceNodes() error = %v", err)
	}
	got["cloud_resource"] = sha256Hex(cloudExec.calls[0].Cypher)
	ec2Exec := &recordingExecutor{}
	if err := NewEC2InstanceNodeWriterWithDialect(ec2Exec, 500, search.Off).WriteEC2InstanceNodes(context.Background(), []map[string]any{{"uid": "u1", "name": "n"}}, "reducer/aws"); err != nil {
		t.Fatalf("WriteEC2InstanceNodes() error = %v", err)
	}
	got["ec2_instance"] = sha256Hex(ec2Exec.calls[0].Cypher)

	for key, want := range infraSearchKeyNornicDBGoldens {
		if got[key] != want {
			t.Errorf("NornicDB statement %s sha256 = %s, want %s (main)", key, got[key], want)
		}
	}
}

// TestReducerCloudWritersAppendInfraSearchKeyOnNeo4jOnly covers the two
// reducer-owned CloudResource writers.
func TestReducerCloudWritersAppendInfraSearchKeyOnNeo4jOnly(t *testing.T) {
	t.Parallel()

	suffix := "\n" + search.Clause("r")
	rows := []map[string]any{{"uid": "u1", "name": "n"}}
	for _, tc := range []struct {
		name  string
		write func(Executor) error
		keyed bool
	}{
		{"cloud_default", func(e Executor) error {
			return NewCloudResourceNodeWriter(e, 0).WriteCloudResourceNodes(context.Background(), rows, "x")
		}, true},
		{"cloud_neo4j", func(e Executor) error {
			return NewCloudResourceNodeWriterWithDialect(e, 0, search.On).WriteCloudResourceNodes(context.Background(), rows, "x")
		}, true},
		{"cloud_nornicdb", func(e Executor) error {
			return NewCloudResourceNodeWriterWithDialect(e, 0, search.Off).WriteCloudResourceNodes(context.Background(), rows, "x")
		}, false},
		{"ec2_default", func(e Executor) error {
			return NewEC2InstanceNodeWriter(e, 0).WriteEC2InstanceNodes(context.Background(), rows, "x")
		}, true},
		{"ec2_neo4j", func(e Executor) error {
			return NewEC2InstanceNodeWriterWithDialect(e, 0, search.On).WriteEC2InstanceNodes(context.Background(), rows, "x")
		}, true},
		{"ec2_nornicdb", func(e Executor) error {
			return NewEC2InstanceNodeWriterWithDialect(e, 0, search.Off).WriteEC2InstanceNodes(context.Background(), rows, "x")
		}, false},
	} {
		exec := &recordingExecutor{}
		if err := tc.write(exec); err != nil {
			t.Fatalf("%s error = %v", tc.name, err)
		}
		if len(exec.calls) != 1 {
			t.Fatalf("%s executed %d statements, want 1", tc.name, len(exec.calls))
		}
		if got := strings.HasSuffix(exec.calls[0].Cypher, suffix); got != tc.keyed {
			t.Errorf("%s ends with the key clause = %v, want %v", tc.name, got, tc.keyed)
		}
		if _, ok := exec.calls[0].Parameters["infra_search_key"]; ok {
			t.Errorf("%s passes the key as a parameter; it must be computed in Cypher", tc.name)
		}
	}
}

// TestWithDialectConstructorsRejectInvalidDialect proves a wiring that passes
// the zero dialect fails at construction instead of writing.
func TestWithDialectConstructorsRejectInvalidDialect(t *testing.T) {
	t.Parallel()

	for name, build := range map[string]func(){
		"canonical": func() { NewCanonicalNodeWriterWithDialect(&recordingExecutor{}, 0, nil, 0) },
		"cloud":     func() { NewCloudResourceNodeWriterWithDialect(&recordingExecutor{}, 0, 0) },
		"ec2":       func() { NewEC2InstanceNodeWriterWithDialect(&recordingExecutor{}, 0, 0) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s WithDialect constructor accepted the zero dialect", name)
				}
			}()
			build()
		}()
	}
}

// TestSemanticEntityWriterNeverTargetsInfraSearchLabels pins the census
// exemption for the semantic entity writer: its labels (the closed plan list
// and the single-row filter) never include an infra search label, so its
// SET n += properties never writes a searched field the key must follow.
func TestSemanticEntityWriterNeverTargetsInfraSearchLabels(t *testing.T) {
	t.Parallel()

	for _, plan := range semanticEntityPlans() {
		if graph.IsInfraSearchKeyLabel(plan.label) {
			t.Errorf("semantic entity plan writes infra search label %s without the key clause", plan.label)
		}
	}
	for _, label := range graph.InfraSearchKeyLabels() {
		row := semantic.EntityRow{
			RepoID: "repo", EntityID: "entity", EntityName: "name", FilePath: "/f", RelativePath: "f",
			EntityType: label, StartLine: 1, EndLine: 1,
		}
		if _, ok := buildSemanticEntityRowMap(row); ok {
			t.Errorf("semantic single-row writer accepts infra search label %s without the key clause", label)
		}
	}
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
