// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/migrations"
)

// deferredDefinitionShippedChecksums pins the sha256 of every definition SQL
// payload that BootstrapDefinitionsWithoutContentSearchIndexes rewrites.
// #9279: #8028 edited the deferred 004 constant in place and the per-file
// manifest (which pins only shipped .sql bytes) stayed green, so no CI gate
// caught the drift that broke pre-#8028 deferred installs on upgrade. This
// pin fails on any in-place edit to a rewritten definition's SQL.
//
// Adding a rewritten definition appends its line here (run
// TestDeferredDefinitionsMatchShippedChecksums -v and copy the reported
// checksum). Never edit an existing line: widen deferred behavior through a
// new migration instead. #7002 states the same rule for shipped files.
var deferredDefinitionShippedChecksums = map[string]string{
	"content_store":                          "3d8ef124d1e6b5641f5d84fe581ede18046fcde70f6aad5adb3934611562f5f3",
	"content_substring_index_state":          "f0d10012b0bd3f727fe3a3af20cc58866dac6b22337ca0f43d85f6e2bd25d642",
	"content_entity_name_trgm_index":         "f5e959e09bb63fdf650a5bfc7c4749d4d2345ea5f41d4a9db750a0d9c6b021f7",
	"content_files_relative_path_trgm_index": "17db4fd369edb9244b9f91d9aeed145c3d04ad8ba6e95d06247f07a63527d11a",
}

// validateDeferredDefinitionChecksums fails when the rewritten set or any
// rewritten payload drifts from the pin above.
func validateDeferredDefinitionChecksums(defs []Definition) error {
	seen := map[string]bool{}
	for _, def := range defs {
		if _, ok := deferredDefinitionShippedChecksums[def.Name]; !ok {
			continue
		}
		seen[def.Name] = true
		if def.Variant != "deferred" {
			return fmt.Errorf("definition %q variant = %q, want %q", def.Name, def.Variant, "deferred")
		}
		if got := migrations.Checksum(def.SQL); got != deferredDefinitionShippedChecksums[def.Name] {
			return fmt.Errorf("definition %q checksum changed: manifest %s, current %s -- a rewritten definition must never be edited in place; widen it through a new guarded migration instead",
				def.Name, deferredDefinitionShippedChecksums[def.Name], got)
		}
	}
	for name := range deferredDefinitionShippedChecksums {
		if !seen[name] {
			return fmt.Errorf("deferred definition %q missing from BootstrapDefinitionsWithoutContentSearchIndexes()", name)
		}
	}
	return nil
}

func TestDeferredDefinitionsMatchShippedChecksums(t *testing.T) {
	t.Parallel()

	if err := validateDeferredDefinitionChecksums(BootstrapDefinitionsWithoutContentSearchIndexes()); err != nil {
		t.Fatal(err)
	}
}

// TestDeferredDefinitionsPinRejectsSeededViolation is the RED half of the
// #9279 pin: a one-byte edit to any rewritten definition must fail the pin.
func TestDeferredDefinitionsPinRejectsSeededViolation(t *testing.T) {
	t.Parallel()

	defs := BootstrapDefinitionsWithoutContentSearchIndexes()
	for i := range defs {
		if _, ok := deferredDefinitionShippedChecksums[defs[i].Name]; ok {
			defs[i].SQL += "-- seeded violation"
			break
		}
	}
	if err := validateDeferredDefinitionChecksums(defs); err == nil {
		t.Fatal("validateDeferredDefinitionChecksums(tampered) = nil, want an error naming the edited definition")
	} else if !strings.Contains(err.Error(), "checksum changed") {
		t.Fatalf("validateDeferredDefinitionChecksums(tampered) error %v does not point at the drift", err)
	}
}

// TestDeferredDefinitionsPinCoversEveryRewrite fails when
// BootstrapDefinitionsWithoutContentSearchIndexes rewrites a definition this
// pin does not know: Silent coverage loss is how #8028's drift escaped, so a
// new rewrite must update the pin above in the same change.
func TestDeferredDefinitionsPinCoversEveryRewrite(t *testing.T) {
	t.Parallel()

	for _, def := range BootstrapDefinitionsWithoutContentSearchIndexes() {
		if def.Variant != "deferred" {
			continue
		}
		if _, ok := deferredDefinitionShippedChecksums[def.Name]; !ok {
			t.Fatalf("definition %q is rewritten by BootstrapDefinitionsWithoutContentSearchIndexes but has no pinned checksum -- add it to deferredDefinitionShippedChecksums",
				def.Name)
		}
		if def.FullChecksum == "" {
			t.Fatalf("definition %q has an empty FullChecksum", def.Name)
		}
	}
}
