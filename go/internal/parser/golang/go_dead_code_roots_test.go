// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package golang_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

func TestDefaultEngineParsePathGoEmitsDeadCodeRootKinds(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "roots.go")
	parsertest.WriteFile(
		t,
		filePath,
		`package roots

import (
	ctxalias "context"
	rootcobra "github.com/spf13/cobra"
	handler "net/http"
	ctrl "sigs.k8s.io/controller-runtime"
)

func ServePayments(w handler.ResponseWriter, r *handler.Request) {}

func runPayments(cmd *rootcobra.Command, args []string) error {
	return nil
}

type PaymentReconciler struct{}

func (r *PaymentReconciler) Reconcile(ctx ctxalias.Context, req ctrl.Request) (ctrl.Result, error) {
	return ctrl.Result{}, nil
}
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	parsertest.AssertStringSliceEquals(t, parsertest.AssertBucketItemByName(t, got, "functions", "ServePayments"), "dead_code_root_kinds", []string{"go.net_http_handler_signature"})
	parsertest.AssertStringSliceEquals(t, parsertest.AssertBucketItemByName(t, got, "functions", "runPayments"), "dead_code_root_kinds", []string{"go.cobra_run_signature"})
	parsertest.AssertStringSliceEquals(t, parsertest.AssertBucketItemByName(t, got, "functions", "Reconcile"), "dead_code_root_kinds", []string{"go.controller_runtime_reconcile_signature"})
}

func TestDefaultEngineParsePathGoDoesNotMarkValueRequestAsHTTPHandlerRoot(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "value_request.go")
	parsertest.WriteFile(
		t,
		filePath,
		`package roots

import handler "net/http"

func ServePayments(w handler.ResponseWriter, r handler.Request) {}
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	functionItem := parsertest.AssertBucketItemByName(t, got, "functions", "ServePayments")
	if _, ok := functionItem["dead_code_root_kinds"]; ok {
		t.Fatalf("dead_code_root_kinds = %#v, want absent for value request signature", functionItem["dead_code_root_kinds"])
	}
}
