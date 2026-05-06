package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathKotlinInterfaceMembersCarryTypeContext(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Service.kt")
	writeTestFile(
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

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertNamedBucketContains(t, got, "interfaces", "IService")
	assertFunctionWithClassContext(t, got, "execute", "IService")
	assertBucketContainsFieldValue(t, got, "function_calls", "full_name", "service.execute")
	assertBucketContainsFieldValue(t, got, "function_calls", "inferred_obj_type", "IService")
}
