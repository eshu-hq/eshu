// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kotlin_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

func TestDefaultEngineParsePathKotlinInterfaceMembersCarryTypeContext(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Service.kt")
	writeKotlinTestFile(
		t,
		filePath,
		`package comprehensive

interface IService {
    fun execute(): String = "ok"
}

class Service : IService {
    override fun execute(): String = "ok"
}

fun createService(): IService = Service()

fun usage(): String {
    val service = createService()
    return service.execute()
}
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	parsertest.AssertNamedBucketContains(t, got, "interfaces", "IService")
	parsertest.AssertFunctionByNameAndClass(t, got, "execute", "IService")
	parsertest.AssertBucketContainsFieldValue(t, got, "function_calls", "full_name", "service.execute")
	parsertest.AssertBucketContainsFieldValue(t, got, "function_calls", "inferred_obj_type", "IService")
}
