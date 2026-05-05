package main

import (
	"bytes"
	"testing"
)

func TestRootVersionFlagPrintsBuildVersion(t *testing.T) {
	got := executeRootVersionForTest(t, "--version")
	if want := "Eshu dev\n"; got != want {
		t.Fatalf("eshu --version output = %q, want %q", got, want)
	}
}

func TestRootVersionShorthandPrintsBuildVersion(t *testing.T) {
	got := executeRootVersionForTest(t, "-v")
	if want := "Eshu dev\n"; got != want {
		t.Fatalf("eshu -v output = %q, want %q", got, want)
	}
}

func executeRootVersionForTest(t *testing.T, arg string) string {
	t.Helper()

	var output bytes.Buffer
	rootCmd.SetArgs([]string{arg})
	rootCmd.SetOut(&output)
	rootCmd.SetErr(&output)
	t.Cleanup(func() {
		rootCmd.SetArgs(nil)
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
	})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("rootCmd.Execute(%q) error = %v; output: %s", arg, err, output.String())
	}
	return output.String()
}
