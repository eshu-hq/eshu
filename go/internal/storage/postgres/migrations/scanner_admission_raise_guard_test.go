// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package migrations

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// scannerAdmissionAuthorityFunction names the admission proof function whose
// denial classes this guard pins (#9299 P2-3).
const scannerAdmissionAuthorityFunction = "eshu_security.validate_scanner_source_admission_v1"

// scannerAdmissionFatalRaises is the exact allowlist of RAISE messages the
// effective admission authority definition may contain. Every other denial
// inside that definition must be a no-row RETURN (a counted per-binding
// skip), never an exception: inside the single admission transaction an
// exception aborts the whole page including healthy siblings. A new RAISE
// fails this guard until it is classified here.
var scannerAdmissionFatalRaises = []string{
	"scanner canonical admission input invalid",
	"scanner local boundary invalid",
	"scanner hosted boundary invalid",
	"scanner and source instances cannot coincide",
}

// scannerAdmissionDefiningMigrations is the exact set of migration files
// allowed to (re)define the admission authority function. A future
// redefinition outside this set bypasses the raise allowlist below.
var scannerAdmissionDefiningMigrations = []string{
	"253_scanner_canonical_admission_authority.sql",
	"265_scanner_admission_authority_deny_no_row.sql",
}

var scannerAdmissionRaisePattern = regexp.MustCompile(`RAISE\s+EXCEPTION\s+'([^']+)'`)

// admissionAuthorityRaiseMessages extracts every RAISE EXCEPTION message
// from one function definition body, in source order.
func admissionAuthorityRaiseMessages(definitionSQL string) []string {
	var messages []string
	for _, match := range scannerAdmissionRaisePattern.FindAllStringSubmatch(definitionSQL, -1) {
		messages = append(messages, match[1])
	}
	return messages
}

// admissionAuthorityDefinitions returns the embedded definitions that
// (re)define the admission authority function, ordered by migration
// sequence with the effective (highest-numbered) definition last.
func admissionAuthorityDefinitions(defs []Definition) []Definition {
	var out []Definition
	for _, def := range defs {
		if strings.Contains(def.SQL, "FUNCTION "+scannerAdmissionAuthorityFunction+"(") {
			out = append(out, def)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return migrationSequenceNumber(out[i].Path) < migrationSequenceNumber(out[j].Path)
	})
	return out
}

func migrationSequenceNumber(defPath string) int {
	base := defPath
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	digits := base
	if i := strings.Index(digits, "_"); i >= 0 {
		digits = digits[:i]
	}
	sequence, err := strconv.Atoi(digits)
	if err != nil {
		return -1
	}
	return sequence
}

func baseNames(defs []Definition) []string {
	names := make([]string, 0, len(defs))
	for _, def := range defs {
		name := def.Path
		if i := strings.LastIndex(name, "/"); i >= 0 {
			name = name[i+1:]
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func equalStringSets(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	counts := make(map[string]int, len(a))
	for _, value := range a {
		counts[value]++
	}
	for _, value := range b {
		counts[value]--
		if counts[value] < 0 {
			return false
		}
	}
	return true
}

// TestScannerAdmissionAuthorityRaiseAllowlist pins the denial classes of the
// effective admission authority definition: exactly the fatal allowlist may
// raise, and exactly the known migrations may define the function.
func TestScannerAdmissionAuthorityRaiseAllowlist(t *testing.T) {
	defining := admissionAuthorityDefinitions(BootstrapDefinitions())
	if got, want := baseNames(defining), scannerAdmissionDefiningMigrations; !equalStringSets(got, want) {
		t.Fatalf("admission authority defining migrations = %v, want %v", got, want)
	}
	effective := defining[len(defining)-1]
	if got, want := admissionAuthorityRaiseMessages(effective.SQL), scannerAdmissionFatalRaises; !equalStringSets(got, want) {
		t.Fatalf("effective %s raises = %v, want exactly %v", effective.Path, got, want)
	}
}

// TestScannerAdmissionAuthorityRaiseAllowlistDetectsViolation is the
// seeded-violation RED half of the guard: a planted extra RAISE and a
// converted fatal must both break the allowlist comparison the real guard
// relies on.
func TestScannerAdmissionAuthorityRaiseAllowlistDetectsViolation(t *testing.T) {
	extraRaise := `CREATE OR REPLACE FUNCTION eshu_security.validate_scanner_source_admission_v1() RETURNS void AS $$
BEGIN
    RAISE EXCEPTION 'scanner canonical admission input invalid';
    RAISE EXCEPTION 'scanner brand new authority outage';
END; $$;`
	if got := admissionAuthorityRaiseMessages(extraRaise); equalStringSets(got, scannerAdmissionFatalRaises) {
		t.Fatalf("planted extra RAISE passed the allowlist: %v", got)
	}
	convertedFatal := `CREATE OR REPLACE FUNCTION eshu_security.validate_scanner_source_admission_v1() RETURNS void AS $$
BEGIN
    IF NOT FOUND THEN RETURN; END IF;
    RAISE EXCEPTION 'scanner hosted boundary invalid';
END; $$;`
	if got := admissionAuthorityRaiseMessages(convertedFatal); equalStringSets(got, scannerAdmissionFatalRaises) {
		t.Fatalf("converted fatal passed the allowlist: %v", got)
	}
	unknownRedefinition := []Definition{
		{Name: "x", Path: "go/internal/storage/postgres/migrations/999_sneaky.sql", SQL: extraRaise},
	}
	if got := baseNames(admissionAuthorityDefinitions(unknownRedefinition)); equalStringSets(got, scannerAdmissionDefiningMigrations) {
		t.Fatalf("unknown redefinition passed the defining set: %v", got)
	}
}
