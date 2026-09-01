// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kotlin_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"

	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

func TestDefaultEngineParsePathKotlinInfersSameFileFunctionReturnTypeAliasCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Usage.kt")
	writeKotlinTestFile(
		t,
		filePath,
		`package comprehensive

class Service {
    fun info(): String = "ok"
}

fun createService(): Service = Service()

fun usage(): String {
    val provider = createService()
    return provider.info()
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

	items, ok := got["function_calls"].([]map[string]any)
	if !ok {
		t.Fatalf("function_calls = %T, want []map[string]any", got["function_calls"])
	}

	for _, item := range items {
		fullName, _ := item["full_name"].(string)
		if fullName != "provider.info" {
			continue
		}
		parsertest.AssertStringFieldValue(t, item, "inferred_obj_type", "Service")
		return
	}
	t.Fatalf("function_calls missing full_name=%q in %#v", "provider.info", items)
}

func TestDefaultEngineParsePathKotlinInfersSameFileFunctionReturnAliasChainCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Usage.kt")
	writeKotlinTestFile(
		t,
		filePath,
		`package comprehensive

class Service {
    fun info(): String = "ok"
}

fun createService(): Service = Service()

fun usage(): String {
    val provider = createService()
    val active = provider
    return active.info()
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

	items, ok := got["function_calls"].([]map[string]any)
	if !ok {
		t.Fatalf("function_calls = %T, want []map[string]any", got["function_calls"])
	}

	for _, item := range items {
		fullName, _ := item["full_name"].(string)
		if fullName != "active.info" {
			continue
		}
		parsertest.AssertStringFieldValue(t, item, "inferred_obj_type", "Service")
		return
	}
	t.Fatalf("function_calls missing full_name=%q in %#v", "active.info", items)
}

func TestDefaultEngineParsePathKotlinInfersNullableFunctionReturnTypeAliasCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Usage.kt")
	writeKotlinTestFile(
		t,
		filePath,
		`package comprehensive

class Service {
    fun info(): String = "ok"
}

fun createService(): Service? = Service()

fun usage(): String {
    val service = createService()
    return service?.info() ?: "missing"
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

	items, ok := got["function_calls"].([]map[string]any)
	if !ok {
		t.Fatalf("function_calls = %T, want []map[string]any", got["function_calls"])
	}

	for _, item := range items {
		fullName, _ := item["full_name"].(string)
		if fullName != "service.info" {
			continue
		}
		parsertest.AssertStringFieldValue(t, item, "inferred_obj_type", "Service")
		return
	}
	t.Fatalf("function_calls missing full_name=%q in %#v", "service.info", items)
}

func TestDefaultEngineParsePathKotlinInfersGenericFunctionReturnTypeAliasCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Usage.kt")
	writeKotlinTestFile(
		t,
		filePath,
		`package comprehensive

class ServiceBox<T> {
    fun info(): String = "ok"
}

fun createBox(): ServiceBox<String> = ServiceBox()

fun usage(): String {
    val box = createBox()
    return box.info()
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

	items, ok := got["function_calls"].([]map[string]any)
	if !ok {
		t.Fatalf("function_calls = %T, want []map[string]any", got["function_calls"])
	}

	for _, item := range items {
		fullName, _ := item["full_name"].(string)
		if fullName != "box.info" {
			continue
		}
		parsertest.AssertStringFieldValue(t, item, "inferred_obj_type", "ServiceBox")
		return
	}
	t.Fatalf("function_calls missing full_name=%q in %#v", "box.info", items)
}

func TestDefaultEngineParsePathKotlinInfersFunctionReturnReceiverChainsForDotCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Usage.kt")
	writeKotlinTestFile(
		t,
		filePath,
		`package comprehensive

class Service {
    fun info(): String = "ok"
}

class Factory {
    fun createService(): Service = Service()
}

fun usage(): String {
    val factory = Factory()
    return factory.createService().info()
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

	items, ok := got["function_calls"].([]map[string]any)
	if !ok {
		t.Fatalf("function_calls = %T, want []map[string]any", got["function_calls"])
	}

	for _, item := range items {
		fullName, _ := item["full_name"].(string)
		if fullName != "factory.createService().info" {
			continue
		}
		parsertest.AssertStringFieldValue(t, item, "inferred_obj_type", "Service")
		return
	}
	t.Fatalf("function_calls missing full_name=%q in %#v", "factory.createService().info", items)
}

func TestDefaultEngineParsePathKotlinInfersNestedFunctionReturnAssignmentReceiverCallsForDotCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Usage.kt")
	writeKotlinTestFile(
		t,
		filePath,
		`package comprehensive

class Service {
    fun info(): String = "ok"
}

class Factory {
    fun createService(): Service = Service()
}

fun createFactory(): Factory = Factory()

fun usage(): String {
    val service = createFactory().createService()
    return service.info()
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

	items, ok := got["function_calls"].([]map[string]any)
	if !ok {
		t.Fatalf("function_calls = %T, want []map[string]any", got["function_calls"])
	}

	want := map[string]string{
		"createFactory().createService": "Factory",
		"service.info":                  "Service",
	}
	for _, item := range items {
		fullName, _ := item["full_name"].(string)
		if wantType, ok := want[fullName]; ok {
			parsertest.AssertStringFieldValue(t, item, "inferred_obj_type", wantType)
			delete(want, fullName)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing inferred receiver calls: %#v in %#v", want, items)
	}
}

func TestDefaultEngineParsePathKotlinInfersConstructorRootReceiverChainsForDotCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Usage.kt")
	writeKotlinTestFile(
		t,
		filePath,
		`package comprehensive

class Service {
    fun info(): String = "ok"
}

class Factory {
    fun createService(): Service = Service()
}

fun usage(): String {
    return Factory().createService().info()
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

	items, ok := got["function_calls"].([]map[string]any)
	if !ok {
		t.Fatalf("function_calls = %T, want []map[string]any", got["function_calls"])
	}

	want := map[string]string{
		"Factory().createService().info": "Service",
	}
	for _, item := range items {
		fullName, _ := item["full_name"].(string)
		if wantType, ok := want[fullName]; ok {
			parsertest.AssertStringFieldValue(t, item, "inferred_obj_type", wantType)
			delete(want, fullName)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing inferred receiver calls: %#v in %#v", want, items)
	}
}

func TestDefaultEngineParsePathKotlinInfersParenthesizedFunctionReturnReceiverChainsForDotCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Usage.kt")
	writeKotlinTestFile(
		t,
		filePath,
		`package comprehensive

class Service {
    fun info(): String = "ok"
}

class Factory {
    fun createService(): Service = Service()
}

fun createFactory(): Factory = Factory()

fun usage(): String {
    return (createFactory().createService()).info()
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

	items, ok := got["function_calls"].([]map[string]any)
	if !ok {
		t.Fatalf("function_calls = %T, want []map[string]any", got["function_calls"])
	}

	want := map[string]string{
		"createFactory().createService":        "Factory",
		"createFactory().createService().info": "Service",
	}
	for _, item := range items {
		fullName, _ := item["full_name"].(string)
		if wantType, ok := want[fullName]; ok {
			parsertest.AssertStringFieldValue(t, item, "inferred_obj_type", wantType)
			delete(want, fullName)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing inferred receiver calls: %#v in %#v", want, items)
	}
}

func TestDefaultEngineParsePathKotlinInfersSiblingFileFunctionReturnTypeAliasCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	apiPath := filepath.Join(repoRoot, "Api.kt")
	usagePath := filepath.Join(repoRoot, "Usage.kt")
	writeKotlinTestFile(
		t,
		apiPath,
		`package comprehensive

class Service {
    fun info(): String = "ok"
}

class Factory {
    fun createService(): Service = Service()
}

fun createFactory(): Factory = Factory()
`,
	)
	writeKotlinTestFile(
		t,
		usagePath,
		`package comprehensive

fun usage(): String {
    val service = createFactory().createService()
    return service.info()
}
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, usagePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	items, ok := got["function_calls"].([]map[string]any)
	if !ok {
		t.Fatalf("function_calls = %T, want []map[string]any", got["function_calls"])
	}

	want := map[string]string{
		"createFactory().createService": "Factory",
		"service.info":                  "Service",
	}
	for _, item := range items {
		fullName, _ := item["full_name"].(string)
		if wantType, ok := want[fullName]; ok {
			parsertest.AssertStringFieldValue(t, item, "inferred_obj_type", wantType)
			delete(want, fullName)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing inferred receiver calls: %#v in %#v", want, items)
	}
}

func TestDefaultEngineParsePathKotlinInfersParentDirectorySiblingFunctionReturnTypeAliasCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	apiPath := filepath.Join(repoRoot, "Api.kt")
	nestedDir := filepath.Join(repoRoot, "nested")
	usagePath := filepath.Join(nestedDir, "Usage.kt")
	writeKotlinTestFile(
		t,
		apiPath,
		`package comprehensive

class Service {
    fun info(): String = "ok"
}

class Factory {
    fun createService(): Service = Service()
}

fun createFactory(): Factory = Factory()
`,
	)
	writeKotlinTestFile(
		t,
		usagePath,
		`package comprehensive

fun usage(): String {
    val service = createFactory().createService()
    return service.info()
}
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, usagePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", usagePath, err)
	}

	items, ok := got["function_calls"].([]map[string]any)
	if !ok {
		t.Fatalf("function_calls = %T, want []map[string]any", got["function_calls"])
	}

	want := map[string]string{
		"createFactory().createService": "Factory",
		"service.info":                  "Service",
	}
	for _, item := range items {
		fullName, _ := item["full_name"].(string)
		if wantType, ok := want[fullName]; ok {
			parsertest.AssertStringFieldValue(t, item, "inferred_obj_type", wantType)
			delete(want, fullName)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing inferred receiver calls: %#v in %#v", want, items)
	}
}

func TestDefaultEngineParsePathKotlinInfersSiblingFileFunctionReturnAliasChainCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	apiPath := filepath.Join(repoRoot, "Api.kt")
	usagePath := filepath.Join(repoRoot, "Usage.kt")
	writeKotlinTestFile(
		t,
		apiPath,
		`package comprehensive

class Service {
    fun info(): String = "ok"
}

fun createService(): Service = Service()
`,
	)
	writeKotlinTestFile(
		t,
		usagePath,
		`package comprehensive

fun usage(): String {
    val provider = createService()
    val active = provider
    return active.info()
}
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, usagePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", usagePath, err)
	}

	items, ok := got["function_calls"].([]map[string]any)
	if !ok {
		t.Fatalf("function_calls = %T, want []map[string]any", got["function_calls"])
	}

	for _, item := range items {
		fullName, _ := item["full_name"].(string)
		if fullName != "active.info" {
			continue
		}
		parsertest.AssertStringFieldValue(t, item, "inferred_obj_type", "Service")
		return
	}
	t.Fatalf("function_calls missing full_name=%q in %#v", "active.info", items)
}

func TestDefaultEngineParsePathKotlinPrefersPackageAwareSiblingFunctionReturnTypesForDotCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	apiPath := filepath.Join(repoRoot, "Api.kt")
	otherPath := filepath.Join(repoRoot, "Other.kt")
	usagePath := filepath.Join(repoRoot, "Usage.kt")
	writeKotlinTestFile(
		t,
		apiPath,
		`package comprehensive

class Service {
    fun info(): String = "ok"
}

class Factory {
    fun createService(): Service = Service()
}

fun createFactory(): Factory = Factory()
`,
	)
	writeKotlinTestFile(
		t,
		otherPath,
		`package otherpkg

class OtherFactory {
    fun createService(): String = "wrong"
}

fun createFactory(): OtherFactory = OtherFactory()
`,
	)
	writeKotlinTestFile(
		t,
		usagePath,
		`package comprehensive

fun usage(): String {
    val service = createFactory().createService()
    return service.info()
}
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, usagePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	items, ok := got["function_calls"].([]map[string]any)
	if !ok {
		t.Fatalf("function_calls = %T, want []map[string]any", got["function_calls"])
	}

	want := map[string]string{
		"createFactory().createService": "Factory",
		"service.info":                  "Service",
	}
	for _, item := range items {
		fullName, _ := item["full_name"].(string)
		if wantType, ok := want[fullName]; ok {
			parsertest.AssertStringFieldValue(t, item, "inferred_obj_type", wantType)
			delete(want, fullName)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing inferred receiver calls: %#v in %#v", want, items)
	}
}

func TestDefaultEngineParsePathKotlinPrefersPackageAwareSiblingFunctionReturnTypesAcrossGrandparentDirectoriesForDotCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	apiPath := filepath.Join(repoRoot, "src", "main", "kotlin", "common", "Api.kt")
	usagePath := filepath.Join(repoRoot, "src", "main", "kotlin", "feature", "module", "Usage.kt")
	writeKotlinTestFile(
		t,
		apiPath,
		`package comprehensive

class Service {
    fun info(): String = "ok"
}

class Factory {
    fun createService(): Service = Service()
}

fun createFactory(): Factory = Factory()
`,
	)
	writeKotlinTestFile(
		t,
		usagePath,
		`package comprehensive

fun usage(): String {
    val service = createFactory().createService()
    return service.info()
}
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, usagePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", usagePath, err)
	}

	items, ok := got["function_calls"].([]map[string]any)
	if !ok {
		t.Fatalf("function_calls = %T, want []map[string]any", got["function_calls"])
	}

	want := map[string]string{
		"createFactory().createService": "Factory",
		"service.info":                  "Service",
	}
	for _, item := range items {
		fullName, _ := item["full_name"].(string)
		if wantType, ok := want[fullName]; ok {
			parsertest.AssertStringFieldValue(t, item, "inferred_obj_type", wantType)
			delete(want, fullName)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing inferred receiver calls: %#v in %#v", want, items)
	}
}

func TestDefaultEngineParsePathKotlinInfersCrossFilePackageAwareFunctionReturnReceiverChainsForDotCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	servicePath := filepath.Join(repoRoot, "src", "main", "kotlin", "com", "example", "api", "Service.kt")
	factoryPath := filepath.Join(repoRoot, "src", "main", "kotlin", "com", "example", "api", "Factory.kt")
	factoryHelpersPath := filepath.Join(repoRoot, "src", "main", "kotlin", "com", "example", "factory", "Factories.kt")
	conflictPath := filepath.Join(repoRoot, "src", "main", "kotlin", "com", "other", "Other.kt")
	usagePath := filepath.Join(repoRoot, "src", "main", "kotlin", "com", "example", "feature", "module", "Usage.kt")

	writeKotlinTestFile(
		t,
		servicePath,
		`package com.example

class Service {
    fun info(): String = "ok"
}
`,
	)
	writeKotlinTestFile(
		t,
		factoryPath,
		`package com.example

class Factory {
    fun createService(): Service = Service()
}
`,
	)
	writeKotlinTestFile(
		t,
		factoryHelpersPath,
		`package com.example

fun createFactory(): Factory = Factory()
`,
	)
	writeKotlinTestFile(
		t,
		conflictPath,
		`package com.other

class OtherService {
    fun info(): String = "wrong"
}

class OtherFactory {
    fun createService(): OtherService = OtherService()
}

fun createFactory(): OtherFactory = OtherFactory()
`,
	)
	writeKotlinTestFile(
		t,
		usagePath,
		`package com.example

fun usage(): String {
    return createFactory().createService().info()
}
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, usagePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", usagePath, err)
	}

	items, ok := got["function_calls"].([]map[string]any)
	if !ok {
		t.Fatalf("function_calls = %T, want []map[string]any", got["function_calls"])
	}

	want := map[string]string{
		"createFactory().createService":        "Factory",
		"createFactory().createService().info": "Service",
	}
	for _, item := range items {
		fullName, _ := item["full_name"].(string)
		if wantType, ok := want[fullName]; ok {
			parsertest.AssertStringFieldValue(t, item, "inferred_obj_type", wantType)
			delete(want, fullName)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing inferred receiver calls: %#v in %#v", want, items)
	}
}

func TestDefaultEngineParsePathKotlinInfersParenthesizedCrossFilePackageAwareFunctionReturnReceiverChainsForDotCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	servicePath := filepath.Join(repoRoot, "src", "main", "kotlin", "com", "example", "api", "Service.kt")
	factoryPath := filepath.Join(repoRoot, "src", "main", "kotlin", "com", "example", "api", "Factory.kt")
	factoryHelpersPath := filepath.Join(repoRoot, "src", "main", "kotlin", "com", "example", "factory", "Factories.kt")
	conflictPath := filepath.Join(repoRoot, "src", "main", "kotlin", "com", "other", "Other.kt")
	usagePath := filepath.Join(repoRoot, "src", "main", "kotlin", "com", "example", "feature", "module", "Usage.kt")

	writeKotlinTestFile(
		t,
		servicePath,
		`package com.example

class Service {
    fun info(): String = "ok"
}
`,
	)
	writeKotlinTestFile(
		t,
		factoryPath,
		`package com.example

class Factory {
    fun createService(): Service = Service()
}
`,
	)
	writeKotlinTestFile(
		t,
		factoryHelpersPath,
		`package com.example

fun createFactory(): Factory = Factory()
`,
	)
	writeKotlinTestFile(
		t,
		conflictPath,
		`package com.other

class OtherService {
    fun info(): String = "wrong"
}

class OtherFactory {
    fun createService(): OtherService = OtherService()
}

fun createFactory(): OtherFactory = OtherFactory()
`,
	)
	writeKotlinTestFile(
		t,
		usagePath,
		`package com.example

fun usage(): String {
    return (createFactory().createService()).info()
}
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, usagePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", usagePath, err)
	}

	items, ok := got["function_calls"].([]map[string]any)
	if !ok {
		t.Fatalf("function_calls = %T, want []map[string]any", got["function_calls"])
	}

	want := map[string]string{
		"createFactory().createService":        "Factory",
		"createFactory().createService().info": "Service",
	}
	for _, item := range items {
		fullName, _ := item["full_name"].(string)
		if wantType, ok := want[fullName]; ok {
			parsertest.AssertStringFieldValue(t, item, "inferred_obj_type", wantType)
			delete(want, fullName)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing inferred receiver calls: %#v in %#v", want, items)
	}
}
