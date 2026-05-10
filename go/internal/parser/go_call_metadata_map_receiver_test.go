package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathGoKeepsMapValueReceiverBindingsBlockScoped(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "descriptors.go")
	writeTestFile(
		t,
		filePath,
		`package main

type OuterDescriptor struct{}
type InnerDescriptor struct{}

func (d *OuterDescriptor) BuildOuter() {}
func (d *InnerDescriptor) BuildInner() {}

func runControllers(controllerDescriptors map[string]*OuterDescriptor, enabled bool) {
	if enabled {
		controllerDescriptors := map[string]*InnerDescriptor{}
		for _, controllerDesc := range controllerDescriptors {
			controllerDesc.BuildInner()
		}
	}
	for _, controllerDesc := range controllerDescriptors {
		controllerDesc.BuildOuter()
	}
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

	innerCall := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "controllerDesc.BuildInner")
	assertStringFieldValue(t, innerCall, "receiver_identifier", "controllerDesc")
	assertStringFieldValue(t, innerCall, "inferred_obj_type", "InnerDescriptor")

	outerCall := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "controllerDesc.BuildOuter")
	assertStringFieldValue(t, outerCall, "receiver_identifier", "controllerDesc")
	assertStringFieldValue(t, outerCall, "inferred_obj_type", "OuterDescriptor")
}
