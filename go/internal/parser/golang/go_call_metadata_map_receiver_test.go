// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package golang_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

func TestDefaultEngineParsePathGoKeepsMapValueReceiverBindingsBlockScoped(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "descriptors.go")
	parsertest.WriteFile(
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

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	innerCall := parsertest.AssertBucketItemByFieldValue(t, got, "function_calls", "full_name", "controllerDesc.BuildInner")
	parsertest.AssertStringFieldValue(t, innerCall, "receiver_identifier", "controllerDesc")
	parsertest.AssertStringFieldValue(t, innerCall, "inferred_obj_type", "InnerDescriptor")

	outerCall := parsertest.AssertBucketItemByFieldValue(t, got, "function_calls", "full_name", "controllerDesc.BuildOuter")
	parsertest.AssertStringFieldValue(t, outerCall, "receiver_identifier", "controllerDesc")
	parsertest.AssertStringFieldValue(t, outerCall, "inferred_obj_type", "OuterDescriptor")
}
