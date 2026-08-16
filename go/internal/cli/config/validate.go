// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package config

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/envregistry"
)

// EnvironMap parses os.Environ()-style "KEY=VALUE" pairs into a map. Pairs
// without an "=" are dropped. Callers supply the slice; this package never
// calls os.Environ itself, so a test can validate an explicit environment
// snapshot instead of the process's own.
func EnvironMap(pairs []string) map[string]string {
	env := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		if key, value, ok := strings.Cut(pair, "="); ok {
			env[key] = value
		}
	}
	return env
}

// ValidateEnv checks an environment snapshot against the ESHU_* registry and
// writes the findings to out. It returns a non-nil error when any error-level
// finding is present, which is what makes `eshu config validate` exit non-zero.
//
// strict turns every unrecognized ESHU_* variable into a finding; without it
// only unknown names that closely resemble a registered one (likely typos) are
// reported, so collector-owned and site-local variables do not generate noise.
func ValidateEnv(out io.Writer, registry *envregistry.Registry, env map[string]string, strict bool) error {
	return reportFindings(out, registry.Validate(env, strict))
}

// reportFindings prints findings grouped into errors and warnings and returns a
// non-nil error when any error-level finding is present, so the command exits
// non-zero. Errors sort ahead of warnings and each group sorts by variable
// name, so the same environment always produces the same report.
func reportFindings(out io.Writer, findings []envregistry.Finding) error {
	if len(findings) == 0 {
		_, _ = fmt.Fprintln(out, "config validate: OK — no ESHU_* configuration problems found")
		return nil
	}

	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Error != findings[j].Error {
			return findings[i].Error // errors first
		}
		return findings[i].Name < findings[j].Name
	})

	var errorCount, warnCount int
	for _, f := range findings {
		label := "WARN "
		if f.Error {
			label = "ERROR"
			errorCount++
		} else {
			warnCount++
		}
		_, _ = fmt.Fprintf(out, "%s %s\n", label, f.Message)
	}
	_, _ = fmt.Fprintf(out, "\nconfig validate: %d error(s), %d warning(s)\n", errorCount, warnCount)

	if errorCount > 0 {
		return fmt.Errorf("configuration invalid: %d error(s)", errorCount)
	}
	return nil
}
