// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package payloadusage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// factKindSchemaFile maps a factschema.FactKind* constant identifier to its
// checked-in JSON Schema file base name under sdk/go/factschema/schema/. This
// mirrors the file-naming convention every schema in that directory follows
// (fact kind name + ".v1.schema.json"); it is intentionally a lookup this
// file owns (not derived from the fact-kind string via string manipulation
// alone) so a future schema file rename is a one-line change here rather than
// a silent lookup miss.
var factKindSchemaFile = map[string]string{
	"FactKindAWSResource":                 "aws_resource.v1.schema.json",
	"FactKindAWSRelationship":             "aws_relationship.v1.schema.json",
	"FactKindAWSSecurityGroupRule":        "aws_security_group_rule.v1.schema.json",
	"FactKindEC2InstancePosture":          "ec2_instance_posture.v1.schema.json",
	"FactKindS3BucketPosture":             "s3_bucket_posture.v1.schema.json",
	"FactKindAWSIAMPermission":            "aws_iam_permission.v1.schema.json",
	"FactKindAWSResourcePolicyPermission": "aws_resource_policy_permission.v1.schema.json",
	"FactKindAWSIAMPrincipal":             "aws_iam_principal.v1.schema.json",
	// Incident family: ONLY the kinds a reducer decode seam wrapper actually
	// decodes (factschema_decode_incident.go) are mapped, so the gate covers
	// exactly what the reducer reads through the typed seam. The unwired
	// incident kinds (lifecycle_event, change.record, applied_alert_route,
	// observed_pagerduty_integration) carry a schema but no reducer decode
	// call, so they are intentionally absent here — mapping them would assert a
	// gate contract for a kind no handler reads.
	"FactKindIncidentRecord":                          "incident.record.v1.schema.json",
	"FactKindIncidentRoutingAppliedPagerDutyResource": "incident_routing.applied_pagerduty_resource.v1.schema.json",
	"FactKindIncidentRoutingObservedPagerDutyService": "incident_routing.observed_pagerduty_service.v1.schema.json",
	"FactKindIncidentRoutingCoverageWarning":          "incident_routing.coverage_warning.v1.schema.json",
	// GCP family: only the two wired cloud kinds the reducer decodes.
	"FactKindGCPCloudResource":                        "gcp_cloud_resource.v1.schema.json",
	"FactKindGCPCloudRelationship":                    "gcp_cloud_relationship.v1.schema.json",
	// Azure family: only the two wired cloud kinds the reducer decodes.
	"FactKindAzureCloudResource":                      "azure_cloud_resource.v1.schema.json",
	"FactKindAzureCloudRelationship":                  "azure_cloud_relationship.v1.schema.json",
}

// jsonSchemaDocument is the subset of a checked-in factschema JSON Schema
// this gate reads: the declared property names. Property type/required
// details are schema-diff's concern (issue #4569, the forward direction);
// this gate only needs to know which top-level keys are declared at all, to
// check the reverse direction (a handler reading a field no schema declares).
type jsonSchemaDocument struct {
	Properties map[string]json.RawMessage `json:"properties"`
}

// LoadDeclaredFieldsFromSchemas reads every JSON Schema file
// factKindSchemaFile names under schemaDir and returns the declared property
// name set per FactKind constant, in the shape CheckManifest's
// declaredOverride parameter expects.
//
// A mapped schema file that is MISSING is a fail-closed ERROR, not a skip.
// Every fact kind in factKindSchemaFile is a kind Load already requires a
// decode seam for (via UnmappedSeamFactKinds), so its schema file must exist:
// if a schema were deleted or moved, silently skipping it would make
// CheckManifest fall back to the manifest's OWN DeclaredFields for that kind,
// which can never report a violation — a false-green that would disable the
// gate for that kind precisely when its declared contract vanished. Failing
// closed here means a removed schema fails the gate loudly instead.
func LoadDeclaredFieldsFromSchemas(schemaDir string) (map[string]map[string]struct{}, error) {
	declared := map[string]map[string]struct{}{}
	for factKindConst, fileName := range factKindSchemaFile {
		path := filepath.Join(schemaDir, fileName)
		// #nosec G304 -- path is schemaDir (a CLI/gate-configured directory)
		// joined with a fixed name from this file's own factKindSchemaFile
		// map, not untrusted input.
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf(
				"payload-usage-manifest: read declared schema %s for fact kind %s: %w (a mapped schema file must exist; a missing one would silently disable the gate for this kind)",
				path, factKindConst, err,
			)
		}
		var doc jsonSchemaDocument
		if err := json.Unmarshal(raw, &doc); err != nil {
			return nil, fmt.Errorf("payload-usage-manifest: parse schema %s: %w", path, err)
		}
		fields := make(map[string]struct{}, len(doc.Properties))
		for name := range doc.Properties {
			fields[name] = struct{}{}
		}
		declared[factKindConst] = fields
	}
	return declared, nil
}

// MergeRegistryPayloadSchemaFields is an ADDITIVE input hook for issue
// #4570's registry v2 payload_schema refs. Per issue #4573's "Out of scope"
// section, registry v2 field validation is issue #4570's own concern and may
// not have landed payload_schema refs yet; this gate's source of truth is
// sdk/go/factschema/schema/*.json (LoadDeclaredFieldsFromSchemas). When a
// registry payload_schema ref is present for a kind, callers MAY widen
// (never narrow) declared with it — narrowing here would let a registry
// authoring bug fail this gate for a field the real schema already declares.
// registryFields is nil-safe: an empty or nil map is a no-op.
func MergeRegistryPayloadSchemaFields(declared map[string]map[string]struct{}, registryFields map[string]map[string]struct{}) map[string]map[string]struct{} {
	if len(registryFields) == 0 {
		return declared
	}
	merged := make(map[string]map[string]struct{}, len(declared))
	for k, v := range declared {
		merged[k] = v
	}
	for factKind, fields := range registryFields {
		existing := merged[factKind]
		if existing == nil {
			existing = map[string]struct{}{}
		}
		widened := make(map[string]struct{}, len(existing)+len(fields))
		for f := range existing {
			widened[f] = struct{}{}
		}
		for f := range fields {
			widened[f] = struct{}{}
		}
		merged[factKind] = widened
	}
	return merged
}

// isKnownFactKindConstant reports whether factKindConst has a schema file
// mapping registered.
func isKnownFactKindConstant(factKindConst string) bool {
	_, ok := factKindSchemaFile[factKindConst]
	return ok
}

// UnmappedSeamFactKinds returns the FactKindConst of every seam with no
// schema-file mapping in factKindSchemaFile, sorted, so a caller can fail
// loudly on a newly migrated kind whose schema mapping was forgotten rather
// than silently skipping its gate coverage.
func UnmappedSeamFactKinds(seams []DecodeSeam) []string {
	var missing []string
	for _, s := range seams {
		if !isKnownFactKindConstant(s.FactKindConst) {
			missing = append(missing, s.FactKindConst)
		}
	}
	sort.Strings(missing)
	return missing
}

// JoinSorted is a small formatting helper for error messages listing several
// fact kind identifiers.
func JoinSorted(names []string) string {
	return strings.Join(names, ", ")
}
