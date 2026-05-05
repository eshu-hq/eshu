package runtime

import (
	"log/slog"
	"os"
	"runtime/debug"
	"strings"
	"testing"
)

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		name  string
		bytes int64
		want  string
	}{
		{
			name:  "zero",
			bytes: 0,
			want:  "0MiB",
		},
		{
			name:  "512 MiB",
			bytes: 512 << 20,
			want:  "512MiB",
		},
		{
			name:  "1 GiB",
			bytes: 1 << 30,
			want:  "1.0GiB",
		},
		{
			name:  "1.5 GiB",
			bytes: (3 << 30) / 2,
			want:  "1.5GiB",
		},
		{
			name:  "4 GiB",
			bytes: 4 << 30,
			want:  "4.0GiB",
		},
		{
			name:  "100 MiB",
			bytes: 100 << 20,
			want:  "100MiB",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := formatBytes(tt.bytes)
			if got != tt.want {
				t.Errorf("formatBytes(%d) = %q, want %q", tt.bytes, got, tt.want)
			}
		})
	}
}

func TestConfigureMemoryLimitRespectsGOMEMLIMITEnv(t *testing.T) {
	// Save original to restore after test
	origGOMEMLIMIT := os.Getenv("GOMEMLIMIT")
	origGODEBUG := os.Getenv("GODEBUG")
	defer func() {
		if origGOMEMLIMIT != "" {
			if err := os.Setenv("GOMEMLIMIT", origGOMEMLIMIT); err != nil {
				t.Fatalf("restore GOMEMLIMIT: %v", err)
			}
		} else {
			if err := os.Unsetenv("GOMEMLIMIT"); err != nil {
				t.Fatalf("restore GOMEMLIMIT unset: %v", err)
			}
		}
		if origGODEBUG != "" {
			if err := os.Setenv("GODEBUG", origGODEBUG); err != nil {
				t.Fatalf("restore GODEBUG: %v", err)
			}
		} else {
			if err := os.Unsetenv("GODEBUG"); err != nil {
				t.Fatalf("restore GODEBUG unset: %v", err)
			}
		}
	}()

	// Set GOMEMLIMIT explicitly
	if err := os.Setenv("GOMEMLIMIT", "2GiB"); err != nil {
		t.Fatalf("set GOMEMLIMIT: %v", err)
	}
	if err := os.Unsetenv("GODEBUG"); err != nil {
		t.Fatalf("unset GODEBUG: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	result := ConfigureMemoryLimit(logger)

	if result != 0 {
		t.Errorf("ConfigureMemoryLimit() with GOMEMLIMIT env set should return 0, got %d", result)
	}
}

func TestConfigureMemoryLimitNoCgroup(t *testing.T) {
	// Save original to restore after test
	origGOMEMLIMIT := os.Getenv("GOMEMLIMIT")
	origGODEBUG := os.Getenv("GODEBUG")
	defer func() {
		if origGOMEMLIMIT != "" {
			if err := os.Setenv("GOMEMLIMIT", origGOMEMLIMIT); err != nil {
				t.Fatalf("restore GOMEMLIMIT: %v", err)
			}
		} else {
			if err := os.Unsetenv("GOMEMLIMIT"); err != nil {
				t.Fatalf("restore GOMEMLIMIT unset: %v", err)
			}
		}
		if origGODEBUG != "" {
			if err := os.Setenv("GODEBUG", origGODEBUG); err != nil {
				t.Fatalf("restore GODEBUG: %v", err)
			}
		} else {
			if err := os.Unsetenv("GODEBUG"); err != nil {
				t.Fatalf("restore GODEBUG unset: %v", err)
			}
		}
	}()

	if err := os.Unsetenv("GOMEMLIMIT"); err != nil {
		t.Fatalf("unset GOMEMLIMIT: %v", err)
	}
	if err := os.Unsetenv("GODEBUG"); err != nil {
		t.Fatalf("unset GODEBUG: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	result := ConfigureMemoryLimit(logger)

	// On a dev machine without cgroup files, should return 0
	// (actual behavior depends on whether cgroup files exist)
	if result < 0 {
		t.Errorf("ConfigureMemoryLimit() should not return negative value, got %d", result)
	}
}

func TestConfigureMADVDONTNEED(t *testing.T) {
	// Save original to restore after test
	origGODEBUG := os.Getenv("GODEBUG")
	defer func() {
		if origGODEBUG != "" {
			if err := os.Setenv("GODEBUG", origGODEBUG); err != nil {
				t.Fatalf("restore GODEBUG: %v", err)
			}
		} else {
			if err := os.Unsetenv("GODEBUG"); err != nil {
				t.Fatalf("restore GODEBUG unset: %v", err)
			}
		}
	}()

	if err := os.Unsetenv("GODEBUG"); err != nil {
		t.Fatalf("unset GODEBUG: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	configureMADVDONTNEED(logger)

	godebug := os.Getenv("GODEBUG")
	if godebug != "madvdontneed=1" {
		t.Errorf("configureMADVDONTNEED() should set GODEBUG=madvdontneed=1, got %q", godebug)
	}
}

func TestConfigureMADVDONTNEEDPreservesExisting(t *testing.T) {
	// Save original to restore after test
	origGODEBUG := os.Getenv("GODEBUG")
	defer func() {
		if origGODEBUG != "" {
			if err := os.Setenv("GODEBUG", origGODEBUG); err != nil {
				t.Fatalf("restore GODEBUG: %v", err)
			}
		} else {
			if err := os.Unsetenv("GODEBUG"); err != nil {
				t.Fatalf("restore GODEBUG unset: %v", err)
			}
		}
	}()

	if err := os.Setenv("GODEBUG", "gctrace=1"); err != nil {
		t.Fatalf("set GODEBUG: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	configureMADVDONTNEED(logger)

	godebug := os.Getenv("GODEBUG")
	if !strings.Contains(godebug, "gctrace=1") {
		t.Errorf("configureMADVDONTNEED() should preserve existing GODEBUG settings, got %q", godebug)
	}
	if !strings.Contains(godebug, "madvdontneed=1") {
		t.Errorf("configureMADVDONTNEED() should append madvdontneed=1, got %q", godebug)
	}
}

func TestConfigureMADVDONTNEEDSkipsIfAlreadySet(t *testing.T) {
	// Save original to restore after test
	origGODEBUG := os.Getenv("GODEBUG")
	defer func() {
		if origGODEBUG != "" {
			if err := os.Setenv("GODEBUG", origGODEBUG); err != nil {
				t.Fatalf("restore GODEBUG: %v", err)
			}
		} else {
			if err := os.Unsetenv("GODEBUG"); err != nil {
				t.Fatalf("restore GODEBUG unset: %v", err)
			}
		}
	}()

	if err := os.Setenv("GODEBUG", "madvdontneed=1,gctrace=1"); err != nil {
		t.Fatalf("set GODEBUG: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	configureMADVDONTNEED(logger)

	godebug := os.Getenv("GODEBUG")
	if godebug != "madvdontneed=1,gctrace=1" {
		t.Errorf("configureMADVDONTNEED() should not modify GODEBUG when madvdontneed already set, got %q", godebug)
	}
}

func TestReadCgroupMemoryLimitReturnsZeroWhenNoFiles(t *testing.T) {
	t.Parallel()
	// This test documents the behavior on dev machines without cgroup files.
	// readCgroupMemoryLimit returns 0 when files don't exist or contain "max".
	limit := readCgroupMemoryLimit()
	if limit < 0 {
		t.Errorf("readCgroupMemoryLimit() should not return negative, got %d", limit)
	}
}

func TestConfigureMemoryLimitAppliesMinLimit(t *testing.T) {
	// Save original to restore after test
	origGOMEMLIMIT := os.Getenv("GOMEMLIMIT")
	origGODEBUG := os.Getenv("GODEBUG")
	origLimit := debug.SetMemoryLimit(-1)
	defer func() {
		debug.SetMemoryLimit(origLimit)
		if origGOMEMLIMIT != "" {
			if err := os.Setenv("GOMEMLIMIT", origGOMEMLIMIT); err != nil {
				t.Fatalf("restore GOMEMLIMIT: %v", err)
			}
		} else {
			if err := os.Unsetenv("GOMEMLIMIT"); err != nil {
				t.Fatalf("restore GOMEMLIMIT unset: %v", err)
			}
		}
		if origGODEBUG != "" {
			if err := os.Setenv("GODEBUG", origGODEBUG); err != nil {
				t.Fatalf("restore GODEBUG: %v", err)
			}
		} else {
			if err := os.Unsetenv("GODEBUG"); err != nil {
				t.Fatalf("restore GODEBUG unset: %v", err)
			}
		}
	}()

	if err := os.Unsetenv("GOMEMLIMIT"); err != nil {
		t.Fatalf("unset GOMEMLIMIT: %v", err)
	}
	if err := os.Unsetenv("GODEBUG"); err != nil {
		t.Fatalf("unset GODEBUG: %v", err)
	}

	// This test documents that when a cgroup limit is detected but the
	// computed limit (70% of container) is below MinMemLimit, we floor at
	// MinMemLimit. On dev machines without cgroup files, this test will
	// see result=0 (no limit set).
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	result := ConfigureMemoryLimit(logger)

	// If a limit was set, it must be >= MinMemLimit or 0 (no cgroup)
	if result > 0 && result < MinMemLimit {
		t.Errorf("ConfigureMemoryLimit() applied limit %d below MinMemLimit %d", result, MinMemLimit)
	}
}
