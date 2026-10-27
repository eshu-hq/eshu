// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	"testing"

	vulnerabilityv1 "github.com/eshu-hq/eshu/sdk/go/factschema/vulnerability/v1"
)

func TestEPSSModelVersionRoundTrip(t *testing.T) {
	t.Parallel()
	version := "v4"
	date := "2026-05-16"
	payload, err := EncodeVulnerabilityEPSSScore(vulnerabilityv1.EPSSScore{
		CVEID: "CVE-2026-0001", ScoreDate: &date, ModelVersion: &version,
	})
	if err != nil {
		t.Fatal(err)
	}
	if payload["model_version"] != version {
		t.Fatalf("model_version = %#v, want %q", payload["model_version"], version)
	}
	decoded, err := DecodeVulnerabilityEPSSScore(Envelope{
		FactKind: FactKindVulnerabilityEPSSScore, SchemaVersion: "1.0.0", Payload: payload,
	})
	if err != nil {
		t.Fatal(err)
	}
	if decoded.ModelVersion == nil || *decoded.ModelVersion != version {
		t.Fatalf("decoded model version = %#v, want %q", decoded.ModelVersion, version)
	}
	delete(payload, "model_version")
	decoded, err = DecodeVulnerabilityEPSSScore(Envelope{
		FactKind: FactKindVulnerabilityEPSSScore, SchemaVersion: "1.0.0", Payload: payload,
	})
	if err != nil || decoded.ModelVersion != nil {
		t.Fatalf("legacy model version = %#v, error = %v; want nil", decoded.ModelVersion, err)
	}
}
