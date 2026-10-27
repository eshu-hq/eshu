// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"regexp"
	"strconv"
	"testing"
	"time"
)

// TestReadinessQueryArgumentsMatchPlaceholders keeps the argument builder and
// the assembled SQL in lockstep: the highest placeholder must equal the
// argument count, so a new CTE parameter cannot ship unbound.
func TestReadinessQueryArgumentsMatchPlaceholders(t *testing.T) {
	t.Parallel()
	highest := 0
	for _, match := range regexp.MustCompile(`\$([0-9]+)`).FindAllStringSubmatch(ListReadinessQuery, -1) {
		value, err := strconv.Atoi(match[1])
		if err != nil {
			t.Fatalf("parse placeholder %q: %v", match[0], err)
		}
		highest = max(highest, value)
	}
	args := readinessQueryArguments(ReadinessQuery{PackageID: "npm://registry.npmjs.org/lodash"}, readinessTarget{}, time.Now())
	if highest != len(args) {
		t.Fatalf("ListReadinessQuery highest placeholder = $%d, arguments = %d", highest, len(args))
	}
}

// TestReadinessEcosystemAliasesUseCanonicalNames pins the derivation spelling
// table: the collector's "go" maps to packageidentity's "gomod", and a
// canonical name the collector rejects in derivation config is not marked as
// a derivation spelling.
func TestReadinessEcosystemAliasesUseCanonicalNames(t *testing.T) {
	t.Parallel()
	spellings, canonicals, derivation := readinessEcosystemAliases()
	got := map[string][2]string{}
	for index, spelling := range spellings {
		got[spelling] = [2]string{canonicals[index], derivation[index]}
	}
	for spelling, want := range map[string][2]string{
		"go":    {"gomod", "true"},
		"gomod": {"gomod", "false"},
		"npm":   {"npm", "true"},
		"pypi":  {"pypi", "true"},
		"maven": {"maven", "true"},
	} {
		if got[spelling] != want {
			t.Fatalf("alias %q = %v, want %v (all = %v)", spelling, got[spelling], want, got)
		}
	}
	if _, ok := got["alpine"]; ok {
		t.Fatal("OS distributions must stay out of the derived readiness alias table until proven")
	}
}
