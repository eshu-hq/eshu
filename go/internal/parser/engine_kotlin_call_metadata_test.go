package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathKotlinThisReceiverCallCarriesClassContext(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Worker.kt")
	writeTestFile(
		t,
		filePath,
		`package comprehensive

class Worker {
    fun helper(): String = "ok"

    fun run(): String {
        return this.helper()
    }
}

fun helper(): String = "top-level"
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

	items, ok := got["function_calls"].([]map[string]any)
	if !ok {
		t.Fatalf("function_calls = %T, want []map[string]any", got["function_calls"])
	}
	for _, item := range items {
		fullName, _ := item["full_name"].(string)
		if fullName != "this.helper" {
			continue
		}
		assertStringFieldValue(t, item, "class_context", "Worker")
		return
	}
	t.Fatalf("function_calls missing full_name=%q in %#v", "this.helper", items)
}

func TestDefaultEngineParsePathKotlinInfersLocalReceiverTypesForDotCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Usage.kt")
	writeTestFile(
		t,
		filePath,
		`package comprehensive

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

	items, ok := got["function_calls"].([]map[string]any)
	if !ok {
		t.Fatalf("function_calls = %T, want []map[string]any", got["function_calls"])
	}

	want := map[string]string{
		"calculator.add":    "Calculator",
		"user.greet":        "User",
		"text.removeSpaces": "String",
	}
	for _, item := range items {
		fullName, _ := item["full_name"].(string)
		if wantType, ok := want[fullName]; ok {
			assertStringFieldValue(t, item, "inferred_obj_type", wantType)
			delete(want, fullName)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing inferred receiver calls: %#v in %#v", want, items)
	}

	extension := assertBucketItemByName(t, got, "functions", "removeSpaces")
	assertStringFieldValue(t, extension, "class_context", "String")
}

func TestDefaultEngineParsePathKotlinInfersCastReceiverTypesForDotCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Usage.kt")
	writeTestFile(
		t,
		filePath,
		`package comprehensive

class Service {
    fun info(): String = "ok"
}

fun usage(any: Any): String {
    val service = any as Service
    return service.info()
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

	items, ok := got["function_calls"].([]map[string]any)
	if !ok {
		t.Fatalf("function_calls = %T, want []map[string]any", got["function_calls"])
	}

	for _, item := range items {
		fullName, _ := item["full_name"].(string)
		if fullName != "service.info" {
			continue
		}
		assertStringFieldValue(t, item, "inferred_obj_type", "Service")
		return
	}
	t.Fatalf("function_calls missing full_name=%q in %#v", "service.info", items)
}

func TestDefaultEngineParsePathKotlinInfersDirectCastReceiverTypesForDotCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Usage.kt")
	writeTestFile(
		t,
		filePath,
		`package comprehensive

class Service {
    fun info(): String = "ok"
}

fun usage(any: Any): String {
    return (any as Service).info()
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

	items, ok := got["function_calls"].([]map[string]any)
	if !ok {
		t.Fatalf("function_calls = %T, want []map[string]any", got["function_calls"])
	}

	for _, item := range items {
		fullName, _ := item["full_name"].(string)
		if fullName != "(any as Service).info" {
			continue
		}
		assertStringFieldValue(t, item, "inferred_obj_type", "Service")
		return
	}
	t.Fatalf("function_calls missing full_name=%q in %#v", "(any as Service).info", items)
}

func TestDefaultEngineParsePathKotlinInfersLocalReceiverTypesForInfixCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Usage.kt")
	writeTestFile(
		t,
		filePath,
		`package comprehensive

class Calculator {
    fun add(a: Int, b: Int): Int = a + b
}

fun usage(): Int {
    val calc = Calculator()
    return calc add 5
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

	items, ok := got["function_calls"].([]map[string]any)
	if !ok {
		t.Fatalf("function_calls = %T, want []map[string]any", got["function_calls"])
	}

	for _, item := range items {
		fullName, _ := item["full_name"].(string)
		if fullName != "calc add" {
			continue
		}
		assertStringFieldValue(t, item, "name", "add")
		assertStringFieldValue(t, item, "inferred_obj_type", "Calculator")
		return
	}
	t.Fatalf("function_calls missing full_name=%q in %#v", "calc add", items)
}

func TestDefaultEngineParsePathKotlinInfersTypedPropertyAliasChainsForDotCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Worker.kt")
	writeTestFile(
		t,
		filePath,
		`package comprehensive

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

	items, ok := got["function_calls"].([]map[string]any)
	if !ok {
		t.Fatalf("function_calls = %T, want []map[string]any", got["function_calls"])
	}
	for _, item := range items {
		fullName, _ := item["full_name"].(string)
		if fullName != "active.info" {
			continue
		}
		assertStringFieldValue(t, item, "inferred_obj_type", "Service")
		return
	}
	t.Fatalf("function_calls missing full_name=%q in %#v", "active.info", items)
}

func TestDefaultEngineParsePathKotlinInfersObjectReceiverTypesForDotCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Usage.kt")
	writeTestFile(
		t,
		filePath,
		`package comprehensive

object AppConfig {
    fun isProduction(): Boolean = true
}

fun usage(): Boolean {
    return AppConfig.isProduction()
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

	items, ok := got["function_calls"].([]map[string]any)
	if !ok {
		t.Fatalf("function_calls = %T, want []map[string]any", got["function_calls"])
	}

	for _, item := range items {
		fullName, _ := item["full_name"].(string)
		if fullName != "AppConfig.isProduction" {
			continue
		}
		assertStringFieldValue(t, item, "inferred_obj_type", "AppConfig")
		return
	}
	t.Fatalf("function_calls missing full_name=%q in %#v", "AppConfig.isProduction", items)
}

func TestDefaultEngineParsePathKotlinInfersCompanionObjectReceiverTypesForDotCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Usage.kt")
	writeTestFile(
		t,
		filePath,
		`package comprehensive

data class Person(val name: String) {
    companion object {
        fun create(name: String): Person = Person(name)
    }
}

fun usage(): String {
    val person = Person.create("Ada")
    return person.name
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

	items, ok := got["function_calls"].([]map[string]any)
	if !ok {
		t.Fatalf("function_calls = %T, want []map[string]any", got["function_calls"])
	}

	want := map[string]string{
		"Person.create": "Person",
	}
	for _, item := range items {
		fullName, _ := item["full_name"].(string)
		if wantType, ok := want[fullName]; ok {
			assertStringFieldValue(t, item, "inferred_obj_type", wantType)
			delete(want, fullName)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing inferred receiver calls: %#v in %#v", want, items)
	}
}

func TestDefaultEngineParsePathKotlinInfersGenericNullableReceiverTypesForDotCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Usage.kt")
	writeTestFile(
		t,
		filePath,
		`package comprehensive

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

	items, ok := got["function_calls"].([]map[string]any)
	if !ok {
		t.Fatalf("function_calls = %T, want []map[string]any", got["function_calls"])
	}

	want := map[string]string{
		"typedBox.unwrap":           "Box",
		"typedBox.unwrap().info":    "Service",
		"returnedBox.unwrap":        "Box",
		"returnedBox.unwrap().info": "Service",
	}
	for _, item := range items {
		fullName, _ := item["full_name"].(string)
		if wantType, ok := want[fullName]; ok {
			assertStringFieldValue(t, item, "inferred_obj_type", wantType)
			delete(want, fullName)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing inferred receiver calls: %#v in %#v", want, items)
	}
}

func TestDefaultEngineParsePathKotlinInfersTypedPropertyChainCallsForDotCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Usage.kt")
	writeTestFile(
		t,
		filePath,
		`package comprehensive

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

	items, ok := got["function_calls"].([]map[string]any)
	if !ok {
		t.Fatalf("function_calls = %T, want []map[string]any", got["function_calls"])
	}

	for _, item := range items {
		fullName, _ := item["full_name"].(string)
		if fullName != "session.service.info" {
			continue
		}
		assertStringFieldValue(t, item, "inferred_obj_type", "Service")
		return
	}
	t.Fatalf("function_calls missing full_name=%q in %#v", "session.service.info", items)
}

func TestDefaultEngineParsePathKotlinInfersSafeCallReceiverChainsForDotCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Usage.kt")
	writeTestFile(
		t,
		filePath,
		`package comprehensive

class Service {
    fun info(): String = "ok"
}

class Session {
    val service: Service = Service()
}

fun usage(): String {
    val session = Session()
    return session?.service?.info()
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

	items, ok := got["function_calls"].([]map[string]any)
	if !ok {
		t.Fatalf("function_calls = %T, want []map[string]any", got["function_calls"])
	}

	for _, item := range items {
		fullName, _ := item["full_name"].(string)
		if fullName != "session.service.info" {
			continue
		}
		assertStringFieldValue(t, item, "inferred_obj_type", "Service")
		return
	}
	t.Fatalf("function_calls missing full_name=%q in %#v", "session.service.info", items)
}

func TestDefaultEngineParsePathKotlinInfersSafeCallAliasChainsForDotCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Usage.kt")
	writeTestFile(
		t,
		filePath,
		`package comprehensive

class Service {
    fun info(): String = "ok"
}

class Session {
    val service: Service = Service()
}

fun usage(): String {
    val session = Session()
    val provider = session?.service
    return provider.info()
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

	items, ok := got["function_calls"].([]map[string]any)
	if !ok {
		t.Fatalf("function_calls = %T, want []map[string]any", got["function_calls"])
	}

	for _, item := range items {
		fullName, _ := item["full_name"].(string)
		if fullName != "provider.info" {
			continue
		}
		assertStringFieldValue(t, item, "inferred_obj_type", "Service")
		return
	}
	t.Fatalf("function_calls missing full_name=%q in %#v", "provider.info", items)
}

func TestDefaultEngineParsePathKotlinInfersDottedPropertyAliasChainsForDotCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Usage.kt")
	writeTestFile(
		t,
		filePath,
		`package comprehensive

class Service {
    fun info(): String = "ok"
}

class Session {
    val service: Service = Service()
}

fun usage(): String {
    val session = Session()
    val provider = session.service
    return provider.info()
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

	items, ok := got["function_calls"].([]map[string]any)
	if !ok {
		t.Fatalf("function_calls = %T, want []map[string]any", got["function_calls"])
	}

	for _, item := range items {
		fullName, _ := item["full_name"].(string)
		if fullName != "provider.info" {
			continue
		}
		assertStringFieldValue(t, item, "inferred_obj_type", "Service")
		return
	}
	t.Fatalf("function_calls missing full_name=%q in %#v", "provider.info", items)
}

func TestDefaultEngineParsePathKotlinInfersPrimaryConstructorPropertyReceiverCallsForDotCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Worker.kt")
	writeTestFile(
		t,
		filePath,
		`package comprehensive

class Service {
    fun info(): String = "ok"
}

class Worker(private val service: Service) {
    fun run(): String {
        return service.info()
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

	items, ok := got["function_calls"].([]map[string]any)
	if !ok {
		t.Fatalf("function_calls = %T, want []map[string]any", got["function_calls"])
	}

	for _, item := range items {
		fullName, _ := item["full_name"].(string)
		if fullName != "service.info" {
			continue
		}
		assertStringFieldValue(t, item, "inferred_obj_type", "Service")
		return
	}
	t.Fatalf("function_calls missing full_name=%q in %#v", "service.info", items)
}
