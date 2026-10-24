// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rust

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/parser/shared/toml/nesting"
)

// cargoNestingCaseEnv names the hostile input a re-executed child parses.
const cargoNestingCaseEnv = "ESHU_CARGO_NESTING_CASE"

// cargoNestingResultPrefix marks the one line the child prints for the parent.
const cargoNestingResultPrefix = "CARGO_NESTING_RESULT "

// hostileDepth is the nesting the regressions use. go-toml v2.3.1 overflows the
// goroutine stack at about 885,000 '[' or 483,000 inline tables, so one
// million crashes the process on an unguarded parse.
const hostileDepth = 1_000_000

// cargoNestingResult is what the child reports about one parse.
type cargoNestingResult struct {
	Err        string `json:"err"`
	TooDeep    bool   `json:"too_deep"`
	AllocBytes uint64 `json:"alloc_bytes"`
	// FileBytes is the total size of the files the case writes.
	FileBytes int      `json:"file_bytes"`
	RowNames  []string `json:"row_names"`
}

// cargoNestingCase is one hostile layout: the files to write under the repo
// root and the file to parse.
type cargoNestingCase struct {
	files map[string]string
	parse string
	// wantTooDeep is true when the parse must fail with nesting.ErrTooDeep;
	// false means the parse must succeed (the ancestor manifest is skipped).
	wantTooDeep bool
	wantRows    []string
}

func cargoNestingCases() map[string]cargoNestingCase {
	open := strings.Repeat("[", hostileDepth)
	closeAll := strings.Repeat("]", hostileDepth)
	inline := strings.Repeat("{a=", hostileDepth)
	dotted := strings.Repeat("a.", hostileDepth) + "a"
	manifestHead := "[package]\nname = \"x\"\nversion = \"0.1.0\"\n\n[dependencies]\n"
	lockHead := "version = 3\n\n[[package]]\nname = \"a\"\nversion = \"1.0.0\"\n"
	return map[string]cargoNestingCase{
		"toml_array_unterminated": {
			files: map[string]string{"Cargo.toml": manifestHead + "hostile = " + open},
			parse: "Cargo.toml", wantTooDeep: true,
		},
		"toml_array_closed": {
			files: map[string]string{"Cargo.toml": manifestHead + "hostile = " + open + closeAll + "\n"},
			parse: "Cargo.toml", wantTooDeep: true,
		},
		"toml_inline_table": {
			files: map[string]string{"Cargo.toml": manifestHead + "hostile = " + inline},
			parse: "Cargo.toml", wantTooDeep: true,
		},
		"toml_dotted_key": {
			files: map[string]string{"Cargo.toml": manifestHead + dotted + " = 1\n"},
			parse: "Cargo.toml", wantTooDeep: true,
		},
		"toml_dotted_header": {
			files: map[string]string{"Cargo.toml": "[" + dotted + "]\n"},
			parse: "Cargo.toml", wantTooDeep: true,
		},
		"lock_array_unterminated": {
			files: map[string]string{"Cargo.lock": lockHead + "dependencies = " + open},
			parse: "Cargo.lock", wantTooDeep: true,
		},
		"lock_inline_table": {
			files: map[string]string{"Cargo.lock": lockHead + "hostile = " + inline},
			parse: "Cargo.lock", wantTooDeep: true,
		},
		"lock_dotted_header": {
			files: map[string]string{"Cargo.lock": lockHead + "\n[[" + dotted + "]]\n"},
			parse: "Cargo.lock", wantTooDeep: true,
		},
		"ancestor_manifest": {
			files: map[string]string{
				"Cargo.toml":            "[workspace.dependencies]\nhostile = " + open,
				"crates/api/Cargo.toml": manifestHead + "tokio = \"1.37\"\n",
			},
			parse: "crates/api/Cargo.toml", wantRows: []string{"tokio"},
		},
		"ancestor_dotted_key": {
			files: map[string]string{
				"Cargo.toml":            dotted + " = 1\n",
				"crates/api/Cargo.toml": manifestHead + "tokio = \"1.37\"\n",
			},
			parse: "crates/api/Cargo.toml", wantRows: []string{"tokio"},
		},
	}
}

// TestCargoHostileNestingIsRejectedWithoutCrash parses each hostile file in a
// re-executed child process. A stack overflow is a fatal runtime error that
// recover cannot catch, so an in-process test would kill the whole test
// binary; the child turns that crash into one failed subtest.
func TestCargoHostileNestingIsRejectedWithoutCrash(t *testing.T) {
	if os.Getenv(cargoNestingCaseEnv) != "" {
		t.Skip("running as the helper child")
	}
	for name, tc := range cargoNestingCases() {
		// Sequential on purpose: an unguarded child can reach about 1 GB
		// resident before it dies, and parallel children would multiply that.
		t.Run(name, func(t *testing.T) {
			result := runCargoNestingChild(t, name)
			if tc.wantTooDeep {
				if !result.TooDeep {
					t.Fatalf("parse error = %q, want nesting.ErrTooDeep", result.Err)
				}
			} else if result.Err != "" {
				t.Fatalf("parse error = %q, want nil (the hostile ancestor must be skipped)", result.Err)
			}
			if strings.Join(result.RowNames, ",") != strings.Join(tc.wantRows, ",") {
				t.Fatalf("rows = %v, want %v", result.RowNames, tc.wantRows)
			}
			// Reading the file costs its size once; the guard itself is
			// allocation-free. 2x plus 64 KiB leaves room for path and
			// payload bookkeeping, far below the hundreds of MB an unguarded
			// parse of this input allocates.
			ceiling := uint64(2*result.FileBytes) + 64<<10
			if result.AllocBytes > ceiling {
				t.Fatalf("parse allocated %d bytes, want <= %d (files %d bytes)",
					result.AllocBytes, ceiling, result.FileBytes)
			}
		})
	}
}

func runCargoNestingChild(t *testing.T, name string) cargoNestingResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCargoNestingHelperProcess$", "-test.v")
	cmd.Env = append(os.Environ(), cargoNestingCaseEnv+"="+name)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helper child for %s failed: %v: %s", name, err, fatalLine(output))
	}
	for _, line := range strings.Split(string(output), "\n") {
		if payload, ok := strings.CutPrefix(line, cargoNestingResultPrefix); ok {
			var result cargoNestingResult
			if err := json.Unmarshal([]byte(payload), &result); err != nil {
				t.Fatalf("decode child result: %v", err)
			}
			return result
		}
	}
	t.Fatalf("helper child for %s printed no result:\n%s", name, tailBytes(output, 2048))
	return cargoNestingResult{}
}

// TestCargoNestingHelperProcess is the child half of
// TestCargoHostileNestingIsRejectedWithoutCrash. It does nothing unless the
// parent set cargoNestingCaseEnv.
func TestCargoNestingHelperProcess(t *testing.T) {
	name := os.Getenv(cargoNestingCaseEnv)
	if name == "" {
		t.Skip("only runs as a child of TestCargoHostileNestingIsRejectedWithoutCrash")
	}
	tc, ok := cargoNestingCases()[name]
	if !ok {
		t.Fatalf("unknown case %q", name)
	}
	repoRoot := t.TempDir()
	for rel, body := range tc.files {
		path := filepath.Join(repoRoot, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	target := filepath.Join(repoRoot, filepath.FromSlash(tc.parse))
	// Every file in the case is read once (the ancestor walk reads the
	// root manifest too), so the ceiling scales with their total size.
	result := cargoNestingResult{}
	for _, body := range tc.files {
		result.FileBytes += len(body)
	}

	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	payload, err := ParseCargoDependencyFile(repoRoot, target, false)
	runtime.ReadMemStats(&after)
	result.AllocBytes = after.TotalAlloc - before.TotalAlloc

	if err != nil {
		result.Err = err.Error()
		result.TooDeep = errors.Is(err, nesting.ErrTooDeep)
	}
	if payload != nil {
		rows, _ := payload["variables"].([]map[string]any)
		for _, row := range rows {
			name, _ := row["name"].(string)
			result.RowNames = append(result.RowNames, name)
		}
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	// Print with a leading newline so test framework output cannot share
	// the line.
	os.Stdout.WriteString("\n" + cargoNestingResultPrefix + string(encoded) + "\n")
}

// fatalLine returns the runtime's "fatal error:" line from a crashed child,
// or the tail of its output when there is none.
func fatalLine(output []byte) string {
	for _, line := range strings.Split(string(output), "\n") {
		if strings.HasPrefix(line, "fatal error:") || strings.HasPrefix(line, "panic:") {
			return line
		}
	}
	return string(tailBytes(output, 2048))
}

func tailBytes(b []byte, n int) []byte {
	if len(b) <= n {
		return b
	}
	return b[len(b)-n:]
}
