package runtime

import (
	"fmt"
	"log/slog"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
)

const (
	// DefaultMemLimitRatio is the fraction of container memory to use as
	// GOMEMLIMIT. 70% leaves headroom for non-heap allocations (goroutine
	// stacks, mmap'd files, cgo, kernel page cache).
	DefaultMemLimitRatio = 0.70

	// MinMemLimit is the floor — never set GOMEMLIMIT below this.
	MinMemLimit = 512 << 20 // 512 MiB
)

// ConfigureMemoryLimit sets GOMEMLIMIT based on:
//  1. GOMEMLIMIT env var (explicit override via Go runtime, highest priority)
//  2. Container cgroup memory limit × DefaultMemLimitRatio
//  3. No-op if neither is available (let Go defaults apply)
//
// It also unconditionally sets GODEBUG=madvdontneed=1 which forces the Go
// runtime to release RSS pages to the OS immediately on GC. This prevents
// the kernel OOM killer from targeting the container based on inflated RSS.
//
// Returns the applied limit in bytes, or 0 if no limit was set.
func ConfigureMemoryLimit(logger *slog.Logger) int64 {
	// Always set madvdontneed=1 for containerized deployments.
	// This is safe and correct everywhere — it only affects how freed pages
	// are returned to the OS (immediately vs lazily).
	configureMADVDONTNEED(logger)

	// Priority 1: GOMEMLIMIT env var (Go runtime reads this natively).
	// If set, the runtime already applied it — just log and return.
	if envLimit := os.Getenv("GOMEMLIMIT"); envLimit != "" {
		if logger != nil {
			logger.Info("memory limit from GOMEMLIMIT env var",
				slog.String("value", envLimit),
				slog.String("source", "env"),
			)
		}
		return 0
	}

	// Priority 2: read container cgroup memory limit
	containerLimit := readCgroupMemoryLimit()
	if containerLimit <= 0 {
		if logger != nil {
			logger.Info("no container memory limit detected, GOMEMLIMIT unchanged",
				slog.String("source", "default"),
			)
		}
		return 0
	}

	limit := int64(float64(containerLimit) * DefaultMemLimitRatio)
	if limit < MinMemLimit {
		limit = MinMemLimit
	}

	previous := debug.SetMemoryLimit(limit)
	if logger != nil {
		logger.Info("GOMEMLIMIT configured from container memory",
			slog.Int64("container_memory_bytes", containerLimit),
			slog.String("container_memory_human", formatBytes(containerLimit)),
			slog.Float64("ratio", DefaultMemLimitRatio),
			slog.Int64("gomemlimit_bytes", limit),
			slog.String("gomemlimit_human", formatBytes(limit)),
			slog.Int64("previous_limit", previous),
			slog.String("source", "cgroup"),
		)
	}

	return limit
}

// configureMADVDONTNEED sets GODEBUG=madvdontneed=1 if not already set.
// Go's default MADV_FREE marks freed pages as reusable but doesn't release
// them to the OS. Docker stats reports RSS including these pages, making it
// appear memory never drops. MADV_DONTNEED forces immediate release.
func configureMADVDONTNEED(logger *slog.Logger) {
	godebug := os.Getenv("GODEBUG")
	if strings.Contains(godebug, "madvdontneed=") {
		return // already configured
	}
	if godebug == "" {
		godebug = "madvdontneed=1"
	} else {
		godebug = godebug + ",madvdontneed=1"
	}
	if err := os.Setenv("GODEBUG", godebug); err != nil {
		if logger != nil {
			logger.Error("failed to configure GODEBUG for immediate RSS release",
				slog.Any("error", err),
			)
		}
		return
	}
	if logger != nil {
		logger.Info("GODEBUG configured for immediate RSS release",
			slog.String("godebug", godebug),
		)
	}
}

// readCgroupMemoryLimit reads the container memory limit from cgroups.
// Tries cgroups v2 first, then v1.
func readCgroupMemoryLimit() int64 {
	// cgroups v2: /sys/fs/cgroup/memory.max
	if data, err := os.ReadFile("/sys/fs/cgroup/memory.max"); err == nil {
		s := strings.TrimSpace(string(data))
		if s != "max" {
			if v, err := strconv.ParseInt(s, 10, 64); err == nil {
				return v
			}
		}
	}

	// cgroups v1: /sys/fs/cgroup/memory/memory.limit_in_bytes
	if data, err := os.ReadFile("/sys/fs/cgroup/memory/memory.limit_in_bytes"); err == nil {
		s := strings.TrimSpace(string(data))
		if v, err := strconv.ParseInt(s, 10, 64); err == nil {
			// cgroups v1 returns a huge number when unlimited
			if v < 1<<62 {
				return v
			}
		}
	}

	return 0
}

func formatBytes(b int64) string {
	const gib = 1 << 30
	if b >= gib {
		return fmt.Sprintf("%.1fGiB", float64(b)/float64(gib))
	}
	return fmt.Sprintf("%.0fMiB", float64(b)/float64(1<<20))
}
