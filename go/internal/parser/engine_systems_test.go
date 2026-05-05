package parser

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestDefaultEngineParsePathC(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "main.c")
	writeTestFile(
		t,
		filePath,
		`#include <stdio.h>
#define MAX_SIZE 1024

struct Point {
    double x;
    double y;
};

enum StatusCode {
    STATUS_OK = 0,
    STATUS_ERROR = 1
};

int add(int a, int b) {
    int total = a + b;
    printf("hello\n");
    return total;
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{VariableScope: "all"})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	if got["lang"] != "c" {
		t.Fatalf("lang = %#v, want %#v", got["lang"], "c")
	}

	assertNamedBucketContains(t, got, "functions", "add")
	assertNamedBucketContains(t, got, "structs", "Point")
	assertNamedBucketContains(t, got, "enums", "StatusCode")
	importItem := assertBucketItemByName(t, got, "imports", "stdio.h")
	assertStringFieldValue(t, importItem, "source", "stdio.h")
	assertStringFieldValue(t, importItem, "full_import_name", "#include <stdio.h>")
	assertStringFieldValue(t, importItem, "include_kind", "system")
	callItem := assertBucketItemByName(t, got, "function_calls", "printf")
	assertStringFieldValue(t, callItem, "full_name", "printf")
	assertNamedBucketContains(t, got, "variables", "total")
	assertNamedBucketContains(t, got, "macros", "MAX_SIZE")
}

func TestDefaultEngineParsePathCPP(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "main.cpp")
	writeTestFile(
		t,
		filePath,
		`#include <iostream>
#define VERSION "1.0.0"

template<typename T>
T max_value(T a, T b) {
    return (a > b) ? a : b;
}

struct Point {
    double x;
    double y;
};

enum class Direction {
    North,
    South
};

class Circle {
public:
    double area() const {
        notify();
        return 3.14;
    }
};
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

	if got["lang"] != "cpp" {
		t.Fatalf("lang = %#v, want %#v", got["lang"], "cpp")
	}

	assertNamedBucketContains(t, got, "functions", "max_value")
	assertNamedBucketContains(t, got, "functions", "area")
	assertNamedBucketContains(t, got, "classes", "Circle")
	assertNamedBucketContains(t, got, "structs", "Point")
	assertNamedBucketContains(t, got, "enums", "Direction")
	assertNamedBucketContains(t, got, "imports", "iostream")
	assertNamedBucketContains(t, got, "function_calls", "notify")
	assertNamedBucketContains(t, got, "macros", "VERSION")
}

func TestDefaultEngineParsePathRust(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "main.rs")
	writeTestFile(
		t,
		filePath,
		`use std::fmt;

pub trait Describable {
    fn describe(&self) -> String;
}

pub struct Point {
    pub x: f64,
}

pub enum Shape {
    Circle,
    Square,
}

impl Point {
    fn new(x: f64) -> Self {
        Point { x }
    }
}

fn main() {
    let point = Point::new(1.0);
    println!("{}", point.x);
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

	if got["lang"] != "rust" {
		t.Fatalf("lang = %#v, want %#v", got["lang"], "rust")
	}

	assertNamedBucketContains(t, got, "functions", "new")
	assertNamedBucketContains(t, got, "functions", "main")
	assertNamedBucketContains(t, got, "classes", "Point")
	assertNamedBucketContains(t, got, "classes", "Shape")
	assertNamedBucketContains(t, got, "traits", "Describable")
	importItem := assertBucketItemByName(t, got, "imports", "std::fmt")
	assertStringFieldValue(t, importItem, "source", "std::fmt")
	assertStringFieldValue(t, importItem, "alias", "fmt")
	assertStringFieldValue(t, importItem, "full_import_name", "use std::fmt;")
	assertBucketContainsFieldValue(t, got, "function_calls", "full_name", "Point::new")
	assertBucketContainsFieldValue(t, got, "function_calls", "full_name", "println")
}

func TestDefaultEngineParsePathRustImplOwnership(t *testing.T) {
	t.Parallel()

	repoRoot := repoFixturePath("ecosystems", "rust_comprehensive")
	filePath := filepath.Join(repoRoot, "impl_blocks.rs")

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertBucketContainsFieldValue(t, got, "functions", "impl_context", "Point")
	assertBucketContainsFieldValue(t, got, "functions", "impl_context", "VecContainer")
}

func TestDefaultEngineParsePathRustImplBlocks(t *testing.T) {
	t.Parallel()

	repoRoot := repoFixturePath("ecosystems", "rust_comprehensive")
	filePath := filepath.Join(repoRoot, "impl_blocks.rs")

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	blocks, ok := got["impl_blocks"].([]map[string]any)
	if !ok {
		t.Fatalf("impl_blocks = %T, want []map[string]any", got["impl_blocks"])
	}
	if len(blocks) != 5 {
		t.Fatalf("impl_blocks = %#v, want 5 entries", blocks)
	}

	assertNamedBucketContains(t, got, "impl_blocks", "Point")
	assertNamedBucketContains(t, got, "impl_blocks", "Shape")
	assertNamedBucketContains(t, got, "impl_blocks", "NamedItem")
	assertNamedBucketContains(t, got, "impl_blocks", "VecContainer")
	assertBucketContainsFieldValue(t, got, "impl_blocks", "kind", "trait_impl")
	assertBucketContainsFieldValue(t, got, "impl_blocks", "trait", "Describable")
	assertBucketContainsFieldValue(t, got, "impl_blocks", "trait", "Container")
	assertBucketContainsFieldValue(t, got, "impl_blocks", "target", "VecContainer<T>")
}

func TestDefaultEnginePreScanPathsSystems(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	cPath := filepath.Join(repoRoot, "main.c")
	cppPath := filepath.Join(repoRoot, "main.cpp")
	rustPath := filepath.Join(repoRoot, "main.rs")

	writeTestFile(
		t,
		cPath,
		`struct Point {};
int add(int a, int b) { return a + b; }
`,
	)
	writeTestFile(
		t,
		cppPath,
		`class Circle {};
double area() { return 3.14; }
`,
	)
	writeTestFile(
		t,
		rustPath,
		`trait Describable {}
struct Point {}
fn main() {}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.PreScanPaths([]string{cPath, cppPath, rustPath})
	if err != nil {
		t.Fatalf("PreScanPaths() error = %v, want nil", err)
	}

	assertPrescanContains(t, got, "Point", cPath)
	assertPrescanContains(t, got, "add", cPath)
	assertPrescanContains(t, got, "Circle", cppPath)
	assertPrescanContains(t, got, "area", cppPath)
	assertPrescanContains(t, got, "Describable", rustPath)
	assertPrescanContains(t, got, "Point", rustPath)
	assertPrescanContains(t, got, "main", rustPath)
}

func TestDefaultEngineParsePathCTypedefAliasEmitsDedicatedEntities(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "types.c")
	writeTestFile(
		t,
		filePath,
		`typedef int my_int;
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

	typedefs, ok := got["typedefs"].([]map[string]any)
	if !ok {
		t.Fatalf("typedefs = %T, want []map[string]any", got["typedefs"])
	}
	want := []map[string]any{{
		"name":        "my_int",
		"line_number": 1,
		"end_line":    1,
		"lang":        "c",
		"type":        "int",
	}}
	if !reflect.DeepEqual(typedefs, want) {
		t.Fatalf("typedefs = %#v, want %#v", typedefs, want)
	}
}

func TestDefaultEngineParsePathCTypedefAliases(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "types.c")
	writeTestFile(
		t,
		filePath,
		`typedef enum {
    STATUS_OK = 0,
    STATUS_ERROR = 1,
    STATUS_NOT_FOUND = 2
} StatusCode;

typedef union {
    int intVal;
    float floatVal;
    char strVal[64];
} GenericValue;

typedef struct {
    int type;
    GenericValue value;
} TypedValue;
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

	assertNamedBucketContains(t, got, "enums", "StatusCode")
	assertNamedBucketContains(t, got, "unions", "GenericValue")
	assertNamedBucketContains(t, got, "structs", "TypedValue")
	assertNamedBucketContains(t, got, "typedefs", "StatusCode")
	assertNamedBucketContains(t, got, "typedefs", "GenericValue")
	assertNamedBucketContains(t, got, "typedefs", "TypedValue")
}

func TestDefaultEngineParsePathSystemsEmptyFiles(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	cPath := filepath.Join(repoRoot, "empty.c")
	cppPath := filepath.Join(repoRoot, "empty.cpp")
	rustPath := filepath.Join(repoRoot, "empty.rs")

	writeTestFile(t, cPath, "")
	writeTestFile(t, cppPath, "")
	writeTestFile(t, rustPath, "")

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	cResult, err := engine.ParsePath(repoRoot, cPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(c) error = %v, want nil", err)
	}
	cppResult, err := engine.ParsePath(repoRoot, cppPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(cpp) error = %v, want nil", err)
	}
	rustResult, err := engine.ParsePath(repoRoot, rustPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(rust) error = %v, want nil", err)
	}

	assertEmptyNamedBucket(t, cResult, "functions")
	assertEmptyNamedBucket(t, cppResult, "functions")
	assertEmptyNamedBucket(t, rustResult, "functions")
}
