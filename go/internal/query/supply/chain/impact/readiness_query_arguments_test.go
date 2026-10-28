// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// readinessHighestPlaceholder returns the highest $n in ListReadinessQuery.
func readinessHighestPlaceholder(t *testing.T) int {
	t.Helper()
	highest := 0
	for _, match := range regexp.MustCompile(`\$([0-9]+)`).FindAllStringSubmatch(ListReadinessQuery, -1) {
		value, err := strconv.Atoi(match[1])
		if err != nil {
			t.Fatalf("parse placeholder %q: %v", match[0], err)
		}
		highest = max(highest, value)
	}
	return highest
}

// TestReadinessProofArgumentsMirrorProduction fails when the live proofs'
// shared argument helper, its PREPARE type list, or its EXECUTE renderer
// drifts from the production argument builder (#9435: six live proofs kept
// a 20-argument copy after the statement grew to 25).
func TestReadinessProofArgumentsMirrorProduction(t *testing.T) {
	t.Parallel()
	highest := readinessHighestPlaceholder(t)
	for _, query := range []ReadinessQuery{
		{CVEID: "CVE-2026-9227"},
		{PackageID: "npm://registry.npmjs.org/lodash"},
		{RepositoryID: "repository:r_proof"},
		{SubjectDigest: "sha256:proof"},
		{ImageRef: "registry.example.test/proof:latest"},
	} {
		proof := readinessArgsForQuery(query)
		production := readinessQueryArguments(query, readinessTarget{}, time.Now())
		if len(proof) != highest || len(production) != highest {
			t.Fatalf("%+v: proof args = %d, production args = %d, placeholders = %d", query, len(proof), len(production), highest)
		}
		for index := range production {
			if reflect.TypeOf(proof[index]) != reflect.TypeOf(production[index]) {
				t.Errorf("%+v: $%d proof type %T, production type %T", query, index+1, proof[index], production[index])
			}
		}
		prepare := readinessPrepareStatement(t, "proof", "SELECT 1", proof)
		types := strings.TrimSuffix(strings.TrimPrefix(prepare, "PREPARE proof("), ") AS SELECT 1")
		if got := len(strings.Split(types, ", ")); got != highest {
			t.Errorf("%+v: PREPARE declares %d types, want %d", query, got, highest)
		}
		execute := repoArmExecuteStatement(t, "proof", proof)
		if !strings.HasPrefix(execute, "EXECUTE proof(") {
			t.Errorf("%+v: EXECUTE rendering = %.60q", query, execute)
		}
	}
}

// TestReadinessQueryArgumentsMatchPlaceholders keeps the argument builder and
// the assembled SQL in lockstep: the highest placeholder must equal the
// argument count, so a new CTE parameter cannot ship unbound.
func TestReadinessQueryArgumentsMatchPlaceholders(t *testing.T) {
	t.Parallel()
	highest := readinessHighestPlaceholder(t)
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
