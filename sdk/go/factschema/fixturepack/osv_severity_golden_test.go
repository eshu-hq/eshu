// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fixturepack

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	factschema "github.com/eshu-hq/eshu/sdk/go/factschema"
)

func TestOSVSeverityGoldenFullContentDigest(t *testing.T) {
	const expected = "edb590c2d98a7d0808e513f98f64fc90955b9391a92272d64f447d0ed16f7b98"
	got := fmt.Sprintf("%x", sha256.Sum256(OSVSeverityGolden()))
	if got != expected {
		t.Fatalf("OSV severity golden digest = %s, want %s; review every case before updating the pin", got, expected)
	}
}

func TestOSVSeverityGoldenContentAndCopy(t *testing.T) {
	raw := OSVSeverityGolden()
	if err := validateOSVSeverityGolden(raw); err != nil {
		t.Fatal(err)
	}
	raw[0] = 0
	if OSVSeverityGolden()[0] == 0 {
		t.Fatal("accessor returned mutable embedded fixture bytes")
	}
}

func TestOSVSeverityGoldenRejectsSeededCorruption(t *testing.T) {
	original := string(OSVSeverityGolden())
	cases := map[string][2]string{
		"unsupported label":                    {`"severity_label": "medium",`, `"severity_label": "moderate",`},
		"v2 vector accepted":                   {`"cvss_vector": "",`, `"cvss_vector": "AV:N/AC:L/Au:N/C:P/I:P/A:P",`},
		"score key allowed":                    {`"expected_absent_payload_keys": ["cvss_score"]`, `"expected_absent_payload_keys": []`},
		"label drifted from database_specific": {`"severity_label": "medium",`, `"severity_label": "high",`},
		"record label changed, expected kept":  {`"database_specific": {"severity": "HIGH"}`, `"database_specific": {"severity": "LOW"}`},
		"cvss_score in expected":               {`"advisory_id": "GHSA-aaaa-0001-0001",`, `"advisory_id": "GHSA-aaaa-0001-0001", "cvss_score": 9.8,`},
		"advisory id drifted":                  {`"advisory_id": "GHSA-aaaa-0001-0001",`, `"advisory_id": "GHSA-WRONG",`},
		"severity echo drifted":                {`"severity": null`, `"severity": []`},
		"v3 preferred over v4":                 {`"cvss_vector": "CVSS:4.0/AV:N/AC:L/AT:N/PR:N/UI:N/VC:H/VI:H/VA:H/SC:N/SI:N/SA:N",`, `"cvss_vector": "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",`},
	}
	for name, replacement := range cases {
		t.Run(name, func(t *testing.T) {
			mutated := strings.Replace(original, replacement[0], replacement[1], 1)
			if mutated == original {
				t.Fatal("seeded violation did not change the fixture")
			}
			if err := validateOSVSeverityGolden([]byte(mutated)); err == nil {
				t.Fatal("seeded violation passed golden validation")
			}
		})
	}
}

// osvSeverityLabel is the documented label rule: trim, upper-case, MODERATE as
// medium, one of CRITICAL, HIGH, MEDIUM, LOW, otherwise empty. A non-string
// value gives empty.
func osvSeverityLabel(value any) string {
	raw, ok := value.(string)
	if !ok {
		return ""
	}
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "CRITICAL":
		return "critical"
	case "HIGH":
		return "high"
	case "MODERATE", "MEDIUM":
		return "medium"
	case "LOW":
		return "low"
	}
	return ""
}

// osvExpectedKeys is the exact key set of every case's expected map. Any other
// key, such as cvss_score, is drift.
var osvExpectedKeys = map[string]bool{"advisory_id": true, "cvss_vector": true, "severity_label": true, "severity": true}

type osvGoldenSeverityEntry struct {
	Type  string `json:"type"`
	Score string `json:"score"`
}

// validateOSVSeverityGolden derives every expected field from the case's own
// OSV record by the documented rules and requires the expected map to match,
// so a drifted expectation cannot pass by being internally consistent.
func validateOSVSeverityGolden(raw []byte) error {
	var golden struct {
		Cases []struct {
			Name   string `json:"name"`
			Record struct {
				ID       string                   `json:"id"`
				Severity []osvGoldenSeverityEntry `json:"severity"`
				Database map[string]any           `json:"database_specific"`
			} `json:"record"`
			Expected map[string]any `json:"expected"`
		} `json:"cases"`
		Absent []string `json:"expected_absent_payload_keys"`
	}
	if err := json.Unmarshal(raw, &golden); err != nil {
		return fmt.Errorf("decode OSV severity golden: %w", err)
	}
	if len(golden.Cases) != 10 {
		return fmt.Errorf("OSV severity golden has %d cases, want 10", len(golden.Cases))
	}
	if len(golden.Absent) != 1 || golden.Absent[0] != "cvss_score" {
		return fmt.Errorf("OSV severity golden must pin cvss_score as absent, got %v", golden.Absent)
	}
	for _, tc := range golden.Cases {
		for key := range tc.Expected {
			if !osvExpectedKeys[key] {
				return fmt.Errorf("case %q expected map carries %q; the OSV envelope emits only advisory_id, cvss_vector, severity_label, severity", tc.Name, key)
			}
		}
		for _, key := range golden.Absent {
			if _, present := tc.Expected[key]; present {
				return fmt.Errorf("case %q expects the absent key %q", tc.Name, key)
			}
		}
		if id := strings.TrimSpace(tc.Record.ID); id == "" || tc.Expected["advisory_id"] != id {
			return fmt.Errorf("case %q advisory_id %v does not equal the record id %q", tc.Name, tc.Expected["advisory_id"], id)
		}
		label, _ := tc.Expected["severity_label"].(string)
		if want := osvSeverityLabel(tc.Record.Database["severity"]); label != want {
			return fmt.Errorf("case %q severity_label %q does not follow database_specific.severity (want %q)", tc.Name, label, want)
		}
		vector, _ := tc.Expected["cvss_vector"].(string)
		// A vector must come from a CVSS_V3 or CVSS_V4 entry of the record, and
		// CVSS_V4 outranks CVSS_V3. CVSS_V2 and unknown types never fill it.
		want, hasV4 := "", false
		var echo []osvGoldenSeverityEntry
		for _, entry := range tc.Record.Severity {
			kind := strings.ToUpper(strings.TrimSpace(entry.Type))
			score := strings.TrimSpace(entry.Score)
			if kind != "" || score != "" {
				echo = append(echo, osvGoldenSeverityEntry{Type: strings.TrimSpace(entry.Type), Score: score})
			}
			switch {
			case kind == "CVSS_V4" && strings.HasPrefix(score, "CVSS:4.0/") && !hasV4:
				want, hasV4 = score, true
			case kind == "CVSS_V3" && strings.HasPrefix(score, "CVSS:3.") && want == "" && !hasV4:
				want = score
			}
		}
		if vector != want {
			return fmt.Errorf("case %q cvss_vector %q does not follow the V4, V3, never-V2 rule (want %q)", tc.Name, vector, want)
		}
		// The raw severity array echoes the record's entries, trimmed; no
		// entries gives null, not an empty array.
		wantRaw, err := json.Marshal(echo)
		if err != nil {
			return fmt.Errorf("case %q derived severity is not JSON: %w", tc.Name, err)
		}
		var wantEcho any
		if err := json.Unmarshal(wantRaw, &wantEcho); err != nil {
			return fmt.Errorf("case %q derived severity does not decode: %w", tc.Name, err)
		}
		if !reflect.DeepEqual(tc.Expected["severity"], wantEcho) {
			return fmt.Errorf("case %q severity %v does not echo the record entries %v", tc.Name, tc.Expected["severity"], wantEcho)
		}
		payload := map[string]any{"advisory_id": tc.Expected["advisory_id"], "source": "osv"}
		if vector != "" {
			payload["cvss_vector"] = vector
		}
		if label != "" {
			payload["severity_label"] = label
		}
		env := factschema.Envelope{FactKind: "vulnerability.cve", SchemaVersion: "1.0.0", Payload: payload}
		if _, err := factschema.DecodeVulnerabilityCVE(env); err != nil {
			return fmt.Errorf("case %q does not decode through the CVE contract: %w", tc.Name, err)
		}
	}
	return nil
}
