package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathCDeadCodeFixtureExpectedRoots(t *testing.T) {
	t.Parallel()

	repoRoot := repoFixturePath("deadcode", "c")
	sourcePath := repoFixturePath("deadcode", "c", "fixture.c")

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%s) error = %v, want nil", sourcePath, err)
	}

	assertParserStringSliceContains(t, assertFunctionByName(t, got, "main"), "dead_code_root_kinds", "c.main_function")
	assertParserStringSliceContains(t, assertFunctionByName(t, got, "eshu_c_public_api"), "dead_code_root_kinds", "c.public_header_api")
	assertParserStringSliceContains(t, assertFunctionByName(t, got, "registered_signal_handler"), "dead_code_root_kinds", "c.signal_handler")
	assertParserStringSliceContains(t, assertFunctionByName(t, got, "dispatch_target"), "dead_code_root_kinds", "c.function_pointer_target")

	if helper := assertFunctionByName(t, got, "directly_used_helper"); helper["dead_code_root_kinds"] != nil {
		t.Fatalf("directly_used_helper dead_code_root_kinds = %#v, want nil", helper["dead_code_root_kinds"])
	}
}

func TestDefaultEngineParsePathCMarksOnlyIncludedHeaderPrototypesAsPublicAPI(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "src", "api.c")
	headerPath := filepath.Join(repoRoot, "src", "api.h")
	otherHeaderPath := filepath.Join(repoRoot, "include", "private.h")
	writeTestFile(
		t,
		headerPath,
		`#ifndef API_H
#define API_H

int exported_api(void);
static int static_header_helper(void);

#endif
`,
	)
	writeTestFile(
		t,
		otherHeaderPath,
		`#ifndef PRIVATE_H
#define PRIVATE_H

int not_exported_by_included_header(void);

#endif
`,
	)
	writeTestFile(
		t,
		sourcePath,
		`#include "api.h"

int exported_api(void) {
    return 1;
}

int not_exported_by_included_header(void) {
    return 2;
}

static int static_header_helper(void) {
    return 3;
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%s) error = %v, want nil", sourcePath, err)
	}

	assertParserStringSliceContains(t, assertFunctionByName(t, got, "exported_api"), "dead_code_root_kinds", "c.public_header_api")
	if private := assertFunctionByName(t, got, "not_exported_by_included_header"); private["dead_code_root_kinds"] != nil {
		t.Fatalf("not_exported_by_included_header dead_code_root_kinds = %#v, want nil", private["dead_code_root_kinds"])
	}
	if staticHelper := assertFunctionByName(t, got, "static_header_helper"); staticHelper["dead_code_root_kinds"] != nil {
		t.Fatalf("static_header_helper dead_code_root_kinds = %#v, want nil", staticHelper["dead_code_root_kinds"])
	}
}

func TestDefaultEngineParsePathCMarksCallbackArgumentTargets(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "callbacks.c")
	writeTestFile(
		t,
		sourcePath,
		`typedef void (*EventHandler)(int event_id);

void register_handler(EventHandler handler);

static void local_handler(int event_id) {
}

static void unused_handler(int event_id) {
}

void setup(void) {
    register_handler(local_handler);
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%s) error = %v, want nil", sourcePath, err)
	}

	assertParserStringSliceContains(t, assertFunctionByName(t, got, "local_handler"), "dead_code_root_kinds", "c.callback_argument_target")
	if unused := assertFunctionByName(t, got, "unused_handler"); unused["dead_code_root_kinds"] != nil {
		t.Fatalf("unused_handler dead_code_root_kinds = %#v, want nil", unused["dead_code_root_kinds"])
	}
}

func TestDefaultEngineParsePathCMarksDuplicateEntrypoints(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "conditional_main.c")
	writeTestFile(
		t,
		sourcePath,
		`#ifdef FIRST
int main(void) {
    return 0;
}
#else
int main(int argc, char **argv) {
    return argc;
}
#endif
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%s) error = %v, want nil", sourcePath, err)
	}

	functions, ok := got["functions"].([]map[string]any)
	if !ok {
		t.Fatalf("functions = %T, want []map[string]any", got["functions"])
	}
	mainCount := 0
	for _, item := range functions {
		if item["name"] != "main" {
			continue
		}
		mainCount++
		assertParserStringSliceContains(t, item, "dead_code_root_kinds", "c.main_function")
	}
	if got, want := mainCount, 2; got != want {
		t.Fatalf("main function count = %d, want %d in %#v", got, want, functions)
	}
}
