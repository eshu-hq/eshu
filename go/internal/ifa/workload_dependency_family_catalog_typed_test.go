// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	codegraphv1 "github.com/eshu-hq/eshu/sdk/go/factschema/codegraph/v1"
)

var (
	workloadDependencyTypedRepositoryBuilder func(codegraphv1.Repository) facts.Envelope = workloadDependencyFamilyRepositoryFact
	workloadDependencyTypedFileBuilder       func(codegraphv1.File) facts.Envelope       = workloadDependencyFamilyK8sDeploymentFact
)

func TestWorkloadDependencyFamilyBuildersPreserveTypedPayloads(t *testing.T) {
	t.Parallel()

	graphID := "repo-typed-workload"
	name := "typed-workload"
	repoSlug := "ifa-org/typed-workload"
	sourceRunID := "run-typed-workload"
	repository := codegraphv1.Repository{
		RepoID: graphID, GraphID: &graphID, Name: &name,
		RepoSlug: &repoSlug, SourceRunID: &sourceRunID,
	}
	repositoryFact := workloadDependencyTypedRepositoryBuilder(repository)
	decodedRepository, err := factschema.DecodeCodegraphRepository(factschema.Envelope{
		SchemaVersion: repositoryFact.SchemaVersion,
		Payload:       repositoryFact.Payload,
	})
	if err != nil {
		t.Fatalf("DecodeCodegraphRepository(catalog payload): %v", err)
	}
	if !reflect.DeepEqual(decodedRepository, repository) {
		t.Fatalf("decoded repository = %#v, want %#v", decodedRepository, repository)
	}

	file := codegraphv1.File{
		RepoID:       repository.RepoID,
		RelativePath: "deploy/deployment.yaml",
		ParsedFileData: map[string]any{
			"k8s_resources": []any{map[string]any{
				"name": "typed-workload", "kind": "Deployment", "namespace": "production",
			}},
		},
	}
	fileFact := workloadDependencyTypedFileBuilder(file)
	decodedFile, err := factschema.DecodeCodegraphFile(factschema.Envelope{
		SchemaVersion: fileFact.SchemaVersion,
		Payload:       fileFact.Payload,
	})
	if err != nil {
		t.Fatalf("DecodeCodegraphFile(catalog payload): %v", err)
	}
	if !reflect.DeepEqual(decodedFile, file) {
		t.Fatalf("decoded file = %#v, want %#v", decodedFile, file)
	}
}

func TestWorkloadDependencyFamilyCodegraphPayloadsRoundTripExactly(t *testing.T) {
	t.Parallel()

	counts := map[string]int{}
	for _, fact := range workloadDependencyFamilyOdu().Odu.Facts {
		var (
			reencoded map[string]any
			err       error
		)
		switch fact.FactKind {
		case factschema.FactKindCodegraphRepository:
			var repository codegraphv1.Repository
			repository, err = factschema.DecodeCodegraphRepository(factschema.Envelope{SchemaVersion: fact.SchemaVersion, Payload: fact.Payload})
			if err == nil {
				reencoded, err = factschema.EncodeCodegraphRepository(repository)
			}
		case factschema.FactKindCodegraphFile:
			var file codegraphv1.File
			file, err = factschema.DecodeCodegraphFile(factschema.Envelope{SchemaVersion: fact.SchemaVersion, Payload: fact.Payload})
			if err == nil {
				reencoded, err = factschema.EncodeCodegraphFile(file)
			}
		default:
			continue
		}
		if err != nil {
			t.Fatalf("typed round trip for %s %q: %v", fact.FactKind, fact.StableFactKey, err)
		}
		if !reflect.DeepEqual(reencoded, fact.Payload) {
			t.Errorf("typed round trip changed %s %q\noriginal: %#v\nreencoded: %#v", fact.FactKind, fact.StableFactKey, fact.Payload, reencoded)
		}
		counts[fact.FactKind]++
	}
	if counts[factschema.FactKindCodegraphRepository] != 6 || counts[factschema.FactKindCodegraphFile] != 4 {
		t.Fatalf("typed fact counts = repository:%d file:%d, want repository:6 file:4", counts[factschema.FactKindCodegraphRepository], counts[factschema.FactKindCodegraphFile])
	}
}

func TestWorkloadDependencyFamilyUsesCodegraphEncoders(t *testing.T) {
	t.Parallel()

	path := filepath.Join(repoRootDir(t), "go", "internal", "ifa", "workload_dependency_family_catalog.go")
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	want := map[string]int{"EncodeCodegraphRepository": 1, "EncodeCodegraphFile": 1}
	got := map[string]int{}
	ast.Inspect(parsed, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		packageIdent, ok := selector.X.(*ast.Ident)
		if ok && packageIdent.Name == "factschema" {
			if _, tracked := want[selector.Sel.Name]; tracked {
				got[selector.Sel.Name]++
			}
		}
		return true
	})
	for encoder, count := range want {
		if got[encoder] != count {
			t.Errorf("factschema.%s calls = %d, want %d", encoder, got[encoder], count)
		}
	}
}

func TestWorkloadDependencyFamilyFileBuilderFailsClosedOnEncodeError(t *testing.T) {
	t.Parallel()

	recovered := func() (value any) {
		defer func() { value = recover() }()
		workloadDependencyTypedFileBuilder(codegraphv1.File{
			RepoID:         "repo-unencodable",
			RelativePath:   "deploy/unencodable.yaml",
			ParsedFileData: map[string]any{"unencodable": make(chan struct{})},
		})
		return nil
	}()
	message, ok := recovered.(string)
	if !ok || !strings.Contains(message, "encode workload-dependency catalog file") {
		t.Fatalf("panic = %#v, want contextual catalog encode failure", recovered)
	}
}
