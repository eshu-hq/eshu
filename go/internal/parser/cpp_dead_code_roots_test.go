package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathCPPMarksDeadCodeRootKinds(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "callbacks.cpp")
	writeTestFile(
		t,
		sourcePath,
		`#include <functional>

using Handler = int (*)();

int main() {
    return 0;
}

int callbackTarget() {
    return 1;
}

int addressCallbackTarget() {
    return 2;
}

int pointerTarget() {
    return 3;
}

int stdFunctionTarget() {
    return 4;
}

int tableTarget() {
    return 5;
}

int unusedTarget() {
    return 6;
}

void nodeAddonInit() {}

void NODE_MODULE_INIT() {}

void NAPI_MODULE_INIT() {}

class Base {
public:
    virtual int run() const {
        return 7;
    }
};

class Derived : public Base {
public:
    int run() const override {
        return 8;
    }
};

void registerCallback(int (*)()) {}

void setup() {
    registerCallback(callbackTarget);
    registerCallback(&addressCallbackTarget);
    Handler handler = pointerTarget;
    std::function<int()> fn = stdFunctionTarget;
    Handler table[] = { tableTarget };
    NODE_MODULE(NODE_GYP_MODULE_NAME, nodeAddonInit)
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

	assertParserStringSliceContains(t, assertFunctionByName(t, got, "main"), "dead_code_root_kinds", "cpp.main_function")
	assertParserStringSliceContains(t, assertFunctionByName(t, got, "callbackTarget"), "dead_code_root_kinds", "cpp.callback_argument_target")
	assertParserStringSliceContains(t, assertFunctionByName(t, got, "addressCallbackTarget"), "dead_code_root_kinds", "cpp.callback_argument_target")
	assertParserStringSliceContains(t, assertFunctionByName(t, got, "pointerTarget"), "dead_code_root_kinds", "cpp.function_pointer_target")
	assertParserStringSliceContains(t, assertFunctionByName(t, got, "stdFunctionTarget"), "dead_code_root_kinds", "cpp.function_pointer_target")
	assertParserStringSliceContains(t, assertFunctionByName(t, got, "tableTarget"), "dead_code_root_kinds", "cpp.function_pointer_target")
	assertParserStringSliceContains(t, assertFunctionByName(t, got, "nodeAddonInit"), "dead_code_root_kinds", "cpp.node_addon_entrypoint")
	assertParserStringSliceContains(t, assertFunctionByName(t, got, "NODE_MODULE_INIT"), "dead_code_root_kinds", "cpp.node_addon_entrypoint")
	assertParserStringSliceContains(t, assertFunctionByName(t, got, "NAPI_MODULE_INIT"), "dead_code_root_kinds", "cpp.node_addon_entrypoint")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "run", "Base"), "dead_code_root_kinds", "cpp.virtual_method")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "run", "Derived"), "dead_code_root_kinds", "cpp.override_method")
	if unused := assertFunctionByName(t, got, "unusedTarget"); unused["dead_code_root_kinds"] != nil {
		t.Fatalf("unusedTarget dead_code_root_kinds = %#v, want nil", unused["dead_code_root_kinds"])
	}
}

func TestDefaultEngineParsePathCPPDeadCodeFixtureExpectedRoots(t *testing.T) {
	t.Parallel()

	repoRoot := repoFixturePath("deadcode", "cpp")
	sourcePath := repoFixturePath("deadcode", "cpp", "fixture.cpp")

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%s) error = %v, want nil", sourcePath, err)
	}

	assertParserStringSliceContains(t, assertFunctionByName(t, got, "main"), "dead_code_root_kinds", "cpp.main_function")
	assertParserStringSliceContains(t, assertFunctionByName(t, got, "eshuCppPublicAPI"), "dead_code_root_kinds", "cpp.public_header_api")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "render", "HeaderWidget"), "dead_code_root_kinds", "cpp.public_header_api")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "run", "Command"), "dead_code_root_kinds", "cpp.virtual_method")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "run", "DerivedCommand"), "dead_code_root_kinds", "cpp.override_method")
	assertParserStringSliceContains(t, assertFunctionByName(t, got, "directlyUsedHelper"), "dead_code_root_kinds", "cpp.callback_argument_target")
	assertParserStringSliceContains(t, assertFunctionByName(t, got, "dispatchTarget"), "dead_code_root_kinds", "cpp.function_pointer_target")
	assertParserStringSliceContains(t, assertFunctionByName(t, got, "NAPI_MODULE_INIT"), "dead_code_root_kinds", "cpp.node_addon_entrypoint")

	if helper := assertFunctionByName(t, got, "unusedCleanupCandidate"); helper["dead_code_root_kinds"] != nil {
		t.Fatalf("unusedCleanupCandidate dead_code_root_kinds = %#v, want nil", helper["dead_code_root_kinds"])
	}
	if helper := assertFunctionByNameAndClass(t, got, "helper", "HeaderWidget"); helper["dead_code_root_kinds"] != nil {
		t.Fatalf("HeaderWidget::helper dead_code_root_kinds = %#v, want nil", helper["dead_code_root_kinds"])
	}
}

func TestDefaultEngineParsePathCPPMarksIncludedHeaderPublicAPI(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "src", "widget.cpp")
	headerPath := filepath.Join(repoRoot, "src", "widget.hpp")
	otherHeaderPath := filepath.Join(repoRoot, "include", "private.hpp")
	writeTestFile(
		t,
		headerPath,
		`#pragma once

int exported_api();
static int static_header_helper();
// int commented_header_api();

class Widget {
public:
    int render() const;
private:
    int helper() const;
};
`,
	)
	writeTestFile(
		t,
		otherHeaderPath,
		`#pragma once

int not_exported_by_included_header();
`,
	)
	writeTestFile(
		t,
		sourcePath,
		`#include "widget.hpp"

int exported_api() {
    return 1;
}

int not_exported_by_included_header() {
    return 2;
}

static int static_header_helper() {
    return 3;
}

int commented_header_api() {
    return 4;
}

int Widget::render() const {
    return 5;
}

int Widget::helper() const {
    return 6;
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

	assertParserStringSliceContains(t, assertFunctionByName(t, got, "exported_api"), "dead_code_root_kinds", "cpp.public_header_api")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "render", "Widget"), "dead_code_root_kinds", "cpp.public_header_api")
	if private := assertFunctionByName(t, got, "not_exported_by_included_header"); private["dead_code_root_kinds"] != nil {
		t.Fatalf("not_exported_by_included_header dead_code_root_kinds = %#v, want nil", private["dead_code_root_kinds"])
	}
	if staticHelper := assertFunctionByName(t, got, "static_header_helper"); staticHelper["dead_code_root_kinds"] != nil {
		t.Fatalf("static_header_helper dead_code_root_kinds = %#v, want nil", staticHelper["dead_code_root_kinds"])
	}
	if commented := assertFunctionByName(t, got, "commented_header_api"); commented["dead_code_root_kinds"] != nil {
		t.Fatalf("commented_header_api dead_code_root_kinds = %#v, want nil", commented["dead_code_root_kinds"])
	}
	if helper := assertFunctionByNameAndClass(t, got, "helper", "Widget"); helper["dead_code_root_kinds"] != nil {
		t.Fatalf("Widget::helper dead_code_root_kinds = %#v, want nil", helper["dead_code_root_kinds"])
	}
}

func TestDefaultEngineParsePathCPPMarksNamespaceQualifiedHeaderMethod(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "src", "service.cpp")
	headerPath := filepath.Join(repoRoot, "src", "service.hpp")
	writeTestFile(
		t,
		headerPath,
		`#pragma once

namespace api {
class Service {
public:
    int run() const;
private:
    int helper() const;
};
}
`,
	)
	writeTestFile(
		t,
		sourcePath,
		`#include "service.hpp"

int api::Service::run() const {
    return 1;
}

int api::Service::helper() const {
    return 2;
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

	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "run", "Service"), "dead_code_root_kinds", "cpp.public_header_api")
	if helper := assertFunctionByNameAndClass(t, got, "helper", "Service"); helper["dead_code_root_kinds"] != nil {
		t.Fatalf("api::Service::helper dead_code_root_kinds = %#v, want nil", helper["dead_code_root_kinds"])
	}
}
