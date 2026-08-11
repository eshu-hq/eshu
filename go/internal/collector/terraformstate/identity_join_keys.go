// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package terraformstate

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/redact"
)

// identityJoinKeys are the scalar attribute names a downstream reader JOINS on.
// They are exempt from SchemaUnknown fail-closure (#5870).
//
// The criterion is "a key some consumer joins on", not "a key that looks like
// an identifier" and not "a key that is not sensitive". It was enumerated by
// following the readers rather than by naming intuition:
//
//   - `arn` — go/internal/storage/postgres/aws_cloud_runtime_drift_evidence_sql.go
//     inner-joins state rows on payload->'attributes'->>'arn'.
//   - `id`, `self_link` — multi_cloud_runtime_drift_evidence_sql.go derives
//     native_identity as COALESCE(arn, id, self_link), so an uncovered GCP
//     provider loses `self_link` exactly the way AWS loses `arn`.
//
// `self_link` is deliberately here while it is NOT in correlationAnchorFields,
// which is why that list is not the criterion.
//
// Why exempting these is not a hole in fail-closed. Redacting a join key does
// not protect a value; it corrupts graph truth. A `->>` over the redaction
// marker OBJECT returns non-null JSON text, so a redacted `arn` does not fall
// through to `id` — it WINS the COALESCE with garbage, the equijoin misses, the
// state row is dropped at the database, and cloudruntime.Classify reports
// orphaned_cloud_resource for a resource Terraform demonstrably manages. The
// operator is told "nothing manages this", which is a wrong answer, not a
// withheld one.
//
// What this costs, stated plainly rather than minimized: an ARN embeds the
// 12-digit AWS account id and a GCP self_link embeds the project id, so those
// can now appear raw from a provider the bundle cannot classify. On the ordinary
// SchemaKnown path these same three keys — and standalone `account_id` — are
// already preserved raw (see defaultRedactionSensitiveKeys in
// cmd/collector-terraform-state/config.go, which lists only credential-shaped
// keys). The genuinely new exposure is a state-only deployment with no cloud
// collector, where nothing else would emit that ARN.
//
// The carve-out is by key NAME, not by content. A provider that stores
// something sensitive under an attribute literally named `id` would emit it
// raw. That is the real edge of this trade, and it is why the set is a fixed
// three rather than a heuristic: an operator can still redact any of them
// explicitly through ESHU_TFSTATE_REDACTION_SENSITIVE_KEYS, which outranks this
// exemption (redact.RuleSet.Classify tests sensitive keys before schema trust).
var identityJoinKeys = map[string]struct{}{
	"arn":       {},
	"id":        {},
	"self_link": {},
}

// classificationSchemaTrust is the schema-trust seam used when CLASSIFYING an
// attribute for emission. It is schemaTrust plus the identity-join-key
// exemption above.
//
// It is deliberately a separate function rather than a change to schemaTrust,
// because schemaTrust has a second caller with a different contract:
// redactsAnchor decides whether to publish a hashed correlation anchor, and
// that guarantee is not part of this exemption. An uncovered provider still
// publishes no anchors; only the attribute the loader actually reads is
// rescued.
//
// Order matters and is asserted by test:
//
//  1. A resolver that already answers SchemaKnown needs nothing from here.
//  2. Composites are never exempt. redact cannot safely serialize a nested
//     structure under an unknown schema, and no join reads one.
//  3. isHardSensitiveStateAttribute wins. No hard-sensitive entry currently
//     names one of the three keys — TestIdentityJoinKeysAreNotHardSensitive
//     pins that — so this is defense in depth for a future entry that does,
//     rather than a live branch.
//  4. Only then is a join key upgraded.
//
// Operator-declared sensitive keys are not checked here because they do not
// need to be: redact.RuleSet.Classify tests them BEFORE it consults schema
// trust, so an operator naming `arn` still gets a marker whatever this returns.
func (p *stateParser) classificationSchemaTrust(
	resourceType string,
	attributeKey string,
	scalar bool,
) redact.SchemaTrust {
	trust := p.schemaTrust(resourceType, attributeKey)
	if trust == redact.SchemaKnown {
		return trust
	}
	p.recordUncoveredResourceType(resourceType)
	if !scalar {
		return trust
	}
	if isHardSensitiveStateAttribute(resourceType, attributeKey) {
		return trust
	}
	if _, ok := identityJoinKeys[strings.TrimSpace(attributeKey)]; !ok {
		return trust
	}
	return redact.SchemaKnown
}

// providerFromResourceType returns the Terraform provider prefix of a resource
// type ("aws" for "aws_s3_bucket"). Terraform's own naming contract is
// <provider>_<resource>, so the prefix is the provider name rather than a
// guess. Returns "" for a blank or prefix-less type, which the caller treats as
// "not reportable" rather than inventing a provider label.
func providerFromResourceType(resourceType string) string {
	resourceType = strings.TrimSpace(resourceType)
	provider, _, found := strings.Cut(resourceType, "_")
	if !found || provider == "" {
		return ""
	}
	return provider
}
