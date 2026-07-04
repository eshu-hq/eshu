// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// orgPolicyAssetType is the Cloud Asset Inventory asset type for a GCP
// Organization Policy (constraint binding). It is declared here so the
// extractor registration stays self-contained in this file.
const orgPolicyAssetType = "orgpolicy.googleapis.com/Policy"

// assetTypeCloudResourceManagerOrganization and
// assetTypeCloudResourceManagerFolder key the resource-hierarchy nodes an
// Organization Policy can target. assetTypeCloudResourceManagerProject is
// declared in extractor_firebase_project.go and reused here, never
// redeclared.
const (
	assetTypeCloudResourceManagerOrganization = "cloudresourcemanager.googleapis.com/Organization"
	assetTypeCloudResourceManagerFolder       = "cloudresourcemanager.googleapis.com/Folder"
)

// cloudResourceManagerResourceNamePrefixes maps the three CAI parent-kind path
// segments an Organization Policy full resource name can carry
// (`organizations/`, `folders/`, `projects/`) to the CAI full-resource-name
// prefix and asset type of the resource-hierarchy node they identify. Both the
// Organization Policy's own full resource name and the target node names share
// this same three-way parent-kind vocabulary, per the Cloud Asset Inventory
// resource-name-format reference.
var cloudResourceManagerResourceNamePrefixes = map[string]struct {
	prefix    string
	assetType string
}{
	"organizations": {prefix: "//cloudresourcemanager.googleapis.com/organizations/", assetType: assetTypeCloudResourceManagerOrganization},
	"folders":       {prefix: "//cloudresourcemanager.googleapis.com/folders/", assetType: assetTypeCloudResourceManagerFolder},
	"projects":      {prefix: cloudProjectResourceNamePrefix, assetType: assetTypeCloudResourceManagerProject},
}

// relationshipTypeOrgPolicyAppliesToResource is the bounded provider
// relationship type for the edge from an Organization Policy to the
// organization, folder, or project resource it is bound to.
const relationshipTypeOrgPolicyAppliesToResource = "org_policy_applies_to_resource"

// orgPolicyFullResourcePoliciesMarker separates an Organization Policy's
// resource-hierarchy parent segment from its trailing `/policies/<constraint>`
// suffix in a CAI full resource name.
const orgPolicyFullResourcePoliciesMarker = "/policies/"

// orgPolicyFullResourceNamePrefix is the exact CAI service prefix every
// Organization Policy full resource name must carry. The target-derivation
// fails closed unless the name starts with this prefix, so a relative or
// wrong-service asset name never mints a fabricated resource-hierarchy edge.
const orgPolicyFullResourceNamePrefix = "//orgpolicy.googleapis.com/"

func init() {
	RegisterAssetExtractor(orgPolicyAssetType, extractOrgPolicy)
}

// orgPolicyData is the bounded view of a CAI orgpolicy.googleapis.com/Policy
// resource.data blob. Only redaction-safe control-plane posture is surfaced to
// facts: rule counts, enforce/allow-all/deny-all/condition presence, and
// inheritance/reset booleans. The rule union's allowed/denied VALUE lists are
// unmarshaled transiently so their entries can be counted, but the entries
// themselves are decoded only to compute those bounded counts and are never
// persisted or surfaced to any fact — they can carry organization- or
// project-specific identifiers (org IDs, resource names, folder IDs); only the
// bounded counts leave the parser, mirroring the Custom IAM Role extractor's
// treatment of its permission list. The opaque spec etag is reduced to a stable
// fingerprint and never persisted raw, per the IAM Role extractor's etag
// treatment.
type orgPolicyData struct {
	Spec       *orgPolicySpec `json:"spec"`
	DryRunSpec *orgPolicySpec `json:"dryRunSpec"`
}

// orgPolicySpec is the bounded view of the Org Policy v2 PolicySpec object
// (etag, updateTime, rules[], inheritFromParent, reset), per the live
// orgpolicy.googleapis.com v2 organizations.policies REST reference.
type orgPolicySpec struct {
	Etag              string          `json:"etag"`
	UpdateTime        string          `json:"updateTime"`
	Rules             []orgPolicyRule `json:"rules"`
	InheritFromParent *bool           `json:"inheritFromParent"`
	Reset             *bool           `json:"reset"`
}

// orgPolicyRule is the bounded view of one PolicySpec.rules[] entry. The rule
// is a union of values/allowAll/denyAll/enforce plus an optional condition.
// The values list contents and the condition expression text are unmarshaled
// only to compute bounded presence/counts (a CEL expression can embed resource
// identifiers, so its text is never surfaced); only presence and the enforce
// value reach a fact.
type orgPolicyRule struct {
	Values *struct {
		AllowedValues []string `json:"allowedValues"`
		DeniedValues  []string `json:"deniedValues"`
	} `json:"values"`
	AllowAll  *bool `json:"allowAll"`
	DenyAll   *bool `json:"denyAll"`
	Enforce   *bool `json:"enforce"`
	Condition *struct {
		Expression string `json:"expression"`
	} `json:"condition"`
}

// extractOrgPolicy extracts bounded, redaction-safe typed depth for one CAI
// Organization Policy asset. It surfaces the constraint name (derived from the
// policy's own full resource name, never trusted from the resource.data body),
// a bounded rule-shape summary (total rule count plus per-kind counts for
// allow-values/deny-values/allow-all/deny-all/condition-present rules and a
// count of rules that enforce), the spec's inheritFromParent/reset booleans, a
// fingerprinted spec etag, the spec updateTime, and dry-run-spec presence with
// its own bounded rule count; emits the org_policy_applies_to_resource edge to
// the organization, folder, or project the policy is bound to.
//
// The rule union's actual allowed/denied VALUE lists, the condition
// expression text, and any custom-constraint parameters are never decoded —
// those can carry organization-specific resource identifiers, project ids, or
// other values that must not cross the redaction boundary; only bounded
// counts and booleans leave this extractor.
func extractOrgPolicy(ctx ExtractContext) (AttributeExtraction, error) {
	var data orgPolicyData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode org policy data: %w", err)
	}

	attrs := map[string]any{}
	var anchors []string
	var rels []RelationshipObservation

	if constraint, target, targetAssetType, ok := orgPolicyTargetFromFullResourceName(ctx.FullResourceName); ok {
		attrs["constraint_name"] = constraint
		anchors = append(anchors, target)
		rels = append(rels, RelationshipObservation{
			SourceFullResourceName: ctx.FullResourceName,
			SourceAssetType:        ctx.AssetType,
			RelationshipType:       relationshipTypeOrgPolicyAppliesToResource,
			TargetFullResourceName: target,
			TargetAssetType:        targetAssetType,
			SupportState:           RelationshipSupportSupported,
		})
	}

	if data.Spec != nil {
		applyOrgPolicySpecAttributes(attrs, data.Spec, "")
	}
	if data.DryRunSpec != nil {
		attrs["has_dry_run_spec"] = true
		applyOrgPolicySpecAttributes(attrs, data.DryRunSpec, "dry_run_")
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: anchors,
		Relationships:      rels,
	}, nil
}

// applyOrgPolicySpecAttributes writes the bounded rule-shape summary for one
// PolicySpec into attrs, prefixing each key with keyPrefix so the live spec
// ("") and the dry-run spec ("dry_run_") never collide in the same attribute
// map.
func applyOrgPolicySpecAttributes(attrs map[string]any, spec *orgPolicySpec, keyPrefix string) {
	if n := len(spec.Rules); n > 0 {
		attrs[keyPrefix+"rule_count"] = n
		allowValues, denyValues, allowAll, denyAll, condition, enforce := summarizeOrgPolicyRules(spec.Rules)
		if allowValues > 0 {
			attrs[keyPrefix+"allow_values_rule_count"] = allowValues
		}
		if denyValues > 0 {
			attrs[keyPrefix+"deny_values_rule_count"] = denyValues
		}
		if allowAll > 0 {
			attrs[keyPrefix+"allow_all_rule_count"] = allowAll
		}
		if denyAll > 0 {
			attrs[keyPrefix+"deny_all_rule_count"] = denyAll
		}
		if condition > 0 {
			attrs[keyPrefix+"condition_rule_count"] = condition
		}
		if enforce > 0 {
			attrs[keyPrefix+"enforce_count"] = enforce
		}
	}
	if keyPrefix != "" {
		// The dry-run spec's own etag/updateTime/inheritance posture are not
		// surfaced as separate attributes: the live spec already carries the
		// operationally relevant posture, and doubling every field would grow
		// typed depth for a rarely-populated preview spec without adding
		// Terraform/drift/monitoring value. Only its rule-shape counts (above)
		// are worth the typed depth, since they show what the dry run would
		// enforce next.
		return
	}
	if v := strings.TrimSpace(spec.UpdateTime); v != "" {
		if normalized, ok := normalizeRFC3339(v); ok {
			attrs["update_time"] = normalized
		}
	}
	if spec.InheritFromParent != nil {
		attrs["inherit_from_parent"] = *spec.InheritFromParent
	}
	if spec.Reset != nil {
		attrs["reset"] = *spec.Reset
	}
	if fp := orgPolicyEtagFingerprint(spec.Etag); fp != "" {
		attrs["etag_fingerprint"] = fp
	}
}

// summarizeOrgPolicyRules reduces a PolicySpec's rules[] to bounded per-kind
// counts. A rule is a union type per the Org Policy v2 schema (exactly one of
// values/allowAll/denyAll/enforce is set), but the counts are tallied
// independently rather than assuming mutual exclusivity so a future API
// addition to the union never silently double-counts or drops a rule.
func summarizeOrgPolicyRules(rules []orgPolicyRule) (allowValues, denyValues, allowAll, denyAll, condition, enforce int) {
	for _, rule := range rules {
		if rule.Values != nil {
			if len(rule.Values.AllowedValues) > 0 {
				allowValues++
			}
			if len(rule.Values.DeniedValues) > 0 {
				denyValues++
			}
		}
		if rule.AllowAll != nil && *rule.AllowAll {
			allowAll++
		}
		if rule.DenyAll != nil && *rule.DenyAll {
			denyAll++
		}
		if rule.Condition != nil {
			condition++
		}
		if rule.Enforce != nil && *rule.Enforce {
			enforce++
		}
	}
	return allowValues, denyValues, allowAll, denyAll, condition, enforce
}

// orgPolicyTargetFromFullResourceName derives the constraint name and the
// bound organization/folder/project target from the Organization Policy's own
// CAI full resource name — never from the resource.data body, which is
// untrusted parser input. The CAI full resource name for this asset type is
// `//orgpolicy.googleapis.com/{organizations|folders|projects}/<id>/policies/<constraint>`
// per the Cloud Asset Inventory resource-name-format reference. It fails closed
// — returns ok=false — unless the name carries the exact
// `//orgpolicy.googleapis.com/` service prefix, a `/policies/<constraint>`
// suffix, and a parent path of exactly `<kind>/<id>` for a recognized
// parent-kind. ctx.FullResourceName is the raw, untrusted CAI asset name, so a
// relative name like `organizations/123/policies/x`, a wrong-service name, or a
// name with extra path segments must not mint a fabricated resource-hierarchy
// edge or anchor.
func orgPolicyTargetFromFullResourceName(fullName string) (constraint, target, targetAssetType string, ok bool) {
	trimmed := strings.TrimSpace(fullName)
	// Fail closed on a missing service prefix: TrimPrefix would silently no-op
	// and accept a relative or wrong-service name.
	if !strings.HasPrefix(trimmed, orgPolicyFullResourceNamePrefix) {
		return "", "", "", false
	}

	index := strings.LastIndex(trimmed, orgPolicyFullResourcePoliciesMarker)
	if index < 0 || index+len(orgPolicyFullResourcePoliciesMarker) >= len(trimmed) {
		return "", "", "", false
	}
	constraint = trimmed[index+len(orgPolicyFullResourcePoliciesMarker):]
	if constraint == "" {
		return "", "", "", false
	}

	parentPath := strings.TrimPrefix(trimmed[:index], orgPolicyFullResourceNamePrefix)
	// The parent path must be exactly `<kind>/<id>` — no missing id and no
	// trailing segments — so a malformed name never resolves to a real-looking
	// hierarchy node.
	kind, id, cutOK := strings.Cut(parentPath, "/")
	if !cutOK || id == "" || strings.Contains(id, "/") {
		return "", "", "", false
	}
	entry, known := cloudResourceManagerResourceNamePrefixes[kind]
	if !known {
		return "", "", "", false
	}
	return constraint, entry.prefix + id, entry.assetType, true
}

// orgPolicyEtagFingerprint reduces the opaque spec etag to a stable,
// redaction-safe digest. The etag is a concurrency token, not sensitive, but
// it is opaque and carries no operator value verbatim; fingerprinting it keeps
// drift-detectable change signal without persisting the raw token, mirroring
// the Custom IAM Role extractor's etag treatment. It returns "" for a blank
// etag so the caller omits the attribute.
func orgPolicyEtagFingerprint(etag string) string {
	trimmed := strings.TrimSpace(etag)
	if trimmed == "" {
		return ""
	}
	return "sha256:" + facts.StableID("GCPCloudOrgPolicyEtag", map[string]any{
		"etag": trimmed,
	})
}
