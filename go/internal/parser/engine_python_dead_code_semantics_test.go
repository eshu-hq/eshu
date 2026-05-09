package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathPythonEmitsConstructorPropertyAndClassReferenceMetadata(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "models.py")
	writeTestFile(
		t,
		filePath,
		`import dataclasses

class BaseModel:
    @classmethod
    def from_dict(cls, obj):
        return cls(**obj)

@dataclasses.dataclass(frozen=True)
class S3Event(BaseModel):
    name: str

    def __post_init__(self):
        self.name = str(self.name)

    @property
    def object_url(self):
        return f"s3://{self.name}"

class Worker:
    def __str__(self):
        return "worker"

    def __init__(self):
        self.event = S3Event.from_dict({"name": "logs"})

    def run(self):
        return self.event.object_url

def main():
    worker = Worker()
    return worker.run()
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

	assertParserStringSliceFieldValue(t, assertBucketItemByName(t, got, "classes", "S3Event"), "bases", []string{"BaseModel"})
	assertParserStringSliceFieldValue(
		t,
		assertFunctionByName(t, got, "__post_init__"),
		"dead_code_root_kinds",
		[]string{"python.dataclass_post_init"},
	)
	assertParserStringSliceFieldValue(
		t,
		assertFunctionByName(t, got, "object_url"),
		"dead_code_root_kinds",
		[]string{"python.property_decorator"},
	)
	assertParserStringSliceFieldValue(
		t,
		assertFunctionByName(t, got, "__str__"),
		"dead_code_root_kinds",
		[]string{"python.dunder_method"},
	)
	constructorCall := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "Worker")
	assertStringFieldValue(t, constructorCall, "call_kind", "constructor_call")
	classReference := assertBucketItemByFieldValue(t, got, "function_calls", "call_kind", "python.class_reference")
	assertStringFieldValue(t, classReference, "name", "S3Event")
}
