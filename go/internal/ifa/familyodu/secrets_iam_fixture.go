// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package familyodu

import (
	"encoding/json"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
	factsidentity "github.com/eshu-hq/eshu/go/internal/facts/identity"
)

// Shared fixture seams for the three secrets/IAM vault direct-materialization
// family Odùs (#6228 slice A): authenticates_vault_role, uses_vault_policy,
// and grants_secret_read.
//
// All three families share one production chain (collector facts ->
// trust-chain read models -> graph-projection rows), so their Odùs share one
// scope generation each but never share facts: every Odù below carries the
// collector facts its own family's edges need plus the negative controls
// that prove the chain resolves to nothing where it must.

// secretsIAMFixtureCollectorKind is the collector kind every vault-family
// fixture fact carries. It mirrors the value the reducer's own
// secretsIAMReducerFact helper stamps, so the Odù describes the same
// envelopes a live generation would carry rather than agreeing only on the
// payload. The extractor never reads it.
const secretsIAMFixtureCollectorKind = "secrets_iam_posture"

// secretsIAMFixtureSourceConfidence marks fixture facts as directly observed,
// the posture a scanner-emitted source fact carries.
const secretsIAMFixtureSourceConfidence = "observed"

// mustEncodeSecretsIAMFixturePayload encodes one typed factschema struct
// value into the payload map a fixture envelope carries (Contract System v1:
// facts are built as typed values and encoded, never hand-built as maps).
// It panics on an encode failure for the same reason IAMCanAssumeFamilyOdu
// does: a failure means the payload contract moved under a committed
// fixture, and every coverage claim built on it is already void.
func mustEncodeSecretsIAMFixturePayload(oduName string, value any) map[string]any {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("familyodu: catalog_seed %s: marshal fixture payload: %v", oduName, err))
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		panic(fmt.Sprintf("familyodu: catalog_seed %s: unmarshal fixture payload: %v", oduName, err))
	}
	return payload
}

// newSecretsIAMFixtureEnvelope builds one fixture envelope for a vault-family
// Odù. The schema version is resolved from the kind's registered contract,
// so a contract bump fails loudly here instead of shipping a fixture that
// decodes against a stale schema.
//
// The envelope carries no FactID on purpose, like every sibling Odù:
// cassette facts carry no fact_id (replay derives it from scope, generation,
// and stable key), and the cassette lockstep compares the compiled Odù
// against the cassette projection field-for-field, so a compiled FactID the
// cassette can never carry would fail the lockstep on every family.
func newSecretsIAMFixtureEnvelope(
	oduName string,
	factKind string,
	scopeID string,
	generationID string,
	identity string,
	payload map[string]any,
) facts.Envelope {
	version, ok := factsidentity.SecretsIAMSchemaVersion(factKind)
	if !ok {
		panic(fmt.Sprintf(
			"familyodu: catalog_seed %s: no registered schema version for fact kind %q",
			oduName, factKind,
		))
	}
	return facts.Envelope{
		ScopeID:          scopeID,
		GenerationID:     generationID,
		FactKind:         factKind,
		StableFactKey:    fmt.Sprintf("secretsiam:%s:%s", factKind, identity),
		SchemaVersion:    version,
		CollectorKind:    secretsIAMFixtureCollectorKind,
		SourceConfidence: secretsIAMFixtureSourceConfidence,
		Payload:          payload,
	}
}
