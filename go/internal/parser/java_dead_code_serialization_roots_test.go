package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathJavaMarksSerializationRuntimeHooks(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src/main/java/example/SerializationHooks.java")
	writeTestFile(t, filePath, `package example;

import java.io.ObjectInput;
import java.io.ObjectInputStream;
import java.io.ObjectOutput;
import java.io.ObjectOutputStream;

final class SerializationHooks {
    private void readObject(ObjectInputStream input) {
    }

    private void writeObject(ObjectOutputStream output) {
    }

    private Object readResolve() {
        return this;
    }

    private Object writeReplace() {
        return this;
    }

    void helper() {
    }
}

final class ExternalizedState {
    public void readExternal(ObjectInput input) {
    }

    public void writeExternal(ObjectOutput output) {
    }
}
`)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "readObject", "SerializationHooks"), "dead_code_root_kinds", "java.serialization_hook_method")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "writeObject", "SerializationHooks"), "dead_code_root_kinds", "java.serialization_hook_method")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "readResolve", "SerializationHooks"), "dead_code_root_kinds", "java.serialization_hook_method")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "writeReplace", "SerializationHooks"), "dead_code_root_kinds", "java.serialization_hook_method")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "readExternal", "ExternalizedState"), "dead_code_root_kinds", "java.externalizable_hook_method")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "writeExternal", "ExternalizedState"), "dead_code_root_kinds", "java.externalizable_hook_method")
	if _, ok := assertFunctionByNameAndClass(t, got, "helper", "SerializationHooks")["dead_code_root_kinds"]; ok {
		t.Fatalf("helper dead_code_root_kinds present, want absent")
	}
}

func TestDefaultEngineParsePathJavaDoesNotRootOrdinaryMethodsWithHookNames(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src/main/java/example/OrdinaryHooks.java")
	writeTestFile(t, filePath, `package example;

import java.io.ObjectInput;
import java.io.ObjectOutput;
import java.io.ObjectOutputStream;

final class OrdinaryHooks {
    void readObject(String input) {
    }

    void writeObject(ObjectOutput output) {
    }

    Object readResolve(String version) {
        return this;
    }

    Object writeReplace(int version) {
        return this;
    }

    void readExternal(ObjectOutput output) {
    }

    void writeExternal(ObjectInput input) {
    }

    void writeExternal(ObjectOutputStream output, String extra) {
    }
}
`)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	for _, name := range []string{"readObject", "writeObject", "readResolve", "writeReplace", "readExternal", "writeExternal"} {
		if _, ok := assertFunctionByNameAndClass(t, got, name, "OrdinaryHooks")["dead_code_root_kinds"]; ok {
			t.Fatalf("%s dead_code_root_kinds present, want absent for ordinary method signature", name)
		}
	}
}
