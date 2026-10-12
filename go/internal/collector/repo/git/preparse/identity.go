// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package preparse

import (
	"crypto/sha256"
	"fmt"
	"hash"
	"sort"
	"strings"
	"time"
)

// IdentityPrefix starts every pre-parse identity. The version suffix changes
// when the identity's inputs or encoding change, so an old stored identity can
// never equal a new one.
const IdentityPrefix = "preparse:v1:"

// BinaryStat identifies one SCIP indexer binary the snapshot would run. SCIP
// output depends on the binary, so its resolved path, size, and modification
// time are folded into the identity; a missing binary is folded too, because
// SCIP then falls back to the native parser and the output differs.
type BinaryStat struct {
	// Language is the SCIP language the binary indexes.
	Language string
	// Path is the resolved executable path, or empty when Missing.
	Path string
	// Size is the file size in bytes.
	Size int64
	// ModTime is the file modification time.
	ModTime time.Time
	// Missing reports that no binary was found on PATH.
	Missing bool
}

// Config is every input outside the commit that changes what a git snapshot
// emits. Build it once from the same values the snapshotter uses (the cmd
// wiring passes one Config to the snapshotter and the selectors), so the
// identity the selector compares and the identity a parse stamps cannot drift.
type Config struct {
	// IgnoredPathGlobs are the discovery ignore rules as "pattern=reason".
	IgnoredPathGlobs []string
	// PreservedPathGlobs are the discovery preserve rules.
	PreservedPathGlobs []string
	// EmitDataflow is the ESHU_EMIT_DATAFLOW gate (taint, interproc, function
	// summaries, dataflow catalogs).
	EmitDataflow bool
	// SCIPEnabled is the SCIP_INDEXER gate.
	SCIPEnabled bool
	// SCIPLanguages are the SCIP_LANGUAGES the snapshot indexes.
	SCIPLanguages []string
	// SCIPBinaries are the indexer binaries for SCIPLanguages; empty when SCIP
	// is off.
	SCIPBinaries []BinaryStat
	// DependencyMode is ESHU_BOOTSTRAP_IS_DEPENDENCY.
	DependencyMode bool
	// DependencyName is ESHU_BOOTSTRAP_PACKAGE_NAME.
	DependencyName string
	// DependencyLanguage is ESHU_BOOTSTRAP_PACKAGE_LANGUAGE.
	DependencyLanguage string
}

// ConfigDigest returns a stable digest of the configuration alone, so a log
// can say whether the configuration or the commit moved.
func ConfigDigest(c Config) string {
	h := sha256.New()
	writeConfig(h, c)
	return fmt.Sprintf("%x", h.Sum(nil))[:16]
}

// Identity returns the pre-parse identity of a snapshot of commit on
// defaultBranch, taken by a collector whose output contract is contentVersion
// and whose configuration is c. Two snapshots with the same identity emit the
// same facts, so a reconcile whose identity equals the active generation's can
// skip the parse (#7978, ruling a9-143). It returns an empty string when the
// content version or the commit is blank: no identity, no skip.
func Identity(contentVersion, commit, defaultBranch string, c Config) string {
	contentVersion = strings.TrimSpace(contentVersion)
	commit = strings.TrimSpace(commit)
	if contentVersion == "" || commit == "" {
		return ""
	}
	h := sha256.New()
	write(h, "content_version", contentVersion)
	write(h, "commit", commit)
	write(h, "default_branch", strings.TrimSpace(defaultBranch))
	writeConfig(h, c)
	return fmt.Sprintf("%s%x", IdentityPrefix, h.Sum(nil))
}

func writeConfig(h hash.Hash, c Config) {
	for _, glob := range sortedTrimmed(c.IgnoredPathGlobs) {
		write(h, "ignored_glob", glob)
	}
	for _, glob := range sortedTrimmed(c.PreservedPathGlobs) {
		write(h, "preserved_glob", glob)
	}
	write(h, "emit_dataflow", fmt.Sprint(c.EmitDataflow))
	write(h, "scip_enabled", fmt.Sprint(c.SCIPEnabled))
	for _, language := range sortedTrimmed(c.SCIPLanguages) {
		write(h, "scip_language", language)
	}
	binaries := append([]BinaryStat(nil), c.SCIPBinaries...)
	sort.Slice(binaries, func(i, j int) bool { return binaries[i].Language < binaries[j].Language })
	for _, binary := range binaries {
		write(h, "scip_binary", fmt.Sprintf("%s|%s|%d|%d|%t",
			binary.Language, binary.Path, binary.Size, binary.ModTime.UnixNano(), binary.Missing))
	}
	write(h, "dependency_mode", fmt.Sprint(c.DependencyMode))
	write(h, "dependency_name", strings.TrimSpace(c.DependencyName))
	write(h, "dependency_language", strings.TrimSpace(c.DependencyLanguage))
}

func write(h hash.Hash, key, value string) {
	_, _ = fmt.Fprintf(h, "%s=%q\n", key, value)
}

func sortedTrimmed(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}
