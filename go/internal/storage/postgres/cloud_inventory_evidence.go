// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/correlation/cloudinventory"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// maxCloudInventoryAttributeKeys caps how many top-level keys any cloud
// inventory attributes map (raw-passthrough or allowlisted) may carry. It is
// defense in depth against a malformed or unexpectedly large provider payload;
// every allowlist defined in this file is already far smaller than the cap.
const maxCloudInventoryAttributeKeys = 64

// PostgresCloudInventoryEvidenceLoader reads the provider cloud-inventory source
// facts for one scope generation and maps each provider payload into the shared
// reducer.CloudInventoryRecord shape the admission path consumes. It is the
// concrete implementation of reducer.CloudInventoryEvidenceLoader for the
// multi-cloud admission domain (issues #1997, #1998).
//
// The loader is read-only and side-effect free: it does not resolve canonical
// identity, fold records, or write anything. Identity resolution and evidence
// folding belong to the admission handler so a stale generation that this loader
// happens to read can still be superseded before any canonical write.
type PostgresCloudInventoryEvidenceLoader struct {
	// DB executes the bounded source-fact read.
	DB Queryer
	// Logger, when set, records bounded skip diagnostics for rows the loader
	// could not decode. Nil disables loader logging.
	Logger *slog.Logger
}

// cloudInventorySourceFactMapping describes how one provider inventory source
// fact kind maps onto the shared admission record: which provider token it
// carries and which payload key holds the provider resource type.
type cloudInventorySourceFactMapping struct {
	provider        string
	resourceTypeKey string
	// surfacesAttributes gates whether the loader carries the RAW payload
	// attributes map onto the admission record, unfiltered beyond
	// boundedCloudInventoryAttributes' type/cap bounding. Only GCP typed-depth
	// facts produce a bounded, redaction-safe attributes map vetted end to end
	// for the cloud inventory readback, so this stays true for GCP only.
	// surfacesAttributes and attributeAllowlist are mutually exclusive: a
	// mapping uses the raw-passthrough path (GCP) or the closed-allowlist path
	// (AWS, Azure), never both.
	surfacesAttributes bool
	// attributeAllowlist, when set, gates the loader to a bounded, CLOSED
	// per-provider allowlist instead of the raw passthrough. AWS and Azure
	// resource facts carry an attributes map of raw provider fields (e.g. uri,
	// cluster_arn, arm_resource_id) that the route contract must never surface;
	// the allowlist keeps only explicitly named image/version evidence (issue
	// #5449) and drops everything else, including any future unreviewed key the
	// provider payload starts emitting.
	attributeAllowlist *cloudInventoryAttributeAllowlist
}

// awsCloudInventoryAttributeAllowlist is the closed image/version allowlist
// for aws_resource attributes (issue #5449). It surfaces the strongest
// deployed-code signals the AWS collector already observes -- an ECS task's
// running container image and digest plus its owning task definition, and a
// Lambda function's container image URI/digest and code version -- while
// dropping every other AWS attribute key (cluster_arn, role_arn, kms_key_arn,
// network_interfaces, environment, vpc_config, and any key not named here).
var awsCloudInventoryAttributeAllowlist = cloudInventoryAttributeAllowlist{
	scalarKeys: map[string]struct{}{
		"task_definition_arn": {},
		"image_uri":           {},
		"resolved_image_uri":  {},
		"code_sha256":         {},
		"version":             {},
	},
	nestedArrayKeys: map[string]map[string]struct{}{
		// containers' sub-key set is intentionally maintained independently of
		// cloudInventoryContainerAttributeKeys in
		// go/internal/query/cloud_inventory_read_model.go, which applies the same
		// {image, image_digest} sub-key set as a second, independent gate on the
		// already-filtered value. There is no shared constant and no test tying
		// the two together across packages: this loader is the sole upstream of
		// that projector's input (every containers value it ever sees already
		// passed through this filter), so drift between the two sets can only
		// ever make the read model MORE restrictive than this loader -- i.e.
		// silently drop container data the loader already allowed -- never leak
		// a raw sub-key this loader dropped. The query package pins its own set
		// with TestCloudInventoryContainerAttributeKeysIsImageAndDigestOnly so a
		// change there is caught by a test even without a cross-package tie.
		"containers": {
			"image":        {},
			"image_digest": {},
		},
	},
}

// azureCloudInventoryAttributeAllowlist is the closed image/version allowlist
// for azure_cloud_resource attributes (issue #5449). It is intentionally EMPTY
// today: the azure_cloud_resource fact's attributes map carries only identity
// and boundary fields (arm_resource_id, subscription_id, resource_group,
// tenant_id, tags, the redacted "extension" object, ...), never an image or
// version field -- Azure's runtime image evidence is emitted as a separate
// azure_image_reference fact kind that this admission mapping does not
// consume. The allowlist mechanism is wired now so Azure image/version keys
// can be added the moment a source fact carries them, without touching the
// filter path again; see TestCloudInventoryRecordFromRowAzureAttributesAlwaysDropped
// for the regression guard that every current Azure key stays dropped.
var azureCloudInventoryAttributeAllowlist = cloudInventoryAttributeAllowlist{}

// cloudInventorySourceFactMappings is the closed set of provider inventory
// source fact kinds the shared admission path consumes. Adding a provider means
// adding its source fact kind both here and in the SQL allowlist so the two stay
// in lockstep.
var cloudInventorySourceFactMappings = map[string]cloudInventorySourceFactMapping{
	facts.AWSResourceFactKind: {
		provider:           cloudinventory.ProviderAWS,
		resourceTypeKey:    "resource_type",
		attributeAllowlist: &awsCloudInventoryAttributeAllowlist,
	},
	facts.GCPCloudResourceFactKind: {
		provider:           cloudinventory.ProviderGCP,
		resourceTypeKey:    "asset_type",
		surfacesAttributes: true,
	},
	facts.AzureCloudResourceFactKind: {
		provider:           cloudinventory.ProviderAzure,
		resourceTypeKey:    "resource_type",
		attributeAllowlist: &azureCloudInventoryAttributeAllowlist,
	},
}

// LoadCloudInventoryEvidence implements reducer.CloudInventoryEvidenceLoader. It
// returns the provider cloud-inventory records in scope for the given
// generation, bound to scope_id and generation_id so a stale generation cannot
// leak rows into a newer admission.
func (l PostgresCloudInventoryEvidenceLoader) LoadCloudInventoryEvidence(
	ctx context.Context,
	scopeID string,
	generationID string,
) ([]reducer.CloudInventoryRecord, error) {
	if l.DB == nil {
		return nil, fmt.Errorf("cloud inventory evidence database is required")
	}
	scopeID = strings.TrimSpace(scopeID)
	generationID = strings.TrimSpace(generationID)
	if scopeID == "" {
		return nil, fmt.Errorf("cloud inventory scope ID must not be blank")
	}
	if generationID == "" {
		return nil, fmt.Errorf("cloud inventory generation ID must not be blank")
	}

	rows, err := l.DB.QueryContext(ctx, listCloudInventorySourceFactsForGenerationQuery, scopeID, generationID)
	if err != nil {
		return nil, fmt.Errorf("list cloud inventory source facts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var records []reducer.CloudInventoryRecord
	for rows.Next() {
		var factKind, rawIdentity string
		var payload []byte
		if err := rows.Scan(&factKind, &rawIdentity, &payload); err != nil {
			return nil, fmt.Errorf("scan cloud inventory source fact: %w", err)
		}
		record, ok := cloudInventoryRecordFromRow(factKind, rawIdentity, payload)
		if !ok {
			l.logSkippedRow(ctx, scopeID, generationID, factKind, rawIdentity)
			continue
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cloud inventory source facts: %w", err)
	}
	return records, nil
}

// cloudInventoryRecordFromRow maps one source-fact row into the shared admission
// record. The raw identity comes from the SQL COALESCE so the provider-specific
// key is already resolved; the resource type is read from the provider's own
// payload key. Rows with an unrecognized fact kind, a blank raw identity, or an
// undecodable payload are dropped so the admission path never receives evidence
// it cannot key.
func cloudInventoryRecordFromRow(
	factKind string,
	rawIdentity string,
	payload []byte,
) (reducer.CloudInventoryRecord, bool) {
	mapping, ok := cloudInventorySourceFactMappings[factKind]
	if !ok {
		return reducer.CloudInventoryRecord{}, false
	}
	rawIdentity = strings.TrimSpace(rawIdentity)
	if rawIdentity == "" {
		return reducer.CloudInventoryRecord{}, false
	}

	var decoded map[string]any
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &decoded); err != nil {
			return reducer.CloudInventoryRecord{}, false
		}
	}

	return reducer.CloudInventoryRecord{
		Provider:     mapping.provider,
		FactKind:     factKind,
		RawIdentity:  rawIdentity,
		ResourceType: strings.TrimSpace(coerceJSONString(decoded[mapping.resourceTypeKey])),
		// The three inventory source fact kinds are provider control-plane
		// observations, so every loaded record is the observed evidence layer.
		// Declared and applied layers arrive from IaC/state source fact kinds in
		// a follow-up slice; the admission handler already keeps declared and
		// applied strictly above observed when those layers are wired.
		SourceLayer: reducer.SourceLayerObserved,
		Attributes:  cloudInventoryRecordAttributes(mapping, decoded),
	}, true
}

// cloudInventoryRecordAttributes returns the bounded attributes map for one
// provider mapping. GCP's typed-depth payload is already vetted safe end to
// end, so it passes through boundedCloudInventoryAttributes unfiltered by key
// name. AWS and Azure resource facts carry a raw-locator attributes map the
// cloud inventory route must never surface, so they are reduced through the
// mapping's closed attributeAllowlist instead; a mapping with neither
// surfacesAttributes nor an allowlist yields nil regardless of payload.
func cloudInventoryRecordAttributes(mapping cloudInventorySourceFactMapping, decoded map[string]any) map[string]any {
	raw := decoded["attributes"]
	if mapping.surfacesAttributes {
		return boundedCloudInventoryAttributes(raw)
	}
	if mapping.attributeAllowlist != nil {
		return mapping.attributeAllowlist.filter(raw)
	}
	return nil
}

// cloudInventoryAttributeAllowlist is a bounded, CLOSED per-provider allowlist
// of attribute keys the cloud inventory readback may surface. It exists
// because the AWS and Azure resource facts carry a raw provider-locator
// attributes map (cluster_arn, role_arn, network_interfaces, arm_resource_id,
// ...) that the route contract must never leak; only the keys explicitly
// named here survive, scoped to image/version deployed-code evidence (issue
// #5449). GCP does not use this type: its attributes map is already vetted
// safe and goes through boundedCloudInventoryAttributes as a raw passthrough.
type cloudInventoryAttributeAllowlist struct {
	// scalarKeys is the closed set of top-level scalar attribute keys kept
	// verbatim (via coerceJSONString) when the value is present and non-blank.
	scalarKeys map[string]struct{}
	// nestedArrayKeys maps a top-level array-of-object attribute key to the
	// closed set of sub-keys kept from each element. An element that is not a
	// JSON object, or whose every allowed sub-key is absent or blank, is
	// dropped; every non-allowlisted sub-key and top-level key is dropped.
	nestedArrayKeys map[string]map[string]struct{}
}

// filter reduces one provider's raw "attributes" payload value to this
// allowlist's closed key set: allowed top-level scalar keys with a non-blank
// value, plus allowed nested-array keys reduced element-by-element to their
// allowed sub-keys. It caps the result at maxCloudInventoryAttributeKeys as
// defense in depth (the allowlist itself is already far smaller than the cap).
// A raw value that is not a JSON object, or a zero-value allowlist (no scalar
// or nested-array keys configured, e.g. Azure today), yields nil.
func (allow cloudInventoryAttributeAllowlist) filter(raw any) map[string]any {
	object, ok := raw.(map[string]any)
	if !ok || len(object) == 0 {
		return nil
	}
	out := make(map[string]any, len(allow.scalarKeys)+len(allow.nestedArrayKeys))
	for key := range allow.scalarKeys {
		if len(out) >= maxCloudInventoryAttributeKeys {
			break
		}
		value, present := object[key]
		if !present {
			continue
		}
		if s, ok := cloudInventoryAllowlistScalarString(value); ok {
			out[key] = s
		}
	}
	for key, subKeys := range allow.nestedArrayKeys {
		if len(out) >= maxCloudInventoryAttributeKeys {
			break
		}
		if filtered := filterCloudInventoryNestedArray(object[key], subKeys); len(filtered) > 0 {
			out[key] = filtered
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// cloudInventoryAllowlistScalarString coerces an allowlisted attribute value
// to a non-blank string, but ONLY when the value is itself a JSON scalar
// (string, bool, float64, or json.Number). A map[string]any or []any value is
// explicitly rejected (ok=false) rather than stringified: a malformed provider
// payload can put a nested object under an allowlisted key name (for example
// task_definition_arn), and coerceJSONString's fmt.Sprint fallback would print
// that object's Go-syntax representation -- including any raw provider key
// inside it (cluster_arn, role_arn, ...) -- defeating the allowlist entirely.
// This mirrors boundedCloudInventoryAttributes' strict type switch, which has
// no default/stringify branch for exactly this reason.
func cloudInventoryAllowlistScalarString(value any) (string, bool) {
	switch v := value.(type) {
	case string:
		s := strings.TrimSpace(v)
		return s, s != ""
	case bool, float64, json.Number:
		s := strings.TrimSpace(coerceJSONString(v))
		return s, s != ""
	default:
		return "", false
	}
}

// filterCloudInventoryNestedArray reduces one nested array-of-object attribute
// value to the given closed sub-key set, keeping only non-blank SCALAR
// sub-values (via cloudInventoryAllowlistScalarString) from elements that
// decode as a JSON object. A raw value that is not a []any, an element that is
// not itself a JSON object, a sub-key whose value is a nested object/array, or
// an element with none of its allowlisted sub-keys present and non-blank, is
// dropped.
func filterCloudInventoryNestedArray(raw any, subKeys map[string]struct{}) []map[string]any {
	elements, ok := raw.([]any)
	if !ok || len(elements) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(elements))
	for _, element := range elements {
		item, ok := element.(map[string]any)
		if !ok {
			continue
		}
		kept := make(map[string]any, len(subKeys))
		for subKey := range subKeys {
			value, present := item[subKey]
			if !present {
				continue
			}
			if s, ok := cloudInventoryAllowlistScalarString(value); ok {
				kept[subKey] = s
			}
		}
		if len(kept) > 0 {
			out = append(out, kept)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// boundedCloudInventoryAttributes extracts the bounded attributes map from the
// decoded provider payload. It keeps only non-blank string keys (cap 64 keys)
// whose values are string, bool, json.Number, float64, or []any of strings.
// Everything else is dropped. This is defense-in-depth; the collector already
// bounds attributes before emission.
func boundedCloudInventoryAttributes(raw any) map[string]any {
	object, ok := raw.(map[string]any)
	if !ok || len(object) == 0 {
		return nil
	}
	out := make(map[string]any, len(object))
	for key, value := range object {
		if strings.TrimSpace(key) == "" {
			continue
		}
		if len(out) >= maxCloudInventoryAttributeKeys {
			break
		}
		switch v := value.(type) {
		case string:
			out[key] = v
		case bool:
			out[key] = v
		case float64:
			out[key] = v
		case json.Number:
			out[key] = v
		case []any:
			// Keep only string-typed elements; drop blank strings.
			strs := make([]string, 0, len(v))
			for _, elem := range v {
				if s, ok := elem.(string); ok && strings.TrimSpace(s) != "" {
					strs = append(strs, s)
				}
			}
			if len(strs) > 0 {
				out[key] = strs
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// logSkippedRow records a bounded diagnostic for one source-fact row the loader
// dropped. The fact kind is a bounded enum and is safe to log; the raw identity
// is emitted through the redaction-aware resource attribute helper so a resource
// id, ARN, full resource name, or ARM id never lands in a structured log
// verbatim.
func (l PostgresCloudInventoryEvidenceLoader) logSkippedRow(
	ctx context.Context,
	scopeID string,
	generationID string,
	factKind string,
	rawIdentity string,
) {
	if l.Logger == nil {
		return
	}
	attrs := []slog.Attr{
		slog.String(telemetry.LogKeyScopeID, scopeID),
		slog.String(telemetry.LogKeyGenerationID, generationID),
		slog.String(telemetry.LogKeyFailureClass, "cloud_inventory_source_fact_decode"),
		slog.String("fact_kind", factKind),
	}
	attrs = append(attrs, telemetry.SafeResourceLogAttrs(rawIdentity)...)
	l.Logger.LogAttrs(ctx, slog.LevelWarn, "cloud inventory evidence loader skipped source fact", attrs...)
}
