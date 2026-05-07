package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/eshu-hq/eshu/go/internal/eshulocal"
)

// resetLocalAuthoritativeState removes per-run stores that local_authoritative
// rebuilds from the workspace source tree on every owner start.
func resetLocalAuthoritativeState(layout eshulocal.Layout) error {
	paths := []string{
		filepath.Join(layout.PostgresDir, "data"),
		filepath.Join(layout.PostgresDir, "runtime"),
		filepath.Join(layout.GraphDir, "nornicdb"),
	}
	for _, path := range paths {
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("reset local authoritative state %q: %w", path, err)
		}
	}
	return nil
}
