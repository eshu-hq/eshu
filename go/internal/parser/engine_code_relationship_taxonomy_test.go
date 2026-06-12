package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathJavaEmitsImplementationAndConstructorIntent(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Service.java")
	writeTestFile(
		t,
		filePath,
		`package demo;

class BaseService {
}

interface RunnableService {
}

interface CloseableService {
}

public class Service extends BaseService implements RunnableService, CloseableService {
    public Service() {
    }

    public static Service build() {
        return new Service();
    }
}

public class GenericService<T extends CloseableService> implements RunnableService {
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

	service := assertBucketItemByName(t, got, "classes", "Service")
	assertStringSliceFieldValue(t, service, "bases", []string{"BaseService"})
	assertStringSliceFieldValue(t, service, "implemented_interfaces", []string{"CloseableService", "RunnableService"})

	genericService := assertBucketItemByName(t, got, "classes", "GenericService")
	if value, ok := genericService["bases"]; ok {
		t.Fatalf("GenericService bases = %#v, want no class bases from generic type parameter bounds", value)
	}
	assertStringSliceFieldValue(t, genericService, "implemented_interfaces", []string{"RunnableService"})

	call := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "Service")
	assertStringFieldValue(t, call, "call_kind", "constructor_call")
}

func TestDefaultEngineParsePathKotlinEmitsImplementationIntent(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Service.kt")
	writeTestFile(
		t,
		filePath,
		`package demo

interface RunnableService {
    fun execute(): String
}

interface AuditableService

class Service : RunnableService, AuditableService {
    override fun execute(): String = "ok"
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

	service := assertBucketItemByName(t, got, "classes", "Service")
	assertStringSliceFieldValue(t, service, "implemented_interfaces", []string{"AuditableService", "RunnableService"})
}

func TestDefaultEngineParsePathTypeScriptEmitsImplementationIntent(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "service.ts")
	writeTestFile(
		t,
		filePath,
		`interface RunnableService {
  execute(): string;
}

interface AuditableService {
  audit(): void;
}

class Service implements RunnableService, AuditableService {
  execute(): string {
    return "ok";
  }

  audit(): void {}
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

	service := assertBucketItemByName(t, got, "classes", "Service")
	assertStringSliceFieldValue(t, service, "implemented_interfaces", []string{"AuditableService", "RunnableService"})
}

func TestDefaultEngineParsePathCSharpEmitsImplementationIntent(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Service.cs")
	writeTestFile(
		t,
		filePath,
		`public class BaseService {
}

public interface IRunnableService {
}

public interface IAuditableService {
}

public class Service : BaseService, IRunnableService, IAuditableService {
}

public struct Worker : IRunnableService {
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

	service := assertBucketItemByName(t, got, "classes", "Service")
	assertStringSliceFieldValue(t, service, "bases", []string{"BaseService", "IAuditableService", "IRunnableService"})
	assertStringSliceFieldValue(t, service, "implemented_interfaces", []string{"IAuditableService", "IRunnableService"})

	worker := assertBucketItemByName(t, got, "structs", "Worker")
	assertStringSliceFieldValue(t, worker, "implemented_interfaces", []string{"IRunnableService"})
}
