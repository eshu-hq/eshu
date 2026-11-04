// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/sdk/go/factschema/fixturepack"
	reducerderivedv1 "github.com/eshu-hq/eshu/sdk/go/factschema/reducerderived/v1"
)

// osPackageDerivationFields are the three finding-derivation evidence fields
// the reducer writes when an OS advisory joins through a source package
// (eshu-hq/eshu#9859): which key joined, the installed binary name, and the
// source name that matched.
var osPackageDerivationFields = []string{
	"os_package_match_derivation",
	"installed_package_name",
	"source_package",
}

// TestSupplyChainImpactFindingOSPackageDerivationRoundTrips pins the three
// evidence fields across Encode -> payload -> Decode. The query surface reads
// them back by key to explain that a finding joined through the source
// package, so a dropped or renamed JSON tag here would leave the reducer
// emitting corroboration no caller can read — inert rather than failing.
func TestSupplyChainImpactFindingOSPackageDerivationRoundTrips(t *testing.T) {
	derivation := "source_package"
	installed := "libssl3"
	source := "openssl"
	finding := reducerderivedv1.SupplyChainImpactFinding{
		FindingID:                "finding-1",
		CVEID:                    "CVE-2026-00010",
		OSPackageMatchDerivation: &derivation,
		InstalledPackageName:     &installed,
		SourcePackage:            &source,
	}
	env := reducerSupplyChainImpactFindingEnvelope(t, finding)
	for _, key := range osPackageDerivationFields {
		if _, ok := env.Payload[key]; !ok {
			t.Fatalf("encoded payload is missing %s: %v", key, env.Payload)
		}
	}
	decoded, err := DecodeReducerSupplyChainImpactFinding(env)
	if err != nil {
		t.Fatalf("DecodeReducerSupplyChainImpactFinding: %v", err)
	}
	if decoded.OSPackageMatchDerivation == nil || *decoded.OSPackageMatchDerivation != "source_package" {
		t.Fatalf("OSPackageMatchDerivation = %v, want source_package", decoded.OSPackageMatchDerivation)
	}
	if decoded.InstalledPackageName == nil || *decoded.InstalledPackageName != "libssl3" {
		t.Fatalf("InstalledPackageName = %v, want libssl3", decoded.InstalledPackageName)
	}
	if decoded.SourcePackage == nil || *decoded.SourcePackage != "openssl" {
		t.Fatalf("SourcePackage = %v, want openssl", decoded.SourcePackage)
	}
}

// TestSupplyChainImpactFindingOSPackageDerivationIsAdditiveOptional pins the
// compatibility half of the same fields: a payload written before #9859
// carries none of the three keys, and that MUST decode successfully to nil
// fields rather than a classified missing-required-field error. Nil fields
// must also stay off the wire, so an unaffected finding's persisted payload is
// byte-identical to what it was before these fields existed.
func TestSupplyChainImpactFindingOSPackageDerivationIsAdditiveOptional(t *testing.T) {
	env := reducerSupplyChainImpactFindingEnvelope(t, reducerderivedv1.SupplyChainImpactFinding{
		FindingID: "finding-2",
		CVEID:     "CVE-2026-00011",
	})
	for _, key := range osPackageDerivationFields {
		if value, ok := env.Payload[key]; ok {
			t.Fatalf("nil derivation field %q must be omitted from the payload, got %v", key, value)
		}
	}
	decoded, err := DecodeReducerSupplyChainImpactFinding(env)
	if err != nil {
		t.Fatalf("DecodeReducerSupplyChainImpactFinding on a pre-#9859 payload: %v", err)
	}
	if decoded.OSPackageMatchDerivation != nil {
		t.Fatalf("OSPackageMatchDerivation = %v, want nil", *decoded.OSPackageMatchDerivation)
	}
	if decoded.InstalledPackageName != nil {
		t.Fatalf("InstalledPackageName = %v, want nil", *decoded.InstalledPackageName)
	}
	if decoded.SourcePackage != nil {
		t.Fatalf("SourcePackage = %v, want nil", *decoded.SourcePackage)
	}
}

// TestSupplyChainImpactFindingOSPackageDerivationWrongTypeFailsClosed proves
// a derivation field of the wrong JSON type is a classified decode failure,
// not a silent drop that would leave a source-joined finding looking
// binary-joined.
func TestSupplyChainImpactFindingOSPackageDerivationWrongTypeFailsClosed(t *testing.T) {
	base := reducerderivedv1.SupplyChainImpactFinding{
		FindingID: "finding-3",
		CVEID:     "CVE-2026-00012",
	}
	for name, payload := range map[string]map[string]any{
		"derivation is a number":   {"os_package_match_derivation": float64(1)},
		"installed name is a bool": {"installed_package_name": true},
		"source is an array":       {"source_package": []any{"openssl"}},
	} {
		t.Run(name, func(t *testing.T) {
			for key, value := range payload {
				basePayload, err := EncodeReducerSupplyChainImpactFinding(withRequiredSupplyChainImpactCollections(base))
				if err != nil {
					t.Fatalf("EncodeReducerSupplyChainImpactFinding: %v", err)
				}
				basePayload[key] = value
				env := Envelope{
					FactKind:      FactKindReducerSupplyChainImpactFinding,
					SchemaVersion: "1.0.0",
					Payload:       basePayload,
				}
				if _, err := DecodeReducerSupplyChainImpactFinding(env); err == nil {
					t.Fatalf("DecodeReducerSupplyChainImpactFinding() error = nil for %s, want a decode failure", key)
				}
			}
		})
	}
}

// TestSupplyChainImpactFindingOSPackageDerivationSchemaShape asserts the
// committed JSON Schema artifact declares all three derivation fields as
// optional strings outside the required set, in both the canonical copy and
// the embedded fixture-pack copy.
func TestSupplyChainImpactFindingOSPackageDerivationSchemaShape(t *testing.T) {
	canonical, err := os.ReadFile("schema/reducer_supply_chain_impact_finding.v1.schema.json")
	if err != nil {
		t.Fatalf("read canonical schema: %v", err)
	}
	packed, ok := fixturepack.SchemaFor("reducer_supply_chain_impact_finding")
	if !ok {
		t.Fatal("fixturepack has no reducer_supply_chain_impact_finding schema")
	}
	for name, raw := range map[string][]byte{"canonical": canonical, "fixturepack": packed} {
		t.Run(name, func(t *testing.T) {
			var schema struct {
				Required   []string                   `json:"required"`
				Properties map[string]json.RawMessage `json:"properties"`
			}
			if err := json.Unmarshal(raw, &schema); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}
			required := map[string]bool{}
			for _, key := range schema.Required {
				required[key] = true
			}
			for _, key := range osPackageDerivationFields {
				field, present := schema.Properties[key]
				if !present {
					t.Fatalf("schema properties lack %s", key)
				}
				if required[key] {
					t.Fatalf("%s is required, want optional: %v", key, schema.Required)
				}
				var shape struct {
					Type []string `json:"type"`
				}
				if err := json.Unmarshal(field, &shape); err != nil {
					t.Fatalf("json.Unmarshal(%s) error = %v", key, err)
				}
				if !reflect.DeepEqual(shape.Type, []string{"string", "null"}) {
					t.Fatalf("%s schema type = %v, want [string null]", key, shape.Type)
				}
			}
		})
	}
}
