// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fixturepack

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	factschema "github.com/eshu-hq/eshu/sdk/go/factschema"
)

// requiredTermsSources are the sources whose snapshots carried no terms before
// eshu-hq/eshu#9958. The golden must name each one.
var requiredTermsSources = []string{"cisa_kev", "first_epss", "nvd", "osv"}

var licenseIdentifier = regexp.MustCompile(`^[A-Za-z0-9.+-]+$`)

const maxAttributionLen = 300

func TestSourceTermsGoldenFullContentDigest(t *testing.T) {
	const expected = "2c60495311714fcac206c7df467657c8d7e0430370a4cd21554374046ee5b335"
	got := fmt.Sprintf("%x", sha256.Sum256(SourceTermsGolden()))
	if got != expected {
		t.Fatalf("source terms golden digest = %s, want %s; review every source before updating the pin", got, expected)
	}
}

func TestSourceTermsGoldenContentAndCopy(t *testing.T) {
	raw := SourceTermsGolden()
	if err := validateSourceTermsGolden(raw); err != nil {
		t.Fatal(err)
	}
	raw[0] = 0
	if SourceTermsGolden()[0] == 0 {
		t.Fatal("accessor returned mutable embedded fixture bytes")
	}
}

// TestSourceTermsGoldenRejectsSeededCorruption plants one violation of each
// rule and requires the validator to reject it, so the validator cannot pass
// vacuously.
func TestSourceTermsGoldenRejectsSeededCorruption(t *testing.T) {
	original := string(SourceTermsGolden())
	cases := map[string][2]string{
		"license left blank":       {`"license": "CC0-1.0"`, `"license": ""`},
		"license with a space":     {`"license": "CC0-1.0"`, `"license": "CC0 1.0"`},
		"license a free sentence":  {`"license": "NOASSERTION"`, `"license": "no stated license"`},
		"attribution left blank":   {`"attribution": "EPSS scores are provided by FIRST, https://www.first.org/epss"`, `"attribution": ""`},
		"attribution on two lines": {`EPSS scores are provided by FIRST,`, `EPSS scores are\nprovided by FIRST,`},
		"attribution with a tab":   {`EPSS scores are provided by FIRST,`, `EPSS scores are\tprovided by FIRST,`},
		"attribution too long": {
			`"attribution": "EPSS scores are provided by FIRST, https://www.first.org/epss"`,
			`"attribution": "` + strings.Repeat("x", maxAttributionLen+1) + `"`,
		},
		"source dropped":           {`"source": "osv"`, `"source": "osv_other"`},
		"source name untrimmed":    {`"source": "nvd"`, `"source": "nvd "`},
		"duplicate source":         {`"source": "first_epss"`, `"source": "cisa_kev"`},
		"sources out of order":     {`"source": "cisa_kev"`, `"source": "zzz_kev"`},
		"unknown extra source key": {`"source": "nvd",`, `"source": "nvd", "url": "https://nvd.nist.gov",`},
	}
	for name, replacement := range cases {
		t.Run(name, func(t *testing.T) {
			mutated := strings.Replace(original, replacement[0], replacement[1], 1)
			if mutated == original {
				t.Fatal("seeded violation did not change the fixture")
			}
			if err := validateSourceTermsGolden([]byte(mutated)); err == nil {
				t.Fatal("seeded violation passed golden validation")
			}
		})
	}
}

// TestSourceTermsGoldenSnapshotsDecodeAndRoundTrip builds the payload each
// entry describes and proves the typed contract decodes it onto the license and
// attribution fields and encodes the same two keys back.
func TestSourceTermsGoldenSnapshotsDecodeAndRoundTrip(t *testing.T) {
	entries, err := parseSourceTermsGolden(SourceTermsGolden())
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		t.Run(entry.Source, func(t *testing.T) {
			snapshot, err := factschema.DecodeVulnerabilitySourceSnapshot(factschema.Envelope{
				FactKind:      factschema.FactKindVulnerabilitySourceSnapshot,
				SchemaVersion: "1.0.0",
				Payload: map[string]any{
					"source":      entry.Source,
					"complete":    true,
					"license":     entry.License,
					"attribution": entry.Attribution,
				},
			})
			if err != nil {
				t.Fatalf("DecodeVulnerabilitySourceSnapshot() error = %v", err)
			}
			if snapshot.License == nil || *snapshot.License != entry.License {
				t.Fatalf("License = %v, want %q", snapshot.License, entry.License)
			}
			if snapshot.Attribution == nil || *snapshot.Attribution != entry.Attribution {
				t.Fatalf("Attribution = %v, want %q", snapshot.Attribution, entry.Attribution)
			}
			payload, err := factschema.EncodeVulnerabilitySourceSnapshot(snapshot)
			if err != nil {
				t.Fatalf("EncodeVulnerabilitySourceSnapshot() error = %v", err)
			}
			if payload["license"] != entry.License || payload["attribution"] != entry.Attribution {
				t.Fatalf("encoded terms = (%v, %v), want (%q, %q)",
					payload["license"], payload["attribution"], entry.License, entry.Attribution)
			}
		})
	}
}

type sourceTermsEntry struct {
	Source      string `json:"source"`
	License     string `json:"license"`
	Attribution string `json:"attribution"`
}

func parseSourceTermsGolden(raw []byte) ([]sourceTermsEntry, error) {
	var golden struct {
		Sources []json.RawMessage `json:"sources"`
	}
	if err := json.Unmarshal(raw, &golden); err != nil {
		return nil, fmt.Errorf("decode source terms golden: %w", err)
	}
	entries := make([]sourceTermsEntry, 0, len(golden.Sources))
	for i, rawEntry := range golden.Sources {
		decoder := json.NewDecoder(strings.NewReader(string(rawEntry)))
		decoder.DisallowUnknownFields()
		var entry sourceTermsEntry
		if err := decoder.Decode(&entry); err != nil {
			return nil, fmt.Errorf("sources[%d]: %w", i, err)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func validateSourceTermsGolden(raw []byte) error {
	entries, err := parseSourceTermsGolden(raw)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for i, entry := range entries {
		if entry.Source == "" || entry.Source != strings.TrimSpace(entry.Source) {
			return fmt.Errorf("sources[%d]: source %q must be non-empty and trimmed", i, entry.Source)
		}
		if !licenseIdentifier.MatchString(entry.License) {
			return fmt.Errorf("%s: license %q is not an SPDX, LicenseRef- or NOASSERTION token", entry.Source, entry.License)
		}
		if strings.TrimSpace(entry.Attribution) == "" {
			return fmt.Errorf("%s: attribution must not be blank", entry.Source)
		}
		if strings.ContainsAny(entry.Attribution, "\r\n\t") {
			return fmt.Errorf("%s: attribution must be one line", entry.Source)
		}
		if len(entry.Attribution) > maxAttributionLen {
			return fmt.Errorf("%s: attribution is %d bytes, want at most %d", entry.Source, len(entry.Attribution), maxAttributionLen)
		}
		names = append(names, entry.Source)
	}
	if !slices.IsSorted(names) {
		return fmt.Errorf("sources %v must be sorted", names)
	}
	if len(slices.Compact(slices.Clone(names))) != len(names) {
		return fmt.Errorf("sources %v must be unique", names)
	}
	if !slices.Equal(names, requiredTermsSources) {
		return fmt.Errorf("sources = %v, want %v", names, requiredTermsSources)
	}
	return nil
}

// TestNVDPagingGoldenSnapshotCarriesSourceTerms proves the NVD paging golden's
// expected source_snapshot payload carries the same terms as the source terms
// golden, so the two shared fixtures cannot drift apart.
func TestNVDPagingGoldenSnapshotCarriesSourceTerms(t *testing.T) {
	entries, err := parseSourceTermsGolden(SourceTermsGolden())
	if err != nil {
		t.Fatal(err)
	}
	var want sourceTermsEntry
	for _, entry := range entries {
		if entry.Source == "nvd" {
			want = entry
		}
	}
	if want.Source == "" {
		t.Fatal("source terms golden has no nvd entry")
	}
	var golden struct {
		ExpectedFacts []struct {
			Kind    string         `json:"kind"`
			Payload map[string]any `json:"payload"`
		} `json:"expected_facts"`
	}
	if err := json.Unmarshal(NVDPagingGolden(), &golden); err != nil {
		t.Fatalf("decode NVD paging golden: %v", err)
	}
	seen := false
	for _, fact := range golden.ExpectedFacts {
		if fact.Kind != "vulnerability.source_snapshot" {
			continue
		}
		seen = true
		if fact.Payload["license"] != want.License || fact.Payload["attribution"] != want.Attribution {
			t.Fatalf("NVD golden snapshot terms = (%v, %v), want (%q, %q)",
				fact.Payload["license"], fact.Payload["attribution"], want.License, want.Attribution)
		}
	}
	if !seen {
		t.Fatal("NVD paging golden has no source_snapshot fact")
	}
}
