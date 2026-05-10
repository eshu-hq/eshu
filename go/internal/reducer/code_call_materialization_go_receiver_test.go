package reducer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestExtractCodeCallRowsResolvesGoReceiverVariableCallsWithoutTreatingImportsAsLocal(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "handler.go")
	if err := os.WriteFile(callerPath, []byte(`package query

import "fmt"

type CodeHandler struct{}

func Println() {}

func (h *CodeHandler) transitiveRelationshipsGraphRow() {}

func (h *CodeHandler) handleRelationships() {
	h.transitiveRelationshipsGraphRow()
	fmt.Println("hello")
}
`), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v, want nil", callerPath, err)
	}

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	callerPayload, err := engine.ParsePath(repoRoot, callerPath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", callerPath, err)
	}
	if functions, ok := callerPayload["functions"].([]map[string]any); ok {
		for _, function := range functions {
			name, _ := function["name"].(string)
			classContext, _ := function["class_context"].(string)
			switch {
			case name == "handleRelationships":
				function["end_line"] = 12
				function["uid"] = "content-entity:go-handle-relationships"
			case name == "transitiveRelationshipsGraphRow" && classContext == "CodeHandler":
				function["uid"] = "content-entity:go-transitive-relationships-row"
			case name == "Println":
				function["uid"] = "content-entity:go-local-println"
			}
		}
	}

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-go",
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-go",
				"relative_path":    "handler.go",
				"parsed_file_data": callerPayload,
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1; rows=%#v; function_calls=%#v", len(rows), rows, callerPayload["function_calls"])
	}
	if got, want := rows[0]["caller_entity_id"], "content-entity:go-handle-relationships"; got != want {
		t.Fatalf("caller_entity_id = %#v, want %#v", got, want)
	}
	if got, want := rows[0]["callee_entity_id"], "content-entity:go-transitive-relationships-row"; got != want {
		t.Fatalf("callee_entity_id = %#v, want %#v", got, want)
	}
	if got, want := rows[0]["full_name"], "h.transitiveRelationshipsGraphRow"; got != want {
		t.Fatalf("full_name = %#v, want %#v", got, want)
	}
}

func TestExtractCodeCallRowsResolvesGoConstructorAssignedLocalReceiver(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "eval.go")
	if err := os.WriteFile(callerPath, []byte(`package main

type HTTPHarness struct{}

func NewHTTPHarness() *HTTPHarness {
	return &HTTPHarness{}
}

func (h *HTTPHarness) AddTestCases() {}

func addDemoTestCases() {
	harness := NewHTTPHarness()
	harness.AddTestCases()
}
`), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v, want nil", callerPath, err)
	}

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	payload, err := engine.ParsePath(repoRoot, callerPath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", callerPath, err)
	}
	for _, function := range payload["functions"].([]map[string]any) {
		name, _ := function["name"].(string)
		classContext, _ := function["class_context"].(string)
		switch {
		case name == "addDemoTestCases":
			function["uid"] = "content-entity:add-demo-test-cases"
		case name == "AddTestCases" && classContext == "HTTPHarness":
			function["uid"] = "content-entity:add-test-cases"
		case name == "NewHTTPHarness":
			function["uid"] = "content-entity:new-http-harness"
		}
	}

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-go",
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-go",
				"relative_path":    "eval.go",
				"parsed_file_data": payload,
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	assertCodeCallRow(t, rows, "content-entity:add-demo-test-cases", "content-entity:add-test-cases")
}

func TestExtractCodeCallRowsResolvesGoTypedParameterReceiver(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "eval.go")
	if err := os.WriteFile(callerPath, []byte(`package main

type HTTPHarness struct{}

func (h *HTTPHarness) AddTestCases() {}

func addDemoTestCases(harness *HTTPHarness) {
	harness.AddTestCases()
}
`), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v, want nil", callerPath, err)
	}

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	payload, err := engine.ParsePath(repoRoot, callerPath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", callerPath, err)
	}
	for _, function := range payload["functions"].([]map[string]any) {
		name, _ := function["name"].(string)
		classContext, _ := function["class_context"].(string)
		switch {
		case name == "addDemoTestCases":
			function["uid"] = "content-entity:add-demo-test-cases"
		case name == "AddTestCases" && classContext == "HTTPHarness":
			function["uid"] = "content-entity:add-test-cases"
		}
	}

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-go",
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-go",
				"relative_path":    "eval.go",
				"parsed_file_data": payload,
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	assertCodeCallRow(t, rows, "content-entity:add-demo-test-cases", "content-entity:add-test-cases")
}

func TestExtractCodeCallRowsResolvesGoRangeMapValueReceiver(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "descriptors.go")
	if err := os.WriteFile(callerPath, []byte(`package main

type ControllerDescriptor struct{}

func (d *ControllerDescriptor) BuildController() {}
func (d *ControllerDescriptor) RequiresSpecialHandling() bool { return false }

func runControllers(controllerDescriptors map[string]*ControllerDescriptor) {
	for _, controllerDesc := range controllerDescriptors {
		controllerDesc.BuildController()
		if controllerDesc.RequiresSpecialHandling() {
			continue
		}
	}
}
`), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v, want nil", callerPath, err)
	}

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	payload, err := engine.ParsePath(repoRoot, callerPath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", callerPath, err)
	}
	for _, function := range payload["functions"].([]map[string]any) {
		name, _ := function["name"].(string)
		classContext, _ := function["class_context"].(string)
		switch {
		case name == "runControllers":
			function["uid"] = "content-entity:run-controllers"
		case name == "BuildController" && classContext == "ControllerDescriptor":
			function["uid"] = "content-entity:build-controller"
		case name == "RequiresSpecialHandling" && classContext == "ControllerDescriptor":
			function["uid"] = "content-entity:requires-special-handling"
		}
	}

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-go",
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-go",
				"relative_path":    "descriptors.go",
				"parsed_file_data": payload,
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	assertCodeCallRow(t, rows, "content-entity:run-controllers", "content-entity:build-controller")
	assertCodeCallRow(t, rows, "content-entity:run-controllers", "content-entity:requires-special-handling")
}

func TestExtractCodeCallRowsResolvesGoFuncLiteralParameterReceiver(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "controllers.go")
	if err := os.WriteFile(callerPath, []byte(`package main

type ControllerDescriptor struct{}

func (d *ControllerDescriptor) BuildController() {}

func runControllers() {
	buildController := func(controllerDesc *ControllerDescriptor) error {
		controllerDesc.BuildController()
		return nil
	}
	_ = buildController
}
`), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v, want nil", callerPath, err)
	}

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	payload, err := engine.ParsePath(repoRoot, callerPath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", callerPath, err)
	}
	for _, function := range payload["functions"].([]map[string]any) {
		name, _ := function["name"].(string)
		classContext, _ := function["class_context"].(string)
		switch {
		case name == "runControllers":
			function["uid"] = "content-entity:run-controllers"
		case name == "BuildController" && classContext == "ControllerDescriptor":
			function["uid"] = "content-entity:build-controller"
		}
	}

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-go",
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-go",
				"relative_path":    "controllers.go",
				"parsed_file_data": payload,
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	assertCodeCallRow(t, rows, "content-entity:run-controllers", "content-entity:build-controller")
}

func TestExtractCodeCallRowsResolvesGoImportedTypedReceiver(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "cmd/kube-apiserver/app/server.go")
	calleePath := filepath.Join(repoRoot, "cmd/kube-apiserver/app/options/completion.go")
	if err := os.MkdirAll(filepath.Dir(callerPath), 0o700); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v, want nil", filepath.Dir(callerPath), err)
	}
	if err := os.MkdirAll(filepath.Dir(calleePath), 0o700); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v, want nil", filepath.Dir(calleePath), err)
	}
	if err := os.WriteFile(callerPath, []byte(`package app

import (
	"context"
	"k8s.io/kubernetes/cmd/kube-apiserver/app/options"
)

func NewAPIServerCommand(ctx context.Context, s *options.ServerRunOptions) error {
	_, err := s.Complete(ctx)
	return err
}
`), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v, want nil", callerPath, err)
	}
	if err := os.WriteFile(calleePath, []byte(`package options

import "context"

type ServerRunOptions struct{}
type CompletedOptions struct{}

func (s *ServerRunOptions) Complete(ctx context.Context) (CompletedOptions, error) {
	return CompletedOptions{}, nil
}
`), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v, want nil", calleePath, err)
	}

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	callerPayload, err := engine.ParsePath(repoRoot, callerPath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", callerPath, err)
	}
	calleePayload, err := engine.ParsePath(repoRoot, calleePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", calleePath, err)
	}
	for _, function := range callerPayload["functions"].([]map[string]any) {
		if name, _ := function["name"].(string); name == "NewAPIServerCommand" {
			function["uid"] = "content-entity:new-apiserver-command"
		}
	}
	for _, function := range calleePayload["functions"].([]map[string]any) {
		if name, _ := function["name"].(string); name == "Complete" {
			function["uid"] = "content-entity:server-run-options-complete"
		}
	}

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-go",
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-go",
				"relative_path":    "cmd/kube-apiserver/app/server.go",
				"parsed_file_data": callerPayload,
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-go",
				"relative_path":    "cmd/kube-apiserver/app/options/completion.go",
				"parsed_file_data": calleePayload,
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	assertCodeCallRow(t, rows, "content-entity:new-apiserver-command", "content-entity:server-run-options-complete")
}
