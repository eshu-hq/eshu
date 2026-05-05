package parser

import (
	"os"
	"path/filepath"
	"runtime"
)

func ensureParentDirectory(path string) error {
	return os.MkdirAll(filepath.Dir(path), 0o755)
}

func osWriteFile(path string, body []byte) error {
	return os.WriteFile(path, body, 0o644)
}

func repoFixturePath(parts ...string) string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		panic("runtime.Caller(0) failed")
	}

	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
	elements := append([]string{root, "tests", "fixtures"}, parts...)
	return filepath.Join(elements...)
}
