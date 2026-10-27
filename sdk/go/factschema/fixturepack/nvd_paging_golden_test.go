// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fixturepack

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	factschema "github.com/eshu-hq/eshu/sdk/go/factschema"
)

type nvdGoldenFact struct {
	Kind          string          `json:"kind"`
	StableFactKey string          `json:"stable_fact_key"`
	Payload       json.RawMessage `json:"payload"`
}

func TestNVDPagingGoldenContentAndCopy(t *testing.T) {
	raw := NVDPagingGolden()
	if err := validateNVDPagingGolden(raw); err != nil {
		t.Fatal(err)
	}
	raw[0] = 0
	if NVDPagingGolden()[0] == 0 {
		t.Fatal("accessor returned mutable embedded fixture bytes")
	}
}

func TestNVDPagingGoldenRejectsSeededCorruption(t *testing.T) {
	original := string(NVDPagingGolden())
	cases := map[string][2]string{
		"response identity":     {`"id": "CVE-2026-0002"`, `"id": "CVE-9999-CORRUPT"`},
		"snapshot completeness": {`"complete": true`, `"complete": false`},
		"snapshot count":        {`"result_count": 2`, `"result_count": 999`},
		"missing advisory":      {`"advisory_id": "CVE-2026-0001",`, ``},
		"stable key":            {`"stable_fact_key": "9b02f30b90e787bb50a014746e88b3857cbe5345e8c9abf3380d19da4084a35b"`, `"stable_fact_key": "0000000000000000000000000000000000000000000000000000000000000000"`},
	}
	for name, replacement := range cases {
		t.Run(name, func(t *testing.T) {
			mutated := strings.Replace(original, replacement[0], replacement[1], 1)
			if mutated == original {
				t.Fatal("seeded violation did not change the fixture")
			}
			if err := validateNVDPagingGolden([]byte(mutated)); err == nil {
				t.Fatal("seeded violation passed golden validation")
			}
		})
	}
}

func validateNVDPagingGolden(raw []byte) error {
	var golden struct {
		Responses     []json.RawMessage `json:"responses"`
		ExpectedFacts []nvdGoldenFact   `json:"expected_facts"`
	}
	if err := json.Unmarshal(raw, &golden); err != nil {
		return fmt.Errorf("decode NVD paging golden: %w", err)
	}
	if len(golden.Responses) != 2 || len(golden.ExpectedFacts) != 3 {
		return fmt.Errorf("NVD paging golden has %d responses and %d facts, want 2 and 3", len(golden.Responses), len(golden.ExpectedFacts))
	}
	ids := []string{"CVE-2026-0001", "CVE-2026-0002"}
	for i, rawPage := range golden.Responses {
		var page struct {
			StartIndex      int `json:"startIndex"`
			ResultsPerPage  int `json:"resultsPerPage"`
			TotalResults    int `json:"totalResults"`
			Vulnerabilities []struct {
				CVE struct {
					ID string `json:"id"`
				} `json:"cve"`
			} `json:"vulnerabilities"`
		}
		if err := json.Unmarshal(rawPage, &page); err != nil {
			return fmt.Errorf("decode NVD page %d: %w", i, err)
		}
		if page.StartIndex != i || page.ResultsPerPage != 1 || page.TotalResults != 2 ||
			len(page.Vulnerabilities) != 1 || page.Vulnerabilities[0].CVE.ID != ids[i] {
			return fmt.Errorf("NVD page %d no longer matches the two-page input", i)
		}
	}
	kinds := []string{"vulnerability.source_snapshot", "vulnerability.cve", "vulnerability.cve"}
	keys := []string{
		"9b02f30b90e787bb50a014746e88b3857cbe5345e8c9abf3380d19da4084a35b",
		"3d5ec65605344f96a5d8a8bf855ff9ce626079d9ea9d0c4ca29a3a75856e1d78",
		"9ca478d91daa1cfced028ac7df9427c72bf6ec9642f29011fef45304783ea26e",
	}
	for i, fact := range golden.ExpectedFacts {
		if fact.Kind != kinds[i] || fact.StableFactKey != keys[i] {
			return fmt.Errorf("expected fact %d kind or stable key changed", i)
		}
		var payload map[string]any
		if err := json.Unmarshal(fact.Payload, &payload); err != nil {
			return fmt.Errorf("decode expected fact %d payload: %w", i, err)
		}
		env := factschema.Envelope{FactKind: fact.Kind, SchemaVersion: "1.0.0", Payload: payload}
		if i == 0 {
			if payload["complete"] != true || payload["result_count"] != float64(2) || payload["source"] != "nvd" {
				return fmt.Errorf("source snapshot payload no longer reports complete two-page collection")
			}
			if _, err := factschema.DecodeVulnerabilitySourceSnapshot(env); err != nil {
				return fmt.Errorf("decode source snapshot contract: %w", err)
			}
			continue
		}
		if payload["cve_id"] != ids[i-1] || payload["advisory_id"] != ids[i-1] || payload["source"] != "nvd" {
			return fmt.Errorf("CVE %d identity no longer matches input page", i)
		}
		if _, err := factschema.DecodeVulnerabilityCVE(env); err != nil {
			return fmt.Errorf("decode CVE contract: %w", err)
		}
	}
	return nil
}
