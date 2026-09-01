// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package java_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

func TestDefaultEngineParsePathJavaInfersEnhancedForReceiverType(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src/main/java/example/ProtobufPluginAction.java")
	writeJavaTestFile(t, filePath, `package example;

public class ProtobufPluginAction {
    private VersionAlignment versionAlignmentFor(DependencyResolveDetails details) {
        for (VersionAlignment alignment : versionAlignment) {
            if (alignment.accepts(details)) {
                return alignment;
            }
        }
        return null;
    }

    private record VersionAlignment(Dependency target, Dependency source) {
        boolean accepts(DependencyResolveDetails details) {
            return true;
        }
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

	call := parsertest.AssertBucketItemByFieldValue(t, got, "function_calls", "full_name", "alignment.accepts")
	parsertest.AssertStringFieldValue(t, call, "inferred_obj_type", "VersionAlignment")
}

func TestDefaultEngineParsePathJavaAddsOuterClassContextToUnqualifiedCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src/main/java/example/BootJar.java")
	writeJavaTestFile(t, filePath, `package example;

public class BootJar {
    protected ZipCompression resolveZipCompression(FileCopyDetails details) {
        return ZipCompression.STORED;
    }

    private final class ZipCompressionResolver implements Function<FileCopyDetails, ZipCompression> {
        public ZipCompression apply(FileCopyDetails details) {
            return resolveZipCompression(details);
        }
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

	call := parsertest.AssertBucketItemByFieldValue(t, got, "function_calls", "name", "resolveZipCompression")
	parsertest.AssertStringFieldValue(t, call, "class_context", "ZipCompressionResolver")
	parsertest.AssertStringSliceContains(t, call, "enclosing_class_contexts", "BootJar")
}

func TestDefaultEngineParsePathJavaInfersExplicitOuterThisFieldReceiver(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src/main/java/example/BootZipCopyAction.java")
	writeJavaTestFile(t, filePath, `package example;

public class BootZipCopyAction {
    private final LayerResolver layerResolver;

    private final class Processor {
        void process(FileCopyDetails details) {
            Layer layer = BootZipCopyAction.this.layerResolver.getLayer(details);
        }
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

	call := parsertest.AssertBucketItemByFieldValue(t, got, "function_calls", "full_name", "BootZipCopyAction.this.layerResolver.getLayer")
	parsertest.AssertStringFieldValue(t, call, "inferred_obj_type", "LayerResolver")
}

func TestDefaultEngineParsePathJavaInfersUnqualifiedArgumentReturnType(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src/main/java/example/LayerResolver.java")
	writeJavaTestFile(t, filePath, `package example;

class LayerResolver {
    Layer getLayer(FileCopyDetails details) {
        return getLayer(asLibrary(details));
    }

    Layer getLayer(Library library) {
        return null;
    }

    private Library asLibrary(FileCopyDetails details) {
        return null;
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

	call := parsertest.AssertBucketItemByFieldValue(t, got, "function_calls", "full_name", "getLayer")
	parsertest.AssertStringSliceContains(t, call, "argument_types", "Library")
}
