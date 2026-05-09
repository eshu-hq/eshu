package terraformstate_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
)

func TestLocalStateSourceOpensExactFileStream(t *testing.T) {
	t.Parallel()

	path := writeStateFile(t, `{"serial":17}`)
	source, err := terraformstate.NewLocalStateSource(terraformstate.LocalSourceConfig{
		Path:     path,
		MaxBytes: 1024,
	})
	if err != nil {
		t.Fatalf("NewLocalStateSource() error = %v, want nil", err)
	}

	reader, metadata, err := source.Open(context.Background())
	if err != nil {
		t.Fatalf("Open() error = %v, want nil", err)
	}
	defer reader.Close()

	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error = %v, want nil", err)
	}
	if got, want := string(body), `{"serial":17}`; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
	if got, want := source.Identity().BackendKind, terraformstate.BackendLocal; got != want {
		t.Fatalf("BackendKind = %q, want %q", got, want)
	}
	if got := source.Identity().Locator; got != path {
		t.Fatalf("Locator = %q, want %q", got, path)
	}
	if got := metadata.Size; got != int64(len(body)) {
		t.Fatalf("metadata.Size = %d, want %d", got, len(body))
	}
}

func TestLocalStateSourceRejectsNonExactSources(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
	}{
		{name: "blank path", path: " "},
		{name: "relative path", path: "terraform.tfstate"},
		{name: "directory", path: t.TempDir()},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := terraformstate.NewLocalStateSource(terraformstate.LocalSourceConfig{
				Path:     test.path,
				MaxBytes: 1024,
			})
			if err == nil {
				t.Fatal("NewLocalStateSource() error = nil, want non-nil")
			}
		})
	}
}

func TestLocalStateSourceRejectsFilesAboveSizeCeiling(t *testing.T) {
	t.Parallel()

	path := writeStateFile(t, strings.Repeat("x", 8))
	source, err := terraformstate.NewLocalStateSource(terraformstate.LocalSourceConfig{
		Path:     path,
		MaxBytes: 4,
	})
	if err != nil {
		t.Fatalf("NewLocalStateSource() error = %v, want nil", err)
	}

	_, _, err = source.Open(context.Background())
	if !errors.Is(err, terraformstate.ErrStateTooLarge) {
		t.Fatalf("Open() error = %v, want ErrStateTooLarge", err)
	}
}

func TestLocalStateSourceEnforcesSizeCeilingWhileReading(t *testing.T) {
	t.Parallel()

	path := writeStateFile(t, strings.Repeat("x", 4))
	source, err := terraformstate.NewLocalStateSource(terraformstate.LocalSourceConfig{
		Path:     path,
		MaxBytes: 4,
	})
	if err != nil {
		t.Fatalf("NewLocalStateSource() error = %v, want nil", err)
	}
	if err := os.WriteFile(path, []byte(strings.Repeat("x", 8)), 0o600); err != nil {
		t.Fatalf("WriteFile(grow) error = %v", err)
	}

	reader, _, err := source.Open(context.Background())
	if err == nil {
		defer reader.Close()
		_, err = io.ReadAll(reader)
	}
	if !errors.Is(err, terraformstate.ErrStateTooLarge) {
		t.Fatalf("read error = %v, want ErrStateTooLarge", err)
	}
}

func writeStateFile(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "terraform.tfstate")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}
