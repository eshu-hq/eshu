package parser

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDetectSCIPProjectLanguagePrefersDominantAllowedLanguage(t *testing.T) {
	t.Parallel()

	got := DetectSCIPProjectLanguage(
		[]string{
			"/tmp/repo/a.py",
			"/tmp/repo/b.py",
			"/tmp/repo/c.go",
		},
		[]string{"go", "python"},
	)
	if got != "python" {
		t.Fatalf("DetectSCIPProjectLanguage() = %q, want %q", got, "python")
	}
}

func TestDetectSCIPProjectLanguageBreaksTiesLikePythonContract(t *testing.T) {
	t.Parallel()

	got := DetectSCIPProjectLanguage(
		[]string{
			"/tmp/repo/a.py",
			"/tmp/repo/b.ts",
		},
		[]string{"typescript", "python"},
	)
	if got != "python" {
		t.Fatalf("DetectSCIPProjectLanguage() = %q, want %q", got, "python")
	}
}

func TestBuildSCIPCommandMatchesRuntimeContract(t *testing.T) {
	t.Parallel()

	got, err := buildSCIPCommand("typescript", "/usr/local/bin/scip-typescript", "/tmp/index.scip")
	if err != nil {
		t.Fatalf("buildSCIPCommand() error = %v, want nil", err)
	}

	want := []string{"/usr/local/bin/scip-typescript", "index", "--output", "/tmp/index.scip"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildSCIPCommand() = %#v, want %#v", got, want)
	}
}

func TestSCIPIndexerRunWritesIndexToOutputPath(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	indexer := SCIPIndexer{
		LookPath: func(binary string) (string, error) {
			if binary != "scip-python" {
				t.Fatalf("LookPath() binary = %q, want %q", binary, "scip-python")
			}
			return "/usr/local/bin/scip-python", nil
		},
		RunCommand: func(_ context.Context, command []string, _ string) error {
			outputPath := command[len(command)-1]
			return os.WriteFile(outputPath, []byte("scip"), 0o644)
		},
	}

	got, err := indexer.Run(context.Background(), "/tmp/repo", "python", outputDir)
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	want := filepath.Join(outputDir, "index.scip")
	if got != want {
		t.Fatalf("Run() output path = %q, want %q", got, want)
	}
}
