// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package cloud declares the fact-kind and schema-version constants for
// cloud-provider and cloud-derived-posture evidence: AWS, Azure, GCP,
// Kubernetes live-cluster observations, Terraform state, and the derived
// EC2 instance, RDS instance, S3 bucket, and S3 external-principal-grant
// posture facts. It moved out of go/internal/facts (issue #6776) with no
// identifier renamed: every exported name is unchanged from its facts-root
// declaration.
//
// Each family file (aws.go, azure.go, gcp.go, kubernetes_live.go,
// terraform_state.go, ec2_instance_posture.go, rds_posture.go,
// s3_bucket_posture.go, s3_external_principal_grant.go) declares that
// family's fact-kind string constants, its schema-version string
// constants, and two accessors: <Family>FactKinds() returns the family's
// kinds in collector emission order, and <Family>SchemaVersion(kind)
// returns the schema version accepted for one kind. Both accessors return
// copies; a caller that mutates a returned slice, or looks up a kind the
// family does not own, never affects the package's registry.
//
// This is a leaf declaration package: constants, ordering slices, and
// lookup maps only. It holds no collector logic, no reducer projection,
// and no I/O. Every fact kind here is a versioned, schema-admitted family:
// go/internal/facts/schema_version.go's schemaVersionFamilies table
// dispatches SchemaVersion, ClassifySchemaVersion, and ValidateSchemaVersion
// across these accessors alongside every other core family, reaching them
// through the facts root's compat_cloud.go and compat_cloud_posture.go,
// which import this package and forward every constant and accessor as
// facts.<Name> (issue #6776's root-wiring step, landed in this worktree --
// see README.md). The same kinds are
// registered in specs/fact-kind-registry.v1.yaml with their lifecycle
// owner, reducer domain, projection hook, and truth profile, and several
// AWS and S3 kinds also carry a checked-in payload schema.
//
// # Private-data boundary
//
// These statements moved here with the families from the facts root's
// package doc (issue #6776); each kind's own declaration carries the
// specific version.
//
// The AWS security-group-rule kind is a derived posture fact: one normalized
// ingress/egress rule the reducer projects into network-reachability edges.
// The derived IAM permission fact is metadata-only: it captures a normalized
// policy statement (effect, action set, resource pattern, condition-key
// summary) and never the raw policy JSON body or condition values.
//
// S3 posture covers block-public-access, default-encryption detail,
// versioning and MFA-delete, object-ownership / ACL-disabled, access-logging
// target, replication presence, and policy-derived public/cross-account
// booleans. External-principal grant evidence covers public, cross-account,
// AWS service, or unsupported-principal metadata. Neither S3 fact carries
// the raw bucket policy document, statement bodies, actions, resources,
// conditions, ACL grants, object keys, or object data, and reducers project
// them separately.
//
// Azure evidence is observed through Azure Resource Graph and bounded ARM
// fallback reads.
//
// GCP source facts never carry raw IAM policy JSON, secret values, object
// contents, startup scripts, public or private network addresses, provider
// response bodies, raw DNS names, or raw IAM member identities. Reducers own
// canonical CloudResource identity, tag evidence admission, relationship
// edges, image identity joins, drift, and query truth; raw IAM policy
// observations, DNS records, and collection warnings remain provenance-only
// or audit evidence until a later reducer/read-model contract admits them.
//
// Callers must treat every kind and schema-version constant here as a
// stable, additive-only contract: a new field on a resource or posture
// payload is additive, and a breaking schema change requires a new
// schema-version constant plus a specs/fact-kind-registry.v1.yaml update,
// never a silent reinterpretation of an existing version.
package cloud
