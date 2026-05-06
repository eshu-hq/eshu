package reducer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestExtractCodeCallRowsResolvesKotlinThisReceiverCallsUsingClassContext(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "Worker.kt")

	writeFile := func(path string, contents string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatalf("WriteFile(%q) error = %v, want nil", path, err)
		}
	}

	writeFile(callerPath, `package comprehensive

class Worker {
    fun helper(): String = "ok"

    fun run(): String {
        return this.helper()
    }
}

fun helper(): String = "top-level"
`)

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
			case name == "run":
				function["end_line"] = 7
				function["uid"] = "content-entity:kotlin-run"
			case name == "helper" && classContext == "Worker":
				function["uid"] = "content-entity:kotlin-worker-helper"
			case name == "helper":
				function["uid"] = "content-entity:kotlin-top-level-helper"
			}
		}
	}
	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-kotlin",
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-kotlin",
				"relative_path":    "Worker.kt",
				"parsed_file_data": callerPayload,
			},
		},
	}

	entityIndex := buildCodeEntityIndex(envelopes)
	calls, ok := callerPayload["function_calls"].([]map[string]any)
	if !ok || len(calls) != 1 {
		t.Fatalf("function_calls = %#v, want exactly one Kotlin call", callerPayload["function_calls"])
	}
	if got := resolveSameFileCalleeEntityID(entityIndex, callerPath, "Worker.kt", calls[0]); got == "" {
		t.Fatalf(
			"resolved same-file callee: %q (candidates=%v, names=%v)",
			got,
			entityIndex.uniqueNameByPath,
			codeCallExactCandidateNames(calls[0], "kotlin"),
		)
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if got, want := rows[0]["callee_entity_id"], "content-entity:kotlin-worker-helper"; got != want {
		t.Fatalf("callee_entity_id = %#v, want %#v", got, want)
	}
}

func TestExtractCodeCallRowsResolvesKotlinTypedReceiverCallsUsingInferredObjectType(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "Usage.kt")

	writeFile := func(path string, contents string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatalf("WriteFile(%q) error = %v, want nil", path, err)
		}
	}

	writeFile(callerPath, `package comprehensive

class User {
    fun greet(): String = "hi"
}

class Calculator {
    fun add(a: Int, b: Int): Int = a + b
}

fun String.removeSpaces(): String = this.replace(" ", "")

fun usage(): String {
    val user = User()
    val calculator = Calculator()
    val text = "Hello World"
    calculator.add(5, 10)
    user.greet()
    return text.removeSpaces()
}
`)

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
			case name == "usage":
				function["end_line"] = 19
				function["uid"] = "content-entity:kotlin-usage"
			case name == "greet" && classContext == "User":
				function["uid"] = "content-entity:kotlin-user-greet"
			case name == "add" && classContext == "Calculator":
				function["uid"] = "content-entity:kotlin-calculator-add"
			case name == "removeSpaces" && classContext == "String":
				function["uid"] = "content-entity:kotlin-string-remove-spaces"
			}
		}
	}

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-kotlin",
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-kotlin",
				"relative_path":    "Usage.kt",
				"parsed_file_data": callerPayload,
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if len(rows) != 3 {
		t.Fatalf("len(rows) = %d, want 3; rows=%#v; function_calls=%#v", len(rows), rows, callerPayload["function_calls"])
	}

	want := map[string]string{
		"content-entity:kotlin-calculator-add":       "calculator.add",
		"content-entity:kotlin-user-greet":           "user.greet",
		"content-entity:kotlin-string-remove-spaces": "text.removeSpaces",
	}
	for _, row := range rows {
		calleeID, _ := row["callee_entity_id"].(string)
		fullName, _ := row["full_name"].(string)
		if expectedFullName, ok := want[calleeID]; ok {
			if fullName != expectedFullName {
				t.Fatalf("full_name = %#v, want %#v for callee %#v", fullName, expectedFullName, calleeID)
			}
			delete(want, calleeID)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing expected callee rows: %#v; rows=%#v", want, rows)
	}
}

func TestExtractCodeCallRowsResolvesKotlinCastReceiverCallsUsingInferredObjectType(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "Usage.kt")

	writeFile := func(path string, contents string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatalf("WriteFile(%q) error = %v, want nil", path, err)
		}
	}

	writeFile(callerPath, `package comprehensive

class Service {
    fun info(): String = "ok"
}

fun usage(any: Any): String {
    val service = any as Service
    return service.info()
}
`)

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
			case name == "usage":
				function["end_line"] = 10
				function["uid"] = "content-entity:kotlin-usage"
			case name == "info" && classContext == "Service":
				function["uid"] = "content-entity:kotlin-service-info"
			}
		}
	}

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-kotlin",
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-kotlin",
				"relative_path":    "Usage.kt",
				"parsed_file_data": callerPayload,
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1; rows=%#v; function_calls=%#v", len(rows), rows, callerPayload["function_calls"])
	}

	if got, want := rows[0]["callee_entity_id"], "content-entity:kotlin-service-info"; got != want {
		t.Fatalf("callee_entity_id = %#v, want %#v", got, want)
	}
	if got, want := rows[0]["full_name"], "service.info"; got != want {
		t.Fatalf("full_name = %#v, want %#v", got, want)
	}
}

func TestExtractCodeCallRowsResolvesKotlinObjectReceiverCallsUsingInferredObjectType(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "Usage.kt")

	writeFile := func(path string, contents string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatalf("WriteFile(%q) error = %v, want nil", path, err)
		}
	}

	writeFile(callerPath, `package comprehensive

object AppConfig {
    fun isProduction(): Boolean = true
}

fun usage(): Boolean {
    return AppConfig.isProduction()
}
`)

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
			case name == "usage":
				function["end_line"] = 8
				function["uid"] = "content-entity:kotlin-usage"
			case name == "isProduction" && classContext == "AppConfig":
				function["uid"] = "content-entity:kotlin-app-config-is-production"
			}
		}
	}

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-kotlin",
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-kotlin",
				"relative_path":    "Usage.kt",
				"parsed_file_data": callerPayload,
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1; rows=%#v; function_calls=%#v", len(rows), rows, callerPayload["function_calls"])
	}

	if got, want := rows[0]["callee_entity_id"], "content-entity:kotlin-app-config-is-production"; got != want {
		t.Fatalf("callee_entity_id = %#v, want %#v", got, want)
	}
	if got, want := rows[0]["full_name"], "AppConfig.isProduction"; got != want {
		t.Fatalf("full_name = %#v, want %#v", got, want)
	}
}

func TestExtractCodeCallRowsResolvesKotlinCompanionObjectReceiverCallsUsingInferredObjectType(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "Usage.kt")

	writeFile := func(path string, contents string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatalf("WriteFile(%q) error = %v, want nil", path, err)
		}
	}

	writeFile(callerPath, `package comprehensive

data class Person(val name: String) {
    companion object {
        fun create(name: String): Person = Person(name)
    }
}

fun usage(): String {
    val person = Person.create("Ada")
    return person.name
}
`)

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
			case name == "usage":
				function["end_line"] = 10
				function["uid"] = "content-entity:kotlin-usage"
			case name == "create" && classContext == "Person":
				function["uid"] = "content-entity:kotlin-person-create"
			}
		}
	}

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-kotlin",
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-kotlin",
				"relative_path":    "Usage.kt",
				"parsed_file_data": callerPayload,
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1; rows=%#v; function_calls=%#v", len(rows), rows, callerPayload["function_calls"])
	}

	if got, want := rows[0]["callee_entity_id"], "content-entity:kotlin-person-create"; got != want {
		t.Fatalf("callee_entity_id = %#v, want %#v", got, want)
	}
	if got, want := rows[0]["full_name"], "Person.create"; got != want {
		t.Fatalf("full_name = %#v, want %#v", got, want)
	}
}

func TestExtractCodeCallRowsResolvesKotlinGenericNullableReceiverCallsUsingInferredObjectType(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "Usage.kt")

	writeFile := func(path string, contents string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatalf("WriteFile(%q) error = %v, want nil", path, err)
		}
	}

	writeFile(callerPath, `package comprehensive

class Box<T>(private val value: T) {
    fun unwrap(): T = value
}

class Service {
    fun info(): String = "ok"
}

fun createBox(): Box<Service>? = Box(Service())

fun usage(): String {
    val typedBox: Box<Service>? = Box(Service())
    val returnedBox = createBox()
    return typedBox.unwrap().info() + returnedBox.unwrap().info()
}
`)

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
			case name == "usage":
				function["end_line"] = 17
				function["uid"] = "content-entity:kotlin-usage"
			case name == "unwrap" && classContext == "Box":
				function["uid"] = "content-entity:kotlin-box-unwrap"
			case name == "info" && classContext == "Service":
				function["uid"] = "content-entity:kotlin-service-info"
			}
		}
	}

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-kotlin",
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-kotlin",
				"relative_path":    "Usage.kt",
				"parsed_file_data": callerPayload,
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2; rows=%#v; function_calls=%#v", len(rows), rows, callerPayload["function_calls"])
	}

	want := map[string]string{
		"typedBox.unwrap":        "content-entity:kotlin-box-unwrap",
		"typedBox.unwrap().info": "content-entity:kotlin-service-info",
	}
	for _, row := range rows {
		calleeID, _ := row["callee_entity_id"].(string)
		fullName, _ := row["full_name"].(string)
		if expectedCalleeID, ok := want[fullName]; ok {
			if calleeID != expectedCalleeID {
				t.Fatalf("callee_entity_id = %#v, want %#v for full_name %#v", calleeID, expectedCalleeID, fullName)
			}
			delete(want, fullName)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing expected callee rows: %#v; rows=%#v", want, rows)
	}
}

func TestExtractCodeCallRowsResolvesKotlinDirectCastReceiverCallsUsingInferredObjectType(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "Usage.kt")

	writeFile := func(path string, contents string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatalf("WriteFile(%q) error = %v, want nil", path, err)
		}
	}

	writeFile(callerPath, `package comprehensive

class Service {
    fun info(): String = "ok"
}

fun usage(any: Any): String {
    return (any as Service).info()
}
`)

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
			case name == "usage":
				function["end_line"] = 9
				function["uid"] = "content-entity:kotlin-usage"
			case name == "info" && classContext == "Service":
				function["uid"] = "content-entity:kotlin-service-info"
			}
		}
	}

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-kotlin",
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-kotlin",
				"relative_path":    "Usage.kt",
				"parsed_file_data": callerPayload,
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1; rows=%#v; function_calls=%#v", len(rows), rows, callerPayload["function_calls"])
	}

	if got, want := rows[0]["callee_entity_id"], "content-entity:kotlin-service-info"; got != want {
		t.Fatalf("callee_entity_id = %#v, want %#v", got, want)
	}
	if got, want := rows[0]["full_name"], "(any as Service).info"; got != want {
		t.Fatalf("full_name = %#v, want %#v", got, want)
	}
}

func TestExtractCodeCallRowsResolvesKotlinTypedInfixCallsUsingInferredObjectType(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "Usage.kt")

	writeFile := func(path string, contents string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatalf("WriteFile(%q) error = %v, want nil", path, err)
		}
	}

	writeFile(callerPath, `package comprehensive

class Calculator {
    fun add(a: Int, b: Int): Int = a + b
}

fun usage(): Int {
    val calc = Calculator()
    return calc add 5
}
`)

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
			case name == "usage":
				function["end_line"] = 9
				function["uid"] = "content-entity:kotlin-usage"
			case name == "add" && classContext == "Calculator":
				function["uid"] = "content-entity:kotlin-calculator-add"
			}
		}
	}

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-kotlin",
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-kotlin",
				"relative_path":    "Usage.kt",
				"parsed_file_data": callerPayload,
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1; rows=%#v; function_calls=%#v", len(rows), rows, callerPayload["function_calls"])
	}

	if got, want := rows[0]["callee_entity_id"], "content-entity:kotlin-calculator-add"; got != want {
		t.Fatalf("callee_entity_id = %#v, want %#v", got, want)
	}
	if got, want := rows[0]["full_name"], "calc add"; got != want {
		t.Fatalf("full_name = %#v, want %#v", got, want)
	}
}

func TestExtractCodeCallRowsResolvesKotlinTypedPropertyAliasChainsUsingInferredObjectType(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "Worker.kt")

	writeFile := func(path string, contents string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatalf("WriteFile(%q) error = %v, want nil", path, err)
		}
	}

	writeFile(callerPath, `package comprehensive

class Service {
    fun info(): String = "ok"
}

class Worker {
    private val service: Service = Service()

    fun run(): String {
        val logger = service
        val active = logger
        return active.info()
    }
}
`)

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
			case name == "run":
				function["end_line"] = 13
				function["uid"] = "content-entity:kotlin-worker-run"
			case name == "info" && classContext == "Service":
				function["uid"] = "content-entity:kotlin-service-info"
			}
		}
	}

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-kotlin",
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-kotlin",
				"relative_path":    "Worker.kt",
				"parsed_file_data": callerPayload,
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1; rows=%#v; function_calls=%#v", len(rows), rows, callerPayload["function_calls"])
	}

	if got, want := rows[0]["callee_entity_id"], "content-entity:kotlin-service-info"; got != want {
		t.Fatalf("callee_entity_id = %#v, want %#v", got, want)
	}
	if got, want := rows[0]["full_name"], "active.info"; got != want {
		t.Fatalf("full_name = %#v, want %#v", got, want)
	}
}

func TestExtractCodeCallRowsResolvesKotlinTypedPropertyChainCallsUsingInferredObjectType(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "Usage.kt")

	writeFile := func(path string, contents string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatalf("WriteFile(%q) error = %v, want nil", path, err)
		}
	}

	writeFile(callerPath, `package comprehensive

class Service {
    fun info(): String = "ok"
}

class Session {
    val service: Service = Service()
}

fun usage(): String {
    val session = Session()
    return session.service.info()
}
`)

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
			case name == "usage":
				function["end_line"] = 13
				function["uid"] = "content-entity:kotlin-usage"
			case name == "info" && classContext == "Service":
				function["uid"] = "content-entity:kotlin-service-info"
			}
		}
	}

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-kotlin",
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-kotlin",
				"relative_path":    "Usage.kt",
				"parsed_file_data": callerPayload,
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1; rows=%#v; function_calls=%#v", len(rows), rows, callerPayload["function_calls"])
	}

	if got, want := rows[0]["callee_entity_id"], "content-entity:kotlin-service-info"; got != want {
		t.Fatalf("callee_entity_id = %#v, want %#v", got, want)
	}
	if got, want := rows[0]["full_name"], "session.service.info"; got != want {
		t.Fatalf("full_name = %#v, want %#v", got, want)
	}
}
