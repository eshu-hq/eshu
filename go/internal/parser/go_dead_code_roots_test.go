package parser

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestDefaultEngineParsePathGoEmitsDeadCodeRootKinds(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "roots.go")
	writeTestFile(
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

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertParserStringSliceFieldValue(t, assertFunctionByName(t, got, "ServePayments"), "dead_code_root_kinds", []string{"go.net_http_handler_signature"})
	assertParserStringSliceFieldValue(t, assertFunctionByName(t, got, "runPayments"), "dead_code_root_kinds", []string{"go.cobra_run_signature"})
	assertParserStringSliceFieldValue(t, assertFunctionByName(t, got, "Reconcile"), "dead_code_root_kinds", []string{"go.controller_runtime_reconcile_signature"})
}

func TestDefaultEngineParsePathGoDoesNotMarkValueRequestAsHTTPHandlerRoot(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "value_request.go")
	writeTestFile(
		t,
		filePath,
		`package roots

import handler "net/http"

func ServePayments(w handler.ResponseWriter, r handler.Request) {}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	functionItem := assertFunctionByName(t, got, "ServePayments")
	if _, ok := functionItem["dead_code_root_kinds"]; ok {
		t.Fatalf("dead_code_root_kinds = %#v, want absent for value request signature", functionItem["dead_code_root_kinds"])
	}
}

func assertParserStringSliceFieldValue(t *testing.T, item map[string]any, field string, want []string) {
	t.Helper()

	got, ok := item[field].([]string)
	if !ok {
		t.Fatalf("%s = %T, want []string", field, item[field])
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s = %#v, want %#v", field, got, want)
	}
}
