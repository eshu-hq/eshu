// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package golang_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

func TestDefaultEngineParsePathGoMarksPackageImportedInterfaceParameterImplementations(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "wiring.go")
	calleePath := filepath.Join(repoRoot, "nornicdb_wiring.go")
	writeGoFixture(
		t,
		calleePath,
		`package roots

func bootstrapCanonicalExecutorForGraphBackend(rawExecutor sourcecypher.Executor) sourcecypher.Executor {
	return rawExecutor
}
`,
	)
	writeGoFixture(
		t,
		callerPath,
		`package roots

type bootstrapNeo4jExecutor struct{}

func (e bootstrapNeo4jExecutor) Execute() error { return nil }
func (e bootstrapNeo4jExecutor) ExecuteGroup() error { return nil }
func (e bootstrapNeo4jExecutor) unused() {}

func wireAPI() {
	rawExecutor := bootstrapNeo4jExecutor{}
	_ = bootstrapCanonicalExecutorForGraphBackend(rawExecutor)
}
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	packageTargets, err := engine.PreScanGoPackageSemanticRoots(
		repoRoot,
		[]string{callerPath, calleePath},
	)
	if err != nil {
		t.Fatalf("PreScanGoPackageSemanticRoots() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, callerPath, false, parser.Options{
		GoImportedInterfaceParamMethods: packageTargets[repoRoot].ImportedInterfaceParamMethods,
	})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	parsertest.AssertStringSliceContains(t, parsertest.AssertBucketItemByName(t, got, "structs", "bootstrapNeo4jExecutor"), "dead_code_root_kinds", "go.interface_implementation_type")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "Execute", "bootstrapNeo4jExecutor"), "dead_code_root_kinds", "go.interface_method_implementation")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "ExecuteGroup", "bootstrapNeo4jExecutor"), "dead_code_root_kinds", "go.interface_method_implementation")
	if _, ok := parsertest.AssertFunctionByNameAndClass(t, got, "unused", "bootstrapNeo4jExecutor")["dead_code_root_kinds"]; ok {
		t.Fatalf("bootstrapNeo4jExecutor.unused dead_code_root_kinds present, want absent outside the imported interface contract")
	}
}

func TestDefaultEngineParsePathGoMarksSameRepoImportedPackageInterfaceEscapes(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeGoFixture(
		t,
		filepath.Join(repoRoot, "go.mod"),
		`module example.com/runtime

go 1.24
`,
	)
	boltPath := filepath.Join(repoRoot, "pkg", "bolt", "server.go")
	writeGoFixture(
		t,
		boltPath,
		`package bolt

import "context"

type QueryResult struct{}
type Config struct{}
type DatabaseManager interface{}

type QueryExecutor interface {
	Execute(context.Context, string, map[string]any) (*QueryResult, error)
}

type SessionExecutorFactory interface {
	NewSessionExecutor() QueryExecutor
}

type TransactionalExecutor interface {
	QueryExecutor
	BeginTransaction(context.Context, map[string]any) error
	CommitTransaction(context.Context) error
}

func NewWithDatabaseManager(config *Config, executor QueryExecutor, dbManager DatabaseManager) *Server {
	return &Server{executor: executor}
}

type Server struct {
	executor QueryExecutor
}

func (s *Server) openSession() {
	if factory, ok := s.executor.(SessionExecutorFactory); ok {
		s.executor = factory.NewSessionExecutor()
	}
}

func (s *Server) begin(ctx context.Context) error {
	if tx, ok := s.executor.(TransactionalExecutor); ok {
		return tx.BeginTransaction(ctx, nil)
	}
	return nil
}
`,
	)
	mainPath := filepath.Join(repoRoot, "cmd", "nornicdb", "main.go")
	writeGoFixture(
		t,
		mainPath,
		`package main

import (
	"context"

	"example.com/runtime/pkg/bolt"
)

type DBQueryExecutor struct{}

func NewDBQueryExecutor() *DBQueryExecutor { return &DBQueryExecutor{} }
func (e *DBQueryExecutor) NewSessionExecutor() bolt.QueryExecutor { return NewDBQueryExecutor() }
func (e *DBQueryExecutor) Execute(context.Context, string, map[string]any) (*bolt.QueryResult, error) {
	return nil, nil
}
func (e *DBQueryExecutor) BeginTransaction(context.Context, map[string]any) error { return nil }
func (e *DBQueryExecutor) CommitTransaction(context.Context) error { return nil }
func (e *DBQueryExecutor) ExportedButNotInInterface() {}
func (e *DBQueryExecutor) helper() {}

func run(dbManager bolt.DatabaseManager) {
	queryExecutor := NewDBQueryExecutor()
	_ = bolt.NewWithDatabaseManager(nil, queryExecutor, dbManager)
}
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	packageTargets, err := engine.PreScanGoPackageSemanticRoots(
		repoRoot,
		[]string{boltPath, mainPath},
	)
	if err != nil {
		t.Fatalf("PreScanGoPackageSemanticRoots() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, mainPath, false, parser.Options{
		GoImportedInterfaceParamMethods: packageTargets[filepath.Dir(mainPath)].ImportedInterfaceParamMethods,
	})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	parsertest.AssertStringSliceContains(t, parsertest.AssertBucketItemByName(t, got, "structs", "DBQueryExecutor"), "dead_code_root_kinds", "go.interface_implementation_type")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "Execute", "DBQueryExecutor"), "dead_code_root_kinds", "go.interface_method_implementation")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "NewSessionExecutor", "DBQueryExecutor"), "dead_code_root_kinds", "go.interface_method_implementation")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "BeginTransaction", "DBQueryExecutor"), "dead_code_root_kinds", "go.interface_method_implementation")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "CommitTransaction", "DBQueryExecutor"), "dead_code_root_kinds", "go.interface_method_implementation")
	if _, ok := parsertest.AssertFunctionByNameAndClass(t, got, "ExportedButNotInInterface", "DBQueryExecutor")["dead_code_root_kinds"]; ok {
		t.Fatalf("DBQueryExecutor.ExportedButNotInInterface dead_code_root_kinds present, want absent outside package interface contracts")
	}
	if _, ok := parsertest.AssertFunctionByNameAndClass(t, got, "helper", "DBQueryExecutor")["dead_code_root_kinds"]; ok {
		t.Fatalf("DBQueryExecutor.helper dead_code_root_kinds present, want absent for unexported method")
	}
}
