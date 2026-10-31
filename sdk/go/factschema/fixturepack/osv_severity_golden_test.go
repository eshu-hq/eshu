// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fixturepack

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
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
		"unsupported label":    {`"severity_label": "medium",`, `"severity_label": "moderate",`},
		"v2 vector accepted":   {`"cvss_vector": "",`, `"cvss_vector": "AV:N/AC:L/Au:N/C:P/I:P/A:P",`},
		"score key allowed":    {`"expected_absent_payload_keys": ["cvss_score"]`, `"expected_absent_payload_keys": []`},
		"v3 preferred over v4": {`"cvss_vector": "CVSS:4.0/AV:N/AC:L/AT:N/PR:N/UI:N/VC:H/VI:H/VA:H/SC:N/SI:N/SA:N",`, `"cvss_vector": "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",`},
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

// osvSeverityLabels is the label vocabulary a conforming OSV collector writes.
var osvSeverityLabels = map[string]bool{"": true, "critical": true, "high": true, "medium": true, "low": true}

func validateOSVSeverityGolden(raw []byte) error {
	var golden struct {
		Cases []struct {
			Name   string `json:"name"`
			Record struct {
				Severity []struct {
					Type  string `json:"type"`
					Score string `json:"score"`
				} `json:"severity"`
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
		label, _ := tc.Expected["severity_label"].(string)
		if !osvSeverityLabels[label] {
			return fmt.Errorf("case %q label %q is outside critical, high, medium, low or empty", tc.Name, label)
		}
		vector, _ := tc.Expected["cvss_vector"].(string)
		// A vector must come from a CVSS_V3 or CVSS_V4 entry of the record, and
		// CVSS_V4 outranks CVSS_V3. CVSS_V2 and unknown types never fill it.
		want, hasV4 := "", false
		for _, entry := range tc.Record.Severity {
			kind := strings.ToUpper(strings.TrimSpace(entry.Type))
			score := strings.TrimSpace(entry.Score)
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
