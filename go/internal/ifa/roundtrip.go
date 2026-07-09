// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/factenvelope"
	"github.com/eshu-hq/eshu/go/internal/replay"
	"github.com/eshu-hq/eshu/go/internal/synth/gcp"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
)

// gcpRoundTripFunc decodes a factschema envelope into its kind's typed
// struct and immediately re-encodes it back into a payload map, so callers
// need only compare the result against the original payload.
type gcpRoundTripFunc func(factschema.Envelope) (map[string]any, error)

// gcpRoundTripByKind is the fail-closed allow-list of fact kinds
// RoundTripTypedPayloads knows how to decode and re-encode. A kind absent
// here is a design gap in this Odù's coverage, not a silent pass: it is
// designed for the demoOrgRoundtripOdu family and is extended deliberately as
// Ifá grows more round-trip-proven fact kinds.
var gcpRoundTripByKind = map[string]gcpRoundTripFunc{
	factschema.FactKindGCPCloudResource: func(env factschema.Envelope) (map[string]any, error) {
		resource, err := factschema.DecodeGCPCloudResource(env)
		if err != nil {
			return nil, fmt.Errorf("decode gcp_cloud_resource: %w", err)
		}
		return factschema.EncodeGCPCloudResource(resource)
	},
	factschema.FactKindGCPCloudRelationship: func(env factschema.Envelope) (map[string]any, error) {
		relationship, err := factschema.DecodeGCPCloudRelationship(env)
		if err != nil {
			return nil, fmt.Errorf("decode gcp_cloud_relationship: %w", err)
		}
		return factschema.EncodeGCPCloudRelationship(relationship)
	},
	factschema.FactKindGCPCollectionWarning: func(env factschema.Envelope) (map[string]any, error) {
		warning, err := factschema.DecodeGCPCollectionWarning(env)
		if err != nil {
			return nil, fmt.Errorf("decode gcp_collection_warning: %w", err)
		}
		return factschema.EncodeGCPCollectionWarning(warning)
	},
	factschema.FactKindGCPDNSRecord: func(env factschema.Envelope) (map[string]any, error) {
		record, err := factschema.DecodeGCPDNSRecord(env)
		if err != nil {
			return nil, fmt.Errorf("decode gcp_dns_record: %w", err)
		}
		return factschema.EncodeGCPDNSRecord(record)
	},
	factschema.FactKindGCPIAMPolicyObservation: func(env factschema.Envelope) (map[string]any, error) {
		observation, err := factschema.DecodeGCPIAMPolicyObservation(env)
		if err != nil {
			return nil, fmt.Errorf("decode gcp_iam_policy_observation: %w", err)
		}
		return factschema.EncodeGCPIAMPolicyObservation(observation)
	},
}

// RoundTripTypedPayloads proves every fact in odu whose kind has a
// registered typed factschema Decode/Encode pair survives
// Encode->Decode->re-Encode with byte-identical canonical JSON: the
// "contract system alive" terminal proof (issue #4804) that the SDK's typed
// struct for a fact kind is not silently dropping, reshaping, or
// misclassifying a payload field the collector actually emitted.
//
// A fact whose kind has no registered round trip is a coverage gap in the
// caller's Odù, not a pass: RoundTripTypedPayloads returns an error naming
// the kind rather than skipping it.
//
// A payload that fails typed decode returns the classified
// *factschema.DecodeError unwrapped (via fmt.Errorf's %w), so a caller can
// errors.As into it to inspect Classification and Field exactly as a reducer
// handler would. A payload that decodes successfully but whose re-encoded
// canonical bytes differ from the original returns an error naming the
// fact's StableFactKey and FactKind and showing both canonical forms.
//
// The byte-equality check reuses replay.CanonicalizeValue directly with no
// extra number-type normalization step: the #4804 T0 probe proved
// encoding/json's own float64/int64 whole-number formatting already agrees
// for every representative int64-typed GCP field (gcp_collection_warning's
// hidden_count, gcp_dns_record's target_count/ttl_seconds) and for a
// gcp_cloud_resource payload carrying an Attributes remainder, so comparing
// each side's direct CanonicalizeValue output is sufficient — see
// roundtrip_test.go's TestRoundTripTypedPayloadsNumberBoundary.
func RoundTripTypedPayloads(odu Odu) error {
	for _, env := range odu.Facts {
		roundTrip, ok := gcpRoundTripByKind[env.FactKind]
		if !ok {
			return fmt.Errorf("ifa: round-trip odu %q: fact kind %q has no registered typed round-trip", odu.Name, env.FactKind)
		}

		fsEnv := factenvelope.FactSchemaFromInternal(env)
		reencoded, err := roundTrip(fsEnv)
		if err != nil {
			return fmt.Errorf("ifa: round-trip odu %q fact %q (kind %q): %w", odu.Name, env.StableFactKey, env.FactKind, err)
		}

		originalCanon, err := replay.CanonicalizeValue(env.Payload, replay.CanonicalOptions{})
		if err != nil {
			return fmt.Errorf("ifa: round-trip odu %q fact %q (kind %q): canonicalize original payload: %w", odu.Name, env.StableFactKey, env.FactKind, err)
		}
		reencodedCanon, err := replay.CanonicalizeValue(reencoded, replay.CanonicalOptions{})
		if err != nil {
			return fmt.Errorf("ifa: round-trip odu %q fact %q (kind %q): canonicalize re-encoded payload: %w", odu.Name, env.StableFactKey, env.FactKind, err)
		}
		if string(originalCanon) != string(reencodedCanon) {
			return fmt.Errorf(
				"ifa: round-trip odu %q fact %q (kind %q): payload changed after decode/re-encode:\n--- original ---\n%s\n--- re-encoded ---\n%s",
				odu.Name, env.StableFactKey, env.FactKind, originalCanon, reencodedCanon,
			)
		}
	}
	return nil
}

// demoOrgRoundtripOdu carries every fact envelope the demo-org GCP synthetic
// cassette generates (go/internal/synth/gcp), replayed through the production
// cassette.Source seam via gcp.DemoOrgFactEnvelopes rather than a hand-built
// mirror. It proves fact_kind:* payload-schema coverage for the full gcp_*
// family this milestone seeds (gcp_cloud_resource, gcp_cloud_relationship,
// gcp_collection_warning, gcp_dns_record, gcp_iam_policy_observation) AND
// RoundTripTypedPayloads' typed-both-sides round-trip proof for every one of
// them in a single Odù, exactly like odu:aws-pack does for the AWS family.
//
// Generation failure here means the synthetic generator itself is broken
// (not a runtime input condition Ifá needs to handle gracefully at catalog
// construction time), so this panics rather than returning an error,
// mirroring awsPackOdu's panic-on-gen-failure contract.
func demoOrgRoundtripOdu() CatalogOdu {
	envelopes, err := gcp.DemoOrgFactEnvelopes(gcp.DefaultDemoOrgProfile())
	if err != nil {
		panic(fmt.Sprintf("ifa: catalog_seed odu:demo-org-roundtrip: generate demo-org GCP envelopes: %v", err))
	}

	return CatalogOdu{
		Odu:    Odu{Name: "odu:demo-org-roundtrip", Facts: envelopes},
		Detail: "every fact the demo-org synthetic GCP cassette generates (gcp_cloud_resource/gcp_cloud_relationship/gcp_collection_warning/gcp_dns_record/gcp_iam_policy_observation), replayed through the production cassette.Source seam and proven byte-identical under Encode->Decode->re-Encode",
	}
}
