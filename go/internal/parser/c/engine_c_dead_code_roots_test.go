// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package c_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

func cFixturePath(t *testing.T, pathParts ...string) string {
	t.Helper()

	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok || sourceFile == "" {
		t.Fatal("runtime.Caller(0) could not locate C test source file")
		return ""
	}

	fixtureParts := []string{filepath.Dir(sourceFile), "..", "..", "..", "..", "tests", "fixtures"}
	return filepath.Join(append(fixtureParts, pathParts...)...)
}

func assertFunctionByName(t *testing.T, payload map[string]any, name string) map[string]any {
	t.Helper()

	return parsertest.AssertBucketItemByName(t, payload, "functions", name)
}

func TestDefaultEngineParsePathCDeadCodeFixtureExpectedRoots(t *testing.T) {
	t.Parallel()

	repoRoot := cFixturePath(t, "deadcode", "c")
	sourcePath := cFixturePath(t, "deadcode", "c", "fixture.c")

	got := parsertest.MustParsePath(t, repoRoot, sourcePath)

	parsertest.AssertStringSliceContains(t, assertFunctionByName(t, got, "main"), "dead_code_root_kinds", "c.main_function")
	parsertest.AssertStringSliceContains(t, assertFunctionByName(t, got, "eshu_c_public_api"), "dead_code_root_kinds", "c.public_header_api")
	parsertest.AssertStringSliceContains(t, assertFunctionByName(t, got, "registered_signal_handler"), "dead_code_root_kinds", "c.signal_handler")
	parsertest.AssertStringSliceContains(t, assertFunctionByName(t, got, "dispatch_target"), "dead_code_root_kinds", "c.function_pointer_target")

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
	for _, dir := range []string{filepath.Dir(headerPath), filepath.Dir(otherHeaderPath)} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) error = %v, want nil", dir, err)
		}
	}
	parsertest.WriteFile(
		t,
		headerPath,
		`#ifndef API_H
#define API_H

int exported_api(void);
static int static_header_helper(void);
/* int commented_public_api(void); */
// int line_commented_public_api(void);

#endif
`,
	)
	parsertest.WriteFile(
		t,
		otherHeaderPath,
		`#ifndef PRIVATE_H
#define PRIVATE_H

int not_exported_by_included_header(void);

#endif
`,
	)
	parsertest.WriteFile(
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

int commented_public_api(void) {
    return 4;
}

int line_commented_public_api(void) {
    return 5;
}
`,
	)

	got := parsertest.MustParsePath(t, repoRoot, sourcePath)

	parsertest.AssertStringSliceContains(t, assertFunctionByName(t, got, "exported_api"), "dead_code_root_kinds", "c.public_header_api")
	if private := assertFunctionByName(t, got, "not_exported_by_included_header"); private["dead_code_root_kinds"] != nil {
		t.Fatalf("not_exported_by_included_header dead_code_root_kinds = %#v, want nil", private["dead_code_root_kinds"])
	}
	if staticHelper := assertFunctionByName(t, got, "static_header_helper"); staticHelper["dead_code_root_kinds"] != nil {
		t.Fatalf("static_header_helper dead_code_root_kinds = %#v, want nil", staticHelper["dead_code_root_kinds"])
	}
	if commented := assertFunctionByName(t, got, "commented_public_api"); commented["dead_code_root_kinds"] != nil {
		t.Fatalf("commented_public_api dead_code_root_kinds = %#v, want nil", commented["dead_code_root_kinds"])
	}
	if commented := assertFunctionByName(t, got, "line_commented_public_api"); commented["dead_code_root_kinds"] != nil {
		t.Fatalf("line_commented_public_api dead_code_root_kinds = %#v, want nil", commented["dead_code_root_kinds"])
	}
}

func TestDefaultEngineParsePathCDoesNotReadHeadersOutsideRepoRoot(t *testing.T) {
	t.Parallel()

	parentRoot := t.TempDir()
	repoRoot := filepath.Join(parentRoot, "repo")
	sourcePath := filepath.Join(repoRoot, "src", "api.c")
	outsideHeaderPath := filepath.Join(parentRoot, "outside.h")
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) error = %v, want nil", filepath.Dir(sourcePath), err)
	}
	parsertest.WriteFile(
		t,
		outsideHeaderPath,
		`int outside_header_api(void);
`,
	)
	parsertest.WriteFile(
		t,
		sourcePath,
		`#include "../../outside.h"

int outside_header_api(void) {
    return 1;
}
`,
	)

	got := parsertest.MustParsePath(t, repoRoot, sourcePath)

	if outside := assertFunctionByName(t, got, "outside_header_api"); outside["dead_code_root_kinds"] != nil {
		t.Fatalf("outside_header_api dead_code_root_kinds = %#v, want nil", outside["dead_code_root_kinds"])
	}
}

func TestDefaultEngineParsePathCDoesNotFollowHeaderSymlinkOutsideRepoRoot(t *testing.T) {
	t.Parallel()

	parentRoot := t.TempDir()
	repoRoot := filepath.Join(parentRoot, "repo")
	sourcePath := filepath.Join(repoRoot, "src", "api.c")
	linkPath := filepath.Join(repoRoot, "src", "outside.h")
	outsideHeaderPath := filepath.Join(parentRoot, "outside.h")
	parsertest.WriteFile(
		t,
		outsideHeaderPath,
		`int symlink_header_api(void);
`,
	)
	if err := os.MkdirAll(filepath.Dir(linkPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) error = %v, want nil", filepath.Dir(linkPath), err)
	}
	if err := os.Symlink(outsideHeaderPath, linkPath); err != nil {
		t.Fatalf("Symlink(%s, %s) error = %v, want nil", outsideHeaderPath, linkPath, err)
	}
	parsertest.WriteFile(
		t,
		sourcePath,
		`#include "outside.h"

int symlink_header_api(void) {
    return 1;
}
`,
	)

	got := parsertest.MustParsePath(t, repoRoot, sourcePath)

	if outside := assertFunctionByName(t, got, "symlink_header_api"); outside["dead_code_root_kinds"] != nil {
		t.Fatalf("symlink_header_api dead_code_root_kinds = %#v, want nil", outside["dead_code_root_kinds"])
	}
}

func TestDefaultEngineParsePathCMarksCallbackArgumentTargets(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "callbacks.c")
	parsertest.WriteFile(
		t,
		sourcePath,
		`typedef void (*EventHandler)(int event_id);

void register_handler(EventHandler handler);

static void local_handler(int event_id) {
}

static void address_handler(int event_id) {
}

static void signal_address_handler(int signal_number) {
}

static void unused_handler(int event_id) {
}

void setup(void) {
    register_handler(local_handler);
    register_handler(&address_handler);
    signal(15, &signal_address_handler);
}
`,
	)

	got := parsertest.MustParsePath(t, repoRoot, sourcePath)

	parsertest.AssertStringSliceContains(t, assertFunctionByName(t, got, "local_handler"), "dead_code_root_kinds", "c.callback_argument_target")
	parsertest.AssertStringSliceContains(t, assertFunctionByName(t, got, "address_handler"), "dead_code_root_kinds", "c.callback_argument_target")
	parsertest.AssertStringSliceContains(t, assertFunctionByName(t, got, "signal_address_handler"), "dead_code_root_kinds", "c.signal_handler")
	parsertest.AssertStringSliceContains(t, assertFunctionByName(t, got, "signal_address_handler"), "dead_code_root_kinds", "c.callback_argument_target")
	if unused := assertFunctionByName(t, got, "unused_handler"); unused["dead_code_root_kinds"] != nil {
		t.Fatalf("unused_handler dead_code_root_kinds = %#v, want nil", unused["dead_code_root_kinds"])
	}
}

func TestDefaultEngineParsePathCMarksFunctionPointerInitializerVariants(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "function_pointers.c")
	parsertest.WriteFile(
		t,
		sourcePath,
		`typedef int (*Handler)(void);

int bare_direct_target(void) {
    return 1;
}

int address_direct_target(void) {
    return 2;
}

int typedef_target(void) {
    return 3;
}

int multi_first_target(void) {
    return 4;
}

int multi_second_target(void) {
    return 5;
}

int multi_typedef_first_target(void) {
    return 6;
}

int multi_typedef_second_target(void) {
    return 7;
}

int table_first_target(void) {
    return 8;
}

int table_second_target(void) {
    return 9;
}

int typedef_table_first_target(void) {
    return 10;
}

int typedef_table_second_target(void) {
    return 11;
}

int unused_target(void) {
    return 12;
}

void setup(void) {
    int (*direct_handler)(void) = bare_direct_target;
    int (*address_handler)(void) = &address_direct_target;
    Handler typedef_handler = &typedef_target;
    int (*multi_first)(void) = multi_first_target, (*multi_second)(void) = &multi_second_target;
    Handler typedef_first = multi_typedef_first_target, typedef_second = &multi_typedef_second_target;
    int (*handler_table[])(void) = { table_first_target, &table_second_target };
    Handler typedef_table[] = { typedef_table_first_target, &typedef_table_second_target };
}
`,
	)

	got := parsertest.MustParsePath(t, repoRoot, sourcePath)

	parsertest.AssertStringSliceContains(t, assertFunctionByName(t, got, "bare_direct_target"), "dead_code_root_kinds", "c.function_pointer_target")
	parsertest.AssertStringSliceContains(t, assertFunctionByName(t, got, "address_direct_target"), "dead_code_root_kinds", "c.function_pointer_target")
	parsertest.AssertStringSliceContains(t, assertFunctionByName(t, got, "typedef_target"), "dead_code_root_kinds", "c.function_pointer_target")
	parsertest.AssertStringSliceContains(t, assertFunctionByName(t, got, "multi_first_target"), "dead_code_root_kinds", "c.function_pointer_target")
	parsertest.AssertStringSliceContains(t, assertFunctionByName(t, got, "multi_second_target"), "dead_code_root_kinds", "c.function_pointer_target")
	parsertest.AssertStringSliceContains(t, assertFunctionByName(t, got, "multi_typedef_first_target"), "dead_code_root_kinds", "c.function_pointer_target")
	parsertest.AssertStringSliceContains(t, assertFunctionByName(t, got, "multi_typedef_second_target"), "dead_code_root_kinds", "c.function_pointer_target")
	parsertest.AssertStringSliceContains(t, assertFunctionByName(t, got, "table_first_target"), "dead_code_root_kinds", "c.function_pointer_target")
	parsertest.AssertStringSliceContains(t, assertFunctionByName(t, got, "table_second_target"), "dead_code_root_kinds", "c.function_pointer_target")
	parsertest.AssertStringSliceContains(t, assertFunctionByName(t, got, "typedef_table_first_target"), "dead_code_root_kinds", "c.function_pointer_target")
	parsertest.AssertStringSliceContains(t, assertFunctionByName(t, got, "typedef_table_second_target"), "dead_code_root_kinds", "c.function_pointer_target")
	if unused := assertFunctionByName(t, got, "unused_target"); unused["dead_code_root_kinds"] != nil {
		t.Fatalf("unused_target dead_code_root_kinds = %#v, want nil", unused["dead_code_root_kinds"])
	}
}

func TestDefaultEngineParsePathCMarksDuplicateEntrypoints(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "conditional_main.c")
	parsertest.WriteFile(
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

	got := parsertest.MustParsePath(t, repoRoot, sourcePath)

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
		parsertest.AssertStringSliceContains(t, item, "dead_code_root_kinds", "c.main_function")
	}
	if got, want := mainCount, 2; got != want {
		t.Fatalf("main function count = %d, want %d in %#v", got, want, functions)
	}
}
