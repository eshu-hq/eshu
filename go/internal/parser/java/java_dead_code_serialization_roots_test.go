// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package java_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

func TestDefaultEngineParsePathJavaMarksSerializationRuntimeHooks(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src/main/java/example/SerializationHooks.java")
	writeJavaTestFile(t, filePath, `package example;

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

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "readObject", "SerializationHooks"), "dead_code_root_kinds", "java.serialization_hook_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "writeObject", "SerializationHooks"), "dead_code_root_kinds", "java.serialization_hook_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "readResolve", "SerializationHooks"), "dead_code_root_kinds", "java.serialization_hook_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "writeReplace", "SerializationHooks"), "dead_code_root_kinds", "java.serialization_hook_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "readExternal", "ExternalizedState"), "dead_code_root_kinds", "java.externalizable_hook_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "writeExternal", "ExternalizedState"), "dead_code_root_kinds", "java.externalizable_hook_method")
	if _, ok := parsertest.AssertFunctionByNameAndClass(t, got, "helper", "SerializationHooks")["dead_code_root_kinds"]; ok {
		t.Fatalf("helper dead_code_root_kinds present, want absent")
	}
}

func TestDefaultEngineParsePathJavaDoesNotRootOrdinaryMethodsWithHookNames(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src/main/java/example/OrdinaryHooks.java")
	writeJavaTestFile(t, filePath, `package example;

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

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	for _, name := range []string{"readObject", "writeObject", "readResolve", "writeReplace", "readExternal", "writeExternal"} {
		if _, ok := parsertest.AssertFunctionByNameAndClass(t, got, name, "OrdinaryHooks")["dead_code_root_kinds"]; ok {
			t.Fatalf("%s dead_code_root_kinds present, want absent for ordinary method signature", name)
		}
	}
}
