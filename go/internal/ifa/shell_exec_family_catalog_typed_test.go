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

// These assignments are the compile-time tooth: shell-exec catalog builders
// must accept public typed codegraph payloads before converting them to maps.
var (
	shellExecFamilyTypedRepositoryBuilder func(codegraphv1.Repository) facts.Envelope = shellExecFamilyRepositoryFact
	shellExecFamilyTypedFileBuilder       func(codegraphv1.File) facts.Envelope       = shellExecFamilyFileFact
)

func TestShellExecFamilyCodegraphBuildersPreserveTypedPayloads(t *testing.T) {
	t.Parallel()

	sourceRunID := "run-typed-contract"
	localPath := "/typed-contract"
	repository := codegraphv1.Repository{
		RepoID:      "repo-typed-contract",
		SourceRunID: &sourceRunID,
		LocalPath:   &localPath,
	}
	repositoryFact := shellExecFamilyTypedRepositoryBuilder(repository)
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
		RepoID:         repository.RepoID,
		RelativePath:   "app/typed.py",
		ParsedFileData: map[string]any{"embedded_shell_commands": []any{}},
	}
	fileFact := shellExecFamilyTypedFileBuilder(file)
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

func TestShellExecFamilyUsesTypedCodegraphLiteralsAndEncoders(t *testing.T) {
	t.Parallel()

	path := filepath.Join(repoRootDir(t), "go", "internal", "ifa", "shell_exec_family_odu.go")
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	wantTypedBuilders := map[string]struct {
		typeName string
		count    int
	}{
		"shellExecFamilyRepositoryFact": {typeName: "Repository", count: 1},
		"shellExecFamilyFileFact":       {typeName: "File", count: 4},
	}
	gotTypedBuilders := map[string]int{}
	wantEncoders := map[string]int{
		"EncodeCodegraphRepository": 1,
		"EncodeCodegraphFile":       1,
	}
	gotEncoders := map[string]int{}

	ast.Inspect(parsed, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if ident, ok := call.Fun.(*ast.Ident); ok {
			want, tracked := wantTypedBuilders[ident.Name]
			if !tracked {
				return true
			}
			if len(call.Args) != 1 || !isCodegraphCompositeLiteral(call.Args[0], want.typeName) {
				t.Errorf("%s must receive one codegraphv1.%s composite literal before payload map conversion", ident.Name, want.typeName)
				return true
			}
			gotTypedBuilders[ident.Name]++
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		packageIdent, ok := selector.X.(*ast.Ident)
		if ok && packageIdent.Name == "factschema" {
			if _, tracked := wantEncoders[selector.Sel.Name]; tracked {
				gotEncoders[selector.Sel.Name]++
			}
		}
		return true
	})

	for builder, want := range wantTypedBuilders {
		if gotTypedBuilders[builder] != want.count {
			t.Errorf("%s typed literal calls = %d, want %d", builder, gotTypedBuilders[builder], want.count)
		}
	}
	for encoder, want := range wantEncoders {
		if gotEncoders[encoder] != want {
			t.Errorf("factschema.%s calls = %d, want %d", encoder, gotEncoders[encoder], want)
		}
	}
}

func TestShellExecFamilyCodegraphPayloadsDecodeAndReencodeExactly(t *testing.T) {
	t.Parallel()

	counts := map[string]int{}
	for _, fact := range shellExecFamilyOdu().Odu.Facts {
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
	if counts[factschema.FactKindCodegraphRepository] != 1 || counts[factschema.FactKindCodegraphFile] != 4 {
		t.Fatalf("typed codegraph fact counts = repository:%d file:%d, want repository:1 file:4", counts[factschema.FactKindCodegraphRepository], counts[factschema.FactKindCodegraphFile])
	}
}

func TestShellExecFamilyFileBuilderFailsClosedOnEncodeError(t *testing.T) {
	t.Parallel()

	deferred := func() (recovered any) {
		defer func() {
			recovered = recover()
		}()
		shellExecFamilyTypedFileBuilder(codegraphv1.File{
			RepoID:         "repo-unencodable",
			RelativePath:   "app/unencodable.py",
			ParsedFileData: map[string]any{"unencodable": make(chan struct{})},
		})
		return nil
	}()
	if deferred == nil {
		t.Fatal("unencodable typed file payload did not fail closed")
	}
	message, ok := deferred.(string)
	if !ok || !strings.Contains(message, "encode shell-exec catalog file") {
		t.Fatalf("panic = %#v, want contextual catalog encode failure", deferred)
	}
}
