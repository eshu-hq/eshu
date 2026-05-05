package terraformschema

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultSchemaDirPrefersEnvironmentOverride(t *testing.T) {
	t.Setenv("ESHU_TERRAFORM_SCHEMA_DIR", filepath.Join(t.TempDir(), "schemas"))

	if got, want := DefaultSchemaDir(), os.Getenv("ESHU_TERRAFORM_SCHEMA_DIR"); got != want {
		t.Fatalf("DefaultSchemaDir() = %q, want %q", got, want)
	}
}

func TestDefaultSchemaDirUsesGoOwnedPackagedSchemas(t *testing.T) {
	t.Setenv("ESHU_TERRAFORM_SCHEMA_DIR", "")

	got := filepath.ToSlash(DefaultSchemaDir())
	wantSuffix := "go/internal/terraformschema/schemas"
	if !strings.HasSuffix(got, wantSuffix) {
		t.Fatalf("DefaultSchemaDir() = %q, want suffix %q", got, wantSuffix)
	}
}

func TestDefaultSchemaDirContainsPackagedSchemas(t *testing.T) {
	t.Setenv("ESHU_TERRAFORM_SCHEMA_DIR", "")

	entries, err := os.ReadDir(DefaultSchemaDir())
	if err != nil {
		t.Fatalf("ReadDir(DefaultSchemaDir()) error = %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("DefaultSchemaDir() contains no packaged schemas")
	}
}
