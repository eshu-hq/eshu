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
	const expected = "9e7b53ee9e490c3d71130762f3f73beae2f586aecff87e86d9003e97eff54fee"
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
		"empty label key dropped":              {"\"cvss_vector\": \"\",\n        \"severity_label\": \"\",\n        \"severity\": [\n          {\"type\": \"Ubuntu\"", "\"cvss_vector\": \"\",\n        \"severity\": [\n          {\"type\": \"Ubuntu\""},
		"null severity key dropped":            {"\"severity_label\": \"\",\n        \"severity\": null\n      }\n    },\n    {\n      \"name\": \"mixed_case", "\"severity_label\": \"\"\n      }\n    },\n    {\n      \"name\": \"mixed_case"},
		"empty vector key dropped":             {"\"advisory_id\": \"GHSA-aaaa-0004-0004\",\n        \"cvss_vector\": \"\",", "\"advisory_id\": \"GHSA-aaaa-0004-0004\","},
		"vector of the wrong JSON type":        {"\"advisory_id\": \"GHSA-aaaa-0004-0004\",\n        \"cvss_vector\": \"\"", "\"advisory_id\": \"GHSA-aaaa-0004-0004\",\n        \"cvss_vector\": 0"},
		"label of the wrong JSON type":         {"\"cvss_vector\": \"\",\n        \"severity_label\": \"\",\n        \"severity\": [\n          {\"type\": \"Ubuntu\"", "\"cvss_vector\": \"\",\n        \"severity_label\": 0,\n        \"severity\": [\n          {\"type\": \"Ubuntu\""},
		"absent key list renamed":              {`["cvss_score"]`, `["cvss_scores"]`},
		"absent key list deleted":              {",\n  \"expected_absent_payload_keys\": [\"cvss_score\"]", ""},
		"record id removed":                    {`"id": "GHSA-aaaa-0003-0003",`, ``},
		"whitespace score in record":           {`{"type": "CVSS_V4", "score": "9.8"}`, `{"type": "CVSS_V4", "score": "   "}`},
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

// TestOSVSeverityGoldenKeepsEveryRuleShape replaces a case with a copy of
// another, which keeps the case count and every per-case value consistent, and
// requires the validator to notice the lost rule shape.
func TestOSVSeverityGoldenKeepsEveryRuleShape(t *testing.T) {
	var golden map[string]any
	if err := json.Unmarshal(OSVSeverityGolden(), &golden); err != nil {
		t.Fatal(err)
	}
	cases, _ := golden["cases"].([]any)
	if len(cases) != 12 {
		t.Fatalf("golden has %d cases, want 12", len(cases))
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
			if err := validateOSVSeverityGolden(raw); err == nil {
				t.Fatal("a lost rule shape passed golden validation")
			}
		})
	}
}

// TestOSVSeverityGoldenPinsTheCaseList removes and adds a case and requires the
// validator to notice either.
func TestOSVSeverityGoldenPinsTheCaseList(t *testing.T) {
	var golden map[string]any
	if err := json.Unmarshal(OSVSeverityGolden(), &golden); err != nil {
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
			if err := validateOSVSeverityGolden(raw); err == nil {
				t.Fatal("a changed case list passed golden validation")
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

// osvExpectedKeys is the exact key set of every case's expected map: each key
// must be present, even when its value is empty or null, and no other key may
// appear. A key such as cvss_score is drift.
var osvExpectedKeys = map[string]bool{"advisory_id": true, "cvss_vector": true, "severity_label": true, "severity": true}

// osvRuleShapes lists the rule shapes the golden must keep, each derived from
// the cases' own records. A case replaced by a copy of another still passes
// every per-case check, so this is what notices a lost shape. The V4-only and
// the empty-array shapes are required like the rest.
var osvRuleShapes = []string{
	"vector: v3 only", "vector: v4 over v3", "vector: first of two v3", "vector: v2 only stays empty",
	"vector: v2 skipped for v3", "vector: malformed v4 falls back to v3", "vector: unknown type ignored",
	"vector: v4 only", "severity: no entries", "severity: empty array", "type: case and whitespace",
	"label: critical", "label: high", "label: moderate is medium", "label: low", "label: unknown string",
	"label: non-string", "label: key missing", "label: case and whitespace",
}

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
	if len(golden.Cases) != 12 {
		return fmt.Errorf("OSV severity golden has %d cases, want 12", len(golden.Cases))
	}
	if len(golden.Absent) != 1 || golden.Absent[0] != "cvss_score" {
		return fmt.Errorf("OSV severity golden must pin cvss_score as absent, got %v", golden.Absent)
	}
	names, shapes := map[string]bool{}, map[string]bool{}
	for _, tc := range golden.Cases {
		if tc.Name == "" || names[tc.Name] {
			return fmt.Errorf("case name %q is empty or repeated", tc.Name)
		}
		names[tc.Name] = true
		for key := range tc.Expected {
			if !osvExpectedKeys[key] {
				return fmt.Errorf("case %q expected map carries %q; the OSV envelope emits only advisory_id, cvss_vector, severity_label, severity", tc.Name, key)
			}
		}
		for key := range osvExpectedKeys {
			if _, ok := tc.Expected[key]; !ok {
				return fmt.Errorf("case %q expected map is missing %q; the OSV envelope emits exactly advisory_id, cvss_vector, severity_label, severity", tc.Name, key)
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
		label, labelOK := tc.Expected["severity_label"].(string)
		vector, vectorOK := tc.Expected["cvss_vector"].(string)
		if !labelOK || !vectorOK {
			return fmt.Errorf("case %q severity_label and cvss_vector must be JSON strings, got %T and %T", tc.Name, tc.Expected["severity_label"], tc.Expected["cvss_vector"])
		}
		rawLabel, labelPresent := tc.Record.Database["severity"]
		if want := osvSeverityLabel(rawLabel); label != want {
			return fmt.Errorf("case %q severity_label %q does not follow database_specific.severity (want %q)", tc.Name, label, want)
		}
		// A vector must come from a CVSS_V3 or CVSS_V4 entry of the record, and
		// CVSS_V4 outranks CVSS_V3. CVSS_V2 and unknown types never fill it.
		want, hasV4, v3Count := "", false, 0
		var echo []osvGoldenSeverityEntry
		seen := map[string]bool{}
		for _, entry := range tc.Record.Severity {
			kind := strings.ToUpper(strings.TrimSpace(entry.Type))
			score := strings.TrimSpace(entry.Score)
			if kind != "" || score != "" {
				echo = append(echo, osvGoldenSeverityEntry{Type: strings.TrimSpace(entry.Type), Score: score})
			}
			seen[kind] = true
			cvssType := kind == "CVSS_V2" || kind == "CVSS_V3" || kind == "CVSS_V4"
			if (cvssType && entry.Type != kind) || entry.Score != score {
				shapes["type: case and whitespace"] = true
			}
			switch {
			case kind == "CVSS_V4" && strings.HasPrefix(score, "CVSS:4.0/") && !hasV4:
				want, hasV4 = score, true
			case kind == "CVSS_V3" && strings.HasPrefix(score, "CVSS:3."):
				v3Count++
				if want == "" && !hasV4 {
					want = score
				}
			case kind != "CVSS_V2" && kind != "CVSS_V3" && kind != "CVSS_V4":
				shapes["vector: unknown type ignored"] = true
			}
		}
		if vector != want {
			return fmt.Errorf("case %q cvss_vector %q does not follow the V4, V3, never-V2 rule (want %q)", tc.Name, vector, want)
		}
		switch {
		case hasV4 && v3Count > 0:
			shapes["vector: v4 over v3"] = true
		case want != "" && !hasV4 && v3Count == 1 && !seen["CVSS_V2"] && !seen["CVSS_V4"]:
			shapes["vector: v3 only"] = true
		case want != "" && v3Count > 1:
			shapes["vector: first of two v3"] = true
		}
		if want != "" && seen["CVSS_V2"] {
			shapes["vector: v2 skipped for v3"] = true
		}
		if want == "" && seen["CVSS_V2"] {
			shapes["vector: v2 only stays empty"] = true
		}
		if !hasV4 && seen["CVSS_V4"] && want != "" {
			shapes["vector: malformed v4 falls back to v3"] = true
		}
		if len(echo) == 0 && tc.Record.Severity == nil {
			shapes["severity: no entries"] = true
		}
		if len(echo) == 0 && tc.Record.Severity != nil {
			shapes["severity: empty array"] = true
		}
		if hasV4 && v3Count == 0 {
			shapes["vector: v4 only"] = true
		}
		switch {
		case !labelPresent:
			shapes["label: key missing"] = true
		case label == "" && fmt.Sprintf("%T", rawLabel) != "string":
			shapes["label: non-string"] = true
		case label == "":
			shapes["label: unknown string"] = true
		default:
			shapes["label: "+label] = true
			if text, _ := rawLabel.(string); strings.ToUpper(text) == "MODERATE" {
				shapes["label: moderate is medium"] = true
			}
			if text, _ := rawLabel.(string); text != strings.ToUpper(strings.TrimSpace(text)) {
				shapes["label: case and whitespace"] = true
			}
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
	for _, shape := range osvRuleShapes {
		if !shapes[shape] {
			return fmt.Errorf("OSV severity golden lost the rule shape %q", shape)
		}
	}
	return nil
}
