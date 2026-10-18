// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

import (
	"net/url"
)

// Closed value-kind values for ConfigKeyDivergence.ValueKind: the shape
// class of the diverged value, recorded so a reader can tell a URL-shaped
// divergence from a scalar one without ever seeing the value itself.
const (
	// ValueKindURL marks a diverged value shaped like a URL. The emitter
	// MUST classify from the userinfo-stripped form (StripURLUserinfo):
	// credentials in the URL must never influence the classification or
	// leak into the emitted fact.
	ValueKindURL = "url"
	// ValueKindString marks a diverged plain-string value.
	ValueKindString = "string"
	// ValueKindNumber marks a diverged numeric value.
	ValueKindNumber = "number"
	// ValueKindBoolean marks a diverged boolean value.
	ValueKindBoolean = "boolean"
	// ValueKindList marks a diverged list value.
	ValueKindList = "list"
	// ValueKindMap marks a diverged map value.
	ValueKindMap = "map"
	// ValueKindNull marks a diverged-into-absent value (the key exists on
	// one side and is unset on the other).
	ValueKindNull = "null"
)

// StripURLUserinfo returns raw with any URL userinfo removed, preserving
// scheme, host, port, path, query, and fragment. Emitters MUST pass every
// URL-shaped value through this normalization before comparing configs or
// classifying ValueKindURL: credentials embedded as userinfo must neither
// leak into an emitted fact nor flip a divergence comparison (two URLs that
// differ only by credentials are the same endpoint, not a divergence).
//
// Input that does not parse as a URL, or parses with no userinfo, passes
// through unchanged: only the caller knows which values are URL-shaped, so
// this function normalizes rather than validates.
func StripURLUserinfo(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User == nil {
		return raw
	}
	parsed.User = nil
	return parsed.String()
}

// ConfigKeyDivergence is the schema-version-1 typed payload for the
// "config_key_divergence" fact kind (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md, issue #5393).
//
// One ConfigKeyDivergence fact is one config key's divergence state for one
// environment on one tracked ref: whether the key's effective value there
// differs from the default branch. The fact carries keys plus a per-env
// divergence boolean only — no values, no hashes — so a divergence answer
// can never leak a secret, and TestConfigKeyDivergenceCarriesNoValuesOrHashes
// pins that bound structurally. A reader that needs the differing values
// names the ref and reads the content overlay through the ref-scoped content
// surface (a sibling Phase 1 leaf); this fact only says WHERE to look.
//
// RepoID, Ref, ConfigKey, Env, and Diverged are required: a divergence row
// with no repo, no ref, no key, no environment, or no verdict is not
// attributable to any comparison. ValueKind is optional: the shape class of
// the diverged value (one of the ValueKind* constants), recorded without the
// value itself; nil when the emitter does not classify. CollectorInstanceID
// stays optional: it identifies the collector run, not the divergence claim.
//
// Emitters MUST NOT emit a row for a sensitive key name at all: a key the
// collector's sensitive-key rule (sdk/go/collector IsSensitiveKeyName)
// rejects carries no divergence row, so even the key's existence on a
// non-default ref stays out of the fact stream.
type ConfigKeyDivergence struct {
	// CollectorInstanceID identifies the collector instance run that emitted
	// this fact. Optional: it is operational metadata about the run, not part
	// of the divergence claim's identity.
	CollectorInstanceID *string `json:"collector_instance_id,omitempty"`

	// RepoID is the canonical repo_id of the repository the tracked ref
	// belongs to. Required: without it the fact cannot be attributed to any
	// repository.
	RepoID string `json:"repo_id"`

	// Ref is the tracked ref as the deployment names it: a branch name, a
	// tag name, or a commit SHA. Required: it is the overlay identity
	// (repo_id@ref) half that distinguishes this row from the default
	// branch.
	Ref string `json:"ref"`

	// ConfigKey is the dotted config key whose effective value was compared
	// (for example "service.api_endpoint"). Required: the row's subject.
	// Never a sensitive key name: emitters suppress those before emission.
	ConfigKey string `json:"config_key"`

	// Env is the environment the comparison was made for (for example
	// "prod"). Required: divergence is per environment, and one row carries
	// one environment's verdict.
	Env string `json:"env"`

	// Diverged is whether the key's effective value for Env on Ref differs
	// from the default branch. Required: the row's verdict. Rows with false
	// are valid observations (an explicitly confirmed non-divergence), not
	// absence markers.
	Diverged bool `json:"diverged"`

	// ValueKind is the shape class of the diverged value (one of the
	// ValueKind* constants), recorded without the value itself. Optional:
	// nil when the emitter does not classify. A URL-shaped classification
	// MUST be computed from the userinfo-stripped form (StripURLUserinfo).
	ValueKind *string `json:"value_kind,omitempty"`
}
