// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ifa"
)

// writeDirectFamilyCassetteFixture writes one throwaway cassette body to a
// temp file and returns its path, so the negative cases below exercise the
// real loader against real bytes rather than an in-memory stand-in.
func writeDirectFamilyCassetteFixture(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cassette.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write temp cassette: %v", err)
	}
	return path
}

// canonicalOduJSON renders an Odù to canonical JSON so the lockstep comparison
// is by MEANING rather than by Go type.
//
// reflect.DeepEqual is the wrong instrument here, and not for a cosmetic
// reason. The compiled Odù builds its payload from typed factschema values, so
// kubernetes_namespace_environment's nested `labels` arrives as a
// map[string]string. The cassette projection decodes the same bytes from JSON,
// where it is necessarily a map[string]any. The two describe identical facts
// and DeepEqual still reports them different.
//
// The JSON form is also the one that matters. In production a fact travels
// collector -> JSON -> Postgres -> reducer, so what an extractor actually reads
// is the decoded map[string]any, not the builder's typed map -- confirmed on a
// live stack, where driving this cassette produced exactly the expected
// TARGETS_ENVIRONMENT edges. Comparing canonical JSON compares the fixture both
// halves really mean, and still fails on any added, dropped, renamed or
// re-valued field. json.Marshal sorts map keys, so the rendering is stable.
func canonicalOduJSON(t *testing.T, odu ifa.Odu) string {
	t.Helper()
	blob, err := json.Marshal(odu)
	if err != nil {
		t.Fatalf("marshal Odù %q: %v", odu.Name, err)
	}
	return string(blob)
}

// Compiled-catalog/cassette lockstep for the two direct-materialization
// families (#6228).
//
// Each family keeps the same fixture twice: the Go-compiled Odù that
// catalog_seed.go registers and the vacuity guard resolves through, and the
// committed cassette (a recorded collector output the gates replay instead of
// calling a real cluster or cloud account) that the live matrices drive. The
// guard proves the compiled half; the live gate proves the cassette half. Only
// these tests prove they are the same fixture.
//
// Without them the two drift silently, and the failure is bad in a specific
// way: the offline guard keeps passing on the compiled Odù while the live gate
// drives different facts, so a coverage row would attest to a proof over
// something other than what it names. That is the same class of defect as a
// guard that repairs the drift it exists to report, which this branch already
// had to fix once.

// directFamilyCassetteCase pairs a family's compiled Odù with the loader and
// path for its committed cassette, so both families run the identical
// comparison instead of two hand-copied bodies that can diverge.
type directFamilyCassetteCase struct {
	// name is the family's Odù catalog name, used to look the compiled Odù up
	// and to name the subtest.
	name string
	// compiled builds the Go-side Odù that catalog_seed.go registers.
	compiled func() ifa.CatalogOdu
	// cassettePath resolves the committed cassette against a repo root.
	cassettePath func(string) string
	// load projects that cassette back into an Odù.
	load func(string) (ifa.Odu, error)
}

func directFamilyCassetteCases() []directFamilyCassetteCase {
	return []directFamilyCassetteCase{
		{
			name:         ifa.KubernetesNamespaceEnvironmentFamilyOduName,
			compiled:     ifa.KubernetesNamespaceEnvironmentFamilyOdu,
			cassettePath: ifa.KubernetesNamespaceEnvironmentFamilyCassetteFullPath,
			load:         ifa.LoadKubernetesNamespaceEnvironmentFamilyOdu,
		},
		{
			name:         ifa.IAMInstanceProfileRoleFamilyOduName,
			compiled:     ifa.IAMInstanceProfileRoleFamilyOdu,
			cassettePath: ifa.IAMInstanceProfileRoleFamilyCassetteFullPath,
			load:         ifa.LoadIAMInstanceProfileRoleFamilyOdu,
		},
	}
}

// TestDirectFamilyCassettesMatchTheirCompiledOdu is the lockstep assertion. It
// compares against the REGISTERED catalog entry, not only the builder, so
// dropping a family from catalogSeed fails here too rather than passing on a
// builder nothing installs.
func TestDirectFamilyCassettesMatchTheirCompiledOdu(t *testing.T) {
	t.Parallel()

	repoRoot := repoRootDir(t)
	catalog := ifa.CatalogByName()
	for _, tc := range directFamilyCassetteCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			registered, ok := catalog[tc.name]
			if !ok {
				t.Fatalf("CatalogByName omits %q -- restore its entry in catalog_seed.go's catalogSeed; without it the installed binary cannot resolve this family's manifest row", tc.name)
			}
			if !reflect.DeepEqual(registered, tc.compiled().Odu) {
				t.Fatalf("registered catalog Odù for %q differs from its builder's output", tc.name)
			}

			fromCassette, err := tc.load(tc.cassettePath(repoRoot))
			if err != nil {
				t.Fatalf("load committed cassette for %q: %v", tc.name, err)
			}
			compiledJSON := canonicalOduJSON(t, registered)
			cassetteJSON := canonicalOduJSON(t, fromCassette)
			if compiledJSON != cassetteJSON {
				t.Fatalf(
					"compiled catalog Odù drifted from the committed cassette projection for %q\ncompiled: %s\ncassette: %s",
					tc.name, compiledJSON, cassetteJSON,
				)
			}
		})
	}
}

// TestDirectFamilyCassetteLoaderRejectsAnEmptyFactList proves the loader fails
// closed rather than returning an Odù with no facts.
//
// This is the case that matters most for a lockstep test, because an empty
// projection compared against an empty compiled Odù would pass while proving
// nothing. The loader must reject it before the comparison ever runs.
func TestDirectFamilyCassetteLoaderRejectsAnEmptyFactList(t *testing.T) {
	t.Parallel()

	path := writeDirectFamilyCassetteFixture(t, `{"collector":"aws","schema_version":"1","scopes":[{"scope_id":"s","source_system":"aws","scope_kind":"region","collector_kind":"aws","partition_key":"s","metadata":{},"generation_id":"g","observed_at":"2026-08-18T00:00:00Z","trigger_kind":"snapshot","facts":[]}]}`)
	if _, err := ifa.LoadIAMInstanceProfileRoleFamilyOdu(path); err == nil {
		t.Fatal("LoadIAMInstanceProfileRoleFamilyOdu(empty fact list) = nil error, want a fail-closed rejection")
	}
}

// TestDirectFamilyCassetteLoaderRejectsAnUnknownField proves the decoder's
// DisallowUnknownFields is active. A typo'd envelope key would otherwise decode
// to a zero value, and the cassette would silently project a fact set that is
// not the one it appears to describe.
func TestDirectFamilyCassetteLoaderRejectsAnUnknownField(t *testing.T) {
	t.Parallel()

	path := writeDirectFamilyCassetteFixture(t, `{"collector":"aws","schema_version":"1","scopes":[{"scope_id":"s","source_system":"aws","scope_kind":"region","collector_kind":"aws","partition_key":"s","metadata":{},"generation_id":"g","observed_at":"2026-08-18T00:00:00Z","trigger_kind":"snapshot","facts":[{"fact_knd":"aws_resource","stable_fact_key":"k","schema_version":"1.0.0","collector_kind":"aws","fencing_token":1,"source_confidence":"observed","payload":{}}]}]}`)
	if _, err := ifa.LoadIAMInstanceProfileRoleFamilyOdu(path); err == nil {
		t.Fatal("LoadIAMInstanceProfileRoleFamilyOdu(fact_knd typo) = nil error, want the unknown-field rejection")
	}
}
