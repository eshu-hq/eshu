// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cpubudget

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"testing"
)

func TestUsableCPUsMatchesGOMAXPROCS(t *testing.T) {
	got := UsableCPUs()
	want := runtime.GOMAXPROCS(0)
	if want < 1 {
		want = 1
	}
	if got != want {
		t.Errorf("UsableCPUs() = %d, want %d (runtime.GOMAXPROCS(0) floored at 1)", got, want)
	}
}

// TestGoDirectiveSupportsAutomaticGOMAXPROCS guards the assumption UsableCPUs
// relies on: that the Go toolchain itself makes GOMAXPROCS cgroup-aware. Go
// made automatic container-aware GOMAXPROCS (the containermaxprocs GODEBUG)
// default-on starting with Go 1.25. UsableCPUs is intentionally a thin
// wrapper over runtime.GOMAXPROCS(0) rather than a handwritten cgroup
// reader — that backstop only holds if this module's pinned `go` directive
// in go.mod is at least 1.25. If this test ever fails after a downgrade, the
// automatic-GOMAXPROCS assumption no longer holds and UsableCPUs needs its
// own cgroup-quota reader again.
func TestGoDirectiveSupportsAutomaticGOMAXPROCS(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// This test file lives at go/internal/cpubudget/cpubudget_test.go; two
	// levels up is the go/ module root containing go.mod.
	modPath := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", "go.mod"))
	data, err := os.ReadFile(modPath)
	if err != nil {
		t.Fatalf("read %s: %v", modPath, err)
	}

	major, minor, ok := parseGoDirective(string(data))
	if !ok {
		t.Fatalf("no `go` directive found in %s", modPath)
	}

	if major < 1 || (major == 1 && minor < 25) {
		t.Fatalf(
			"go.mod `go` directive is %d.%d, want >= 1.25: "+
				"UsableCPUs relies on the Go runtime's automatic cgroup-aware "+
				"GOMAXPROCS (default-on since Go 1.25); below that version "+
				"UsableCPUs needs its own cgroup-quota reader",
			major, minor,
		)
	}
}

var goDirectiveRe = regexp.MustCompile(`(?m)^go\s+(\d+)\.(\d+)(?:\.\d+)?\s*$`)

// parseGoDirective extracts the major.minor version from a go.mod file's
// `go` directive line (e.g. "go 1.26.4" -> 1, 26). Returns ok=false if no
// directive line is found.
func parseGoDirective(modContent string) (major, minor int, ok bool) {
	match := goDirectiveRe.FindStringSubmatch(modContent)
	if match == nil {
		return 0, 0, false
	}
	major, errMajor := strconv.Atoi(match[1])
	minor, errMinor := strconv.Atoi(match[2])
	if errMajor != nil || errMinor != nil {
		return 0, 0, false
	}
	return major, minor, true
}

func TestParseGoDirective(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		wantMajor int
		wantMinor int
		wantOK    bool
	}{
		{
			name:      "patch version",
			content:   "module example.com/x\n\ngo 1.26.4\n",
			wantMajor: 1,
			wantMinor: 26,
			wantOK:    true,
		},
		{
			name:      "no patch version",
			content:   "module example.com/x\n\ngo 1.25\n",
			wantMajor: 1,
			wantMinor: 25,
			wantOK:    true,
		},
		{
			name:    "missing directive",
			content: "module example.com/x\n",
			wantOK:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			major, minor, ok := parseGoDirective(tt.content)
			if ok != tt.wantOK {
				t.Fatalf("parseGoDirective(%q) ok = %v, want %v", tt.content, ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if major != tt.wantMajor || minor != tt.wantMinor {
				t.Errorf(
					"parseGoDirective(%q) = %d.%d, want %d.%d",
					tt.content, major, minor, tt.wantMajor, tt.wantMinor,
				)
			}
		})
	}
}

// TestParseGoDirectiveTrimsTrailingWhitespace guards against a regexp that
// silently stops matching if go.mod ever gains trailing whitespace on the go
// directive line.
func TestParseGoDirectiveTrimsTrailingWhitespace(t *testing.T) {
	content := "module example.com/x\n\ngo 1.26.4   \n"
	major, minor, ok := parseGoDirective(content)
	if !ok || major != 1 || minor != 26 {
		t.Fatalf("parseGoDirective with trailing space = %d.%d ok=%v, want 1.26 ok=true", major, minor, ok)
	}
}
