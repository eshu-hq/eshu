package terraformschema

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// DefaultSchemaDir returns the canonical packaged Terraform schema directory
// for Go-owned runtime extraction. An explicit environment override wins.
func DefaultSchemaDir() string {
	if envDir := strings.TrimSpace(os.Getenv("ESHU_TERRAFORM_SCHEMA_DIR")); envDir != "" {
		return envDir
	}
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "schemas"))
}
