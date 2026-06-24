// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloudinventory

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

const (
	// ProviderAWS is the normalized provider token for AWS inventory facts whose
	// raw identity is an ARN or provider-native id.
	ProviderAWS = "aws"
	// ProviderGCP is the normalized provider token for GCP inventory facts whose
	// raw identity is a Cloud Asset Inventory full resource name.
	ProviderGCP = "gcp"
	// ProviderAzure is the normalized provider token for Azure inventory facts
	// whose raw identity is an ARM resource id.
	ProviderAzure = "azure"
)

// cloudResourceUIDPrefix namespaces every canonical cloud resource uid so the
// shared keyspace cannot collide with other reducer identity keyspaces.
const cloudResourceUIDPrefix = "cloud_resource:"

// ResolutionOutcome classifies how one provider raw identity resolved into the
// shared canonical keyspace. Only ResolutionOutcomeAdmitted carries a uid; every
// other outcome is counted and surfaced, never fabricated into canonical truth.
type ResolutionOutcome string

const (
	// ResolutionOutcomeAdmitted means the raw identity resolved into a stable
	// canonical cloud_resource_uid.
	ResolutionOutcomeAdmitted ResolutionOutcome = "admitted"
	// ResolutionOutcomeUnresolved means the raw identity was blank or otherwise
	// carried no keyable content for a supported provider.
	ResolutionOutcomeUnresolved ResolutionOutcome = "unresolved"
	// ResolutionOutcomeAmbiguous means the raw identity was present but did not
	// match the provider's expected identity shape closely enough to key safely.
	ResolutionOutcomeAmbiguous ResolutionOutcome = "ambiguous"
	// ResolutionOutcomeUnsupported means the provider is outside the shared
	// admission contract and has no canonical keyspace yet.
	ResolutionOutcomeUnsupported ResolutionOutcome = "unsupported"
)

// Resolution is the outcome of resolving one provider raw identity. A uid is
// present only when Outcome is ResolutionOutcomeAdmitted.
type Resolution struct {
	// Provider is the normalized provider token the resolver classified.
	Provider string
	// CloudResourceUID is the canonical shared identity, empty unless admitted.
	CloudResourceUID string
	// Outcome classifies the resolution for telemetry and read-model evidence.
	Outcome ResolutionOutcome
}

// NormalizeProvider lower-cases and trims a provider token so callers compare
// against the Provider* constants without per-collector casing drift.
func NormalizeProvider(provider string) string {
	return strings.ToLower(strings.TrimSpace(provider))
}

// ResolveProviderIdentity maps one provider raw identity into the shared
// canonical cloud_resource_uid keyspace. Supported providers are AWS (ARN), GCP
// (Cloud Asset Inventory full resource name), and Azure (ARM resource id). The
// uid is a namespaced SHA-256 over the normalized provider and raw identity, so
// it is deterministic and stable across retries and concurrent workers. Blank,
// malformed, or unsupported identities return the matching non-admitted outcome
// with an empty uid instead of fabricating canonical truth.
func ResolveProviderIdentity(provider, raw string) Resolution {
	normalizedProvider := NormalizeProvider(provider)
	normalizedRaw := strings.TrimSpace(raw)

	switch normalizedProvider {
	case ProviderAWS, ProviderGCP, ProviderAzure:
	default:
		return Resolution{Provider: normalizedProvider, Outcome: ResolutionOutcomeUnsupported}
	}

	if normalizedRaw == "" {
		return Resolution{Provider: normalizedProvider, Outcome: ResolutionOutcomeUnresolved}
	}

	if !providerIdentityIsKeyable(normalizedProvider, normalizedRaw) {
		return Resolution{Provider: normalizedProvider, Outcome: ResolutionOutcomeAmbiguous}
	}

	return Resolution{
		Provider:         normalizedProvider,
		CloudResourceUID: cloudResourceUID(normalizedProvider, normalizedRaw),
		Outcome:          ResolutionOutcomeAdmitted,
	}
}

// providerIdentityIsKeyable reports whether the raw identity matches the
// provider's expected identity prefix shape closely enough to key without
// guessing. It is intentionally conservative: a near-miss is ambiguous, not
// admitted, so the reducer counts it rather than inventing a uid.
func providerIdentityIsKeyable(provider, raw string) bool {
	switch provider {
	case ProviderAWS:
		// AWS ARNs start with arn:; provider-native ids are admitted as-is when
		// no ARN prefix is expected, but a bare colon-free token is too weak to
		// distinguish from noise, so require either an ARN or a non-trivial token.
		return strings.HasPrefix(raw, "arn:") || len(raw) >= 3
	case ProviderGCP:
		// Cloud Asset Inventory full resource names begin with //service.host/.
		return strings.HasPrefix(raw, "//")
	case ProviderAzure:
		// ARM resource ids are rooted at /subscriptions/ (case-insensitive).
		return strings.HasPrefix(strings.ToLower(raw), "/subscriptions/")
	default:
		return false
	}
}

// cloudResourceUID builds the namespaced canonical uid for one normalized
// provider and raw identity. AWS and GCP identities are case-significant and are
// hashed verbatim; Azure ARM ids are case-insensitive per Azure and are
// lower-cased before hashing so two casings of the same resource converge.
func cloudResourceUID(provider, raw string) string {
	keyed := raw
	if provider == ProviderAzure {
		keyed = strings.ToLower(raw)
	}
	digest := sha256.Sum256([]byte(provider + "\x00" + keyed))
	return cloudResourceUIDPrefix + hex.EncodeToString(digest[:])
}
