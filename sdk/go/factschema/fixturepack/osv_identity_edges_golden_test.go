// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fixturepack

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	factschema "github.com/eshu-hq/eshu/sdk/go/factschema"
)

func TestOSVIdentityEdgesGoldenFullContentDigest(t *testing.T) {
	const expected = "3df283f40ecbe08dcd559bd3e3500c865411d247d8e644383c52fda8b3266010"
	got := fmt.Sprintf("%x", sha256.Sum256(OSVIdentityEdgesGolden()))
	if got != expected {
		t.Fatalf("OSV identity edges golden digest = %s, want %s; review every case before updating the pin", got, expected)
	}
}

func TestOSVIdentityEdgesGoldenContentAndCopy(t *testing.T) {
	raw := OSVIdentityEdgesGolden()
	if err := validateOSVIdentityEdgesGolden(raw); err != nil {
		t.Fatal(err)
	}
	raw[0] = 0
	if OSVIdentityEdgesGolden()[0] == 0 {
		t.Fatal("accessor returned mutable embedded fixture bytes")
	}
}

func TestOSVIdentityEdgesGoldenRejectsSeededCorruption(t *testing.T) {
	original := string(OSVIdentityEdgesGolden())
	cases := map[string][2]string{
		"output left unsorted":         {`"upstream": ["CVE-2026-2002", "CVE-2026-2003"]`, `"upstream": ["CVE-2026-2003", "CVE-2026-2002"]`},
		"duplicate kept in output":     {`"related": ["GHSA-bbbb-0010-0010", "GHSA-bbbb-0011-0011"]`, `"related": ["GHSA-bbbb-0010-0010", "GHSA-bbbb-0011-0011", "GHSA-bbbb-0011-0011"]`},
		"untrimmed id kept in output":  {`"upstream": ["CVE-2026-2020", "CVE-2026-2021", "CVE-2026-2022"]`, `"upstream": ["CVE-2026-2020 ", "CVE-2026-2021", "CVE-2026-2022"]`},
		"upstream folded into aliases": {`"aliases": ["CVE-2026-2010", "DSA-9002-1-ALT"]`, `"aliases": ["CVE-2026-2010", "CVE-2026-2020", "DSA-9002-1-ALT"]`},
		"empty aliases written as array": {`"aliases": null,
        "related": ["GHSA-bbbb-0009-0009"]`, `"aliases": [],
        "related": ["GHSA-bbbb-0009-0009"]`},
		"empty aliases key dropped": {`"aliases": null,
        "related": ["GHSA-bbbb-0009-0009"]`, `"related": ["GHSA-bbbb-0009-0009"]`},
		"empty upstream written": {`"advisory_id": "GHSA-bbbb-0005-0005",`, `"advisory_id": "GHSA-bbbb-0005-0005", "upstream": [],`},
		"upstream key dropped when set": {`"related": null,
        "upstream": ["CVE-2026-2002", "CVE-2026-2003"]`, `"related": null`},
		"withdrawn status dropped": {`"related": null,
        "status": "withdrawn"`, `"related": null`},
		"status on an active record": {`"advisory_id": "GHSA-bbbb-0001-0001",`, `"advisory_id": "GHSA-bbbb-0001-0001", "status": "withdrawn",`},
		"wrong status text":          {`"status": "withdrawn"`, `"status": "Withdrawn"`},
		"advisory id drifted":        {`"advisory_id": "DSA-9001-1",`, `"advisory_id": "DSA-WRONG",`},
		"absent list dropped a key": {`"expected_absent_payload_keys": ["status"]
    },
    {
      "name": "all_three`, `"expected_absent_payload_keys": []
    },
    {
      "name": "all_three`},
		"absent list names a present key": {`"expected_absent_payload_keys": ["upstream"]`, `"expected_absent_payload_keys": ["upstream", "aliases"]`},
		"record input changed":            {`"aliases": ["DSA-9002-1-ALT", "  CVE-2026-2010", "CVE-2026-2010 ", ""]`, `"aliases": ["DSA-9002-1-ALT", "  CVE-2026-2010", "CVE-2026-2011 ", ""]`},
		"unexpected expected key":         {`"advisory_id": "GHSA-bbbb-0001-0001",`, `"advisory_id": "GHSA-bbbb-0001-0001", "cve_id": "CVE-2026-2001",`},
		"aliases of the wrong JSON type": {`"aliases": ["CVE-2026-2001"],
        "related": null`, `"aliases": "CVE-2026-2001",
        "related": null`},
		"record id removed": {`"id": "GHSA-bbbb-0003-0003",`, ``},
	}
	for name, replacement := range cases {
		t.Run(name, func(t *testing.T) {
			mutated := strings.Replace(original, replacement[0], replacement[1], 1)
			if mutated == original {
				t.Fatal("seeded violation did not change the fixture")
			}
			if err := validateOSVIdentityEdgesGolden([]byte(mutated)); err == nil {
				t.Fatal("seeded violation passed golden validation")
			}
		})
	}
}

// TestOSVIdentityEdgesGoldenKeepsEveryRuleShape replaces a case with a copy of
// another, which keeps the case count and every per-case value consistent, and
// requires the validator to notice the lost rule shape.
func TestOSVIdentityEdgesGoldenKeepsEveryRuleShape(t *testing.T) {
	var golden map[string]any
	if err := json.Unmarshal(OSVIdentityEdgesGolden(), &golden); err != nil {
		t.Fatal(err)
	}
	cases, _ := golden["cases"].([]any)
	if len(cases) != 7 {
		t.Fatalf("golden has %d cases, want 7", len(cases))
	}
	for victim := 1; victim < len(cases); victim++ {
		t.Run(fmt.Sprintf("case %d replaced by a copy of case 0", victim), func(t *testing.T) {
			mutated := append([]any(nil), cases...)
			clone := map[string]any{}
			for k, v := range cases[0].(map[string]any) {
				clone[k] = v
			}
			clone["name"] = fmt.Sprintf("copy_%d", victim)
			mutated[victim] = clone
			golden["cases"] = mutated
			raw, err := json.Marshal(golden)
			golden["cases"] = cases
			if err != nil {
				t.Fatal(err)
			}
			if err := validateOSVIdentityEdgesGolden(raw); err == nil {
				t.Fatal("a lost rule shape passed golden validation")
			}
		})
	}
}

// TestOSVIdentityEdgesGoldenPinsTheCaseList removes and adds a case and
// requires the validator to notice either.
func TestOSVIdentityEdgesGoldenPinsTheCaseList(t *testing.T) {
	var golden map[string]any
	if err := json.Unmarshal(OSVIdentityEdgesGolden(), &golden); err != nil {
		t.Fatal(err)
	}
	cases, _ := golden["cases"].([]any)
	for name, mutated := range map[string][]any{
		"case deleted":  cases[1:],
		"case appended": append(append([]any(nil), cases...), cases[0]),
		"cases emptied": {},
	} {
		t.Run(name, func(t *testing.T) {
			golden["cases"] = mutated
			raw, err := json.Marshal(golden)
			golden["cases"] = cases
			if err != nil {
				t.Fatal(err)
			}
			if err := validateOSVIdentityEdgesGolden(raw); err == nil {
				t.Fatal("a changed case list passed golden validation")
			}
		})
	}
}

// osvIdentityEdgesRuleShapes lists the rule shapes the golden must keep, each
// derived from the cases' own records. A case replaced by a copy of another
// still passes every per-case check, so this is what notices a lost shape.
var osvIdentityEdgesRuleShapes = []string{
	"edges: aliases only", "edges: related only", "edges: upstream only", "edges: all three",
	"input: unsorted", "input: duplicate", "input: blank entry", "input: surrounding whitespace",
	"upstream: not folded into aliases", "upstream: only blank entries adds no key",
	"output: empty edge list is null", "status: withdrawn", "status: empty withdrawn string stays active",
}

// cleanOSVEdge is the documented list rule: trim, drop blanks, de-duplicate,
// sort ascending. An empty result is nil, which serializes to JSON null.
func cleanOSVEdge(values []string) []string {
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !slices.Contains(out, value) {
			out = append(out, value)
		}
	}
	slices.Sort(out)
	return out
}

// osvEdgeJSON converts a cleaned list to the value a decoded JSON document
// holds for it: nil for an empty list, a []any of strings otherwise.
func osvEdgeJSON(values []string) any {
	if len(values) == 0 {
		return nil
	}
	out := make([]any, len(values))
	for i, value := range values {
		out[i] = value
	}
	return out
}

// validateOSVIdentityEdgesGolden derives every expected field from the case's
// own OSV record by the documented rules and requires the expected map and the
// absent-key list to match, so a drifted expectation cannot pass by being
// internally consistent.
func validateOSVIdentityEdgesGolden(raw []byte) error {
	var golden struct {
		Cases []struct {
			Name   string `json:"name"`
			Record struct {
				ID        string   `json:"id"`
				Withdrawn *string  `json:"withdrawn"`
				Aliases   []string `json:"aliases"`
				Related   []string `json:"related"`
				Upstream  []string `json:"upstream"`
			} `json:"record"`
			Expected map[string]any `json:"expected"`
			Absent   []string       `json:"expected_absent_payload_keys"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &golden); err != nil {
		return fmt.Errorf("decode OSV identity edges golden: %w", err)
	}
	if len(golden.Cases) != 7 {
		return fmt.Errorf("OSV identity edges golden has %d cases, want 7", len(golden.Cases))
	}
	names, shapes := map[string]bool{}, map[string]bool{}
	for _, tc := range golden.Cases {
		if tc.Name == "" || names[tc.Name] {
			return fmt.Errorf("case name %q is empty or repeated", tc.Name)
		}
		names[tc.Name] = true
		aliases, related, upstream := cleanOSVEdge(tc.Record.Aliases), cleanOSVEdge(tc.Record.Related), cleanOSVEdge(tc.Record.Upstream)
		withdrawn := tc.Record.Withdrawn != nil && strings.TrimSpace(*tc.Record.Withdrawn) != ""

		wantKeys := map[string]bool{"advisory_id": true, "aliases": true, "related": true}
		wantAbsent := []string{}
		if len(upstream) > 0 {
			wantKeys["upstream"] = true
		} else {
			wantAbsent = append(wantAbsent, "upstream")
		}
		if withdrawn {
			wantKeys["status"] = true
		} else {
			wantAbsent = append(wantAbsent, "status")
		}
		for key := range tc.Expected {
			if !wantKeys[key] {
				return fmt.Errorf("case %q expected map carries %q; the OSV envelope emits it for this record only when the rules allow", tc.Name, key)
			}
		}
		for key := range wantKeys {
			if _, ok := tc.Expected[key]; !ok {
				return fmt.Errorf("case %q expected map is missing %q", tc.Name, key)
			}
		}
		slices.Sort(wantAbsent)
		gotAbsent := append([]string{}, tc.Absent...)
		slices.Sort(gotAbsent)
		if !reflect.DeepEqual(gotAbsent, wantAbsent) {
			return fmt.Errorf("case %q expected_absent_payload_keys %v, want %v", tc.Name, tc.Absent, wantAbsent)
		}
		if id := strings.TrimSpace(tc.Record.ID); id == "" || tc.Expected["advisory_id"] != id {
			return fmt.Errorf("case %q advisory_id %v does not equal the record id %q", tc.Name, tc.Expected["advisory_id"], id)
		}
		for key, want := range map[string]any{"aliases": osvEdgeJSON(aliases), "related": osvEdgeJSON(related)} {
			if !reflect.DeepEqual(tc.Expected[key], want) {
				return fmt.Errorf("case %q %s %#v does not follow the trim, de-duplicate, sort rule (want %#v)", tc.Name, key, tc.Expected[key], want)
			}
		}
		if len(upstream) > 0 && !reflect.DeepEqual(tc.Expected["upstream"], osvEdgeJSON(upstream)) {
			return fmt.Errorf("case %q upstream %#v does not follow the trim, de-duplicate, sort rule (want %#v)", tc.Name, tc.Expected["upstream"], osvEdgeJSON(upstream))
		}
		if withdrawn && tc.Expected["status"] != "withdrawn" {
			return fmt.Errorf("case %q status %v, want withdrawn", tc.Name, tc.Expected["status"])
		}
		// upstream is directed: none of its ids may also be an alias, so a
		// distro bundle of CVEs cannot be passed off as an alias set.
		for _, id := range upstream {
			if slices.Contains(aliases, id) {
				return fmt.Errorf("case %q lists %q as both an alias and an upstream id", tc.Name, id)
			}
		}
		// Rule shapes, derived from the record.
		switch {
		case len(aliases) > 0 && len(related) == 0 && len(upstream) == 0:
			shapes["edges: aliases only"] = true
		case len(related) > 0 && len(aliases) == 0 && len(upstream) == 0:
			shapes["edges: related only"] = true
		case len(upstream) > 0 && len(aliases) == 0 && len(related) == 0:
			shapes["edges: upstream only"] = true
		case len(aliases) > 0 && len(related) > 0 && len(upstream) > 0:
			shapes["edges: all three"] = true
			shapes["upstream: not folded into aliases"] = true
		}
		for _, list := range [][]string{tc.Record.Aliases, tc.Record.Related, tc.Record.Upstream} {
			if !slices.IsSorted(list) {
				shapes["input: unsorted"] = true
			}
			seen := map[string]bool{}
			for _, value := range list {
				trimmed := strings.TrimSpace(value)
				switch {
				case trimmed == "":
					shapes["input: blank entry"] = true
				case trimmed != value:
					shapes["input: surrounding whitespace"] = true
				}
				if trimmed != "" && seen[trimmed] {
					shapes["input: duplicate"] = true
				}
				seen[trimmed] = true
			}
		}
		if len(tc.Record.Upstream) > 0 && len(upstream) == 0 {
			shapes["upstream: only blank entries adds no key"] = true
		}
		if len(aliases) == 0 || len(related) == 0 {
			shapes["output: empty edge list is null"] = true
		}
		switch {
		case withdrawn:
			shapes["status: withdrawn"] = true
		case tc.Record.Withdrawn != nil:
			shapes["status: empty withdrawn string stays active"] = true
		}
		// The derived payload must still decode through the CVE contract.
		payload := map[string]any{"advisory_id": tc.Expected["advisory_id"], "source": "osv"}
		if withdrawn {
			payload["status"] = "withdrawn"
		}
		env := factschema.Envelope{FactKind: "vulnerability.cve", SchemaVersion: "1.0.0", Payload: payload}
		if _, err := factschema.DecodeVulnerabilityCVE(env); err != nil {
			return fmt.Errorf("case %q does not decode through the CVE contract: %w", tc.Name, err)
		}
	}
	for _, shape := range osvIdentityEdgesRuleShapes {
		if !shapes[shape] {
			return fmt.Errorf("OSV identity edges golden lost the rule shape %q", shape)
		}
	}
	return nil
}
