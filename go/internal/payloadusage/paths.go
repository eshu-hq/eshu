// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package payloadusage

import (
	"path/filepath"
	"strings"
)

// Paths locates every filesystem input the manifest derivation reads. Every
// field defaults relative to RepoRoot when left empty; see ResolvePaths.
type Paths struct {
	// RepoRoot is the repository root used to resolve every other empty
	// field. Defaults to "." when empty.
	RepoRoot string
	// ReducerDir is go/internal/reducer — both the source of the decode
	// seam files (DecodeFiles) and the handler files ScanDecodeUsage walks.
	ReducerDir string
	// DecodeFile, when set, restricts seam parsing to that single file. It is
	// the CLI's -decode-file override; leaving it empty is the normal path and
	// lets DecodeFiles resolve to the per-family glob below. It is retained for
	// backward compatibility with callers that pin one file.
	DecodeFile string
	// DecodeFiles is the set of reducer decode-seam files ParseDecodeSeams
	// reads. Families split their decode wrappers into per-family files
	// (factschema_decode.go, factschema_decode_incident.go, ...) as the
	// 500-line cap forces a split, so the seam source is a GLOB, not a single
	// file. When empty, ResolvePaths fills it from DecodeFile (if set) or from
	// filepath.Glob(ReducerDir/"factschema_decode*.go"). A gate that read only
	// the single factschema_decode.go would silently miss a family whose
	// wrappers live in a split file — the exact false-green this glob closes.
	DecodeFiles []string
	// SchemaDir is sdk/go/factschema/schema, the checked-in JSON Schemas
	// LoadDeclaredFieldsFromSchemas reads.
	SchemaDir string
	// AWSStructDir is sdk/go/factschema/aws/v1.
	AWSStructDir string
	// IAMStructDir is sdk/go/factschema/iam/v1.
	IAMStructDir string
	// IncidentStructDir is sdk/go/factschema/incident/v1.
	IncidentStructDir string
	// GCPStructDir is sdk/go/factschema/gcp/v1.
	GCPStructDir string
	// AzureStructDir is sdk/go/factschema/azure/v1.
	AzureStructDir string
	// KubernetesLiveStructDir is sdk/go/factschema/kuberneteslive/v1.
	KubernetesLiveStructDir string
	// OCIRegistryStructDir is sdk/go/factschema/ociregistry/v1.
	OCIRegistryStructDir string
	// TerraformStateStructDir is sdk/go/factschema/terraformstate/v1.
	TerraformStateStructDir string
	// PackageRegistryStructDir is sdk/go/factschema/packageregistry/v1.
	PackageRegistryStructDir string
	// SBOMStructDir is sdk/go/factschema/sbom/v1.
	SBOMStructDir string
	// VulnerabilityStructDir is sdk/go/factschema/vulnerability/v1.
	VulnerabilityStructDir string
	// CICDRunStructDir is sdk/go/factschema/cicdrun/v1.
	CICDRunStructDir string
	// SecretsIAMStructDir is sdk/go/factschema/secretsiam/v1 (Contract System
	// v1 Wave 4d: the secrets_iam family's VAULT + K8S lanes only; the AWS IAM
	// lane's structs live in IAMStructDir, and the GCP IAM lane is deferred).
	SecretsIAMStructDir string
	// WorkItemStructDir is sdk/go/factschema/workitem/v1.
	WorkItemStructDir string
	// SecurityAlertStructDir is sdk/go/factschema/securityalert/v1.
	SecurityAlertStructDir string
	// ProjectorDir is go/internal/projector — the source of the projector's
	// decode-seam files (ProjectorDecodeFiles) and the canonical-extractor files
	// ScanDecodeUsage walks for the projector-side decode sites. The projector is
	// the primary graph-identity producer for the oci_registry family (its
	// canonical extractor decodes through the same sdk/go/factschema seam the
	// reducer uses), so the manifest gate must scan it alongside ReducerDir.
	ProjectorDir string
	// ProjectorDecodeFiles is the set of projector decode-seam files to parse.
	// When empty, ResolvePaths does NOT default it (same rationale as
	// DecodeFiles); resolveProjectorDecodeFiles fills it by globbing
	// factschema_decode*.go under ProjectorDir.
	ProjectorDecodeFiles []string
	// QueryDir is go/internal/query — the source of the query layer's
	// decode-seam files (QueryDecodeFiles) and the read-model row-builder files
	// ScanDecodeUsage walks for the query-side decode sites. The query
	// read-model layer is the ONLY decode site for the work_item family (no
	// reducer or projector domain consumes work_item.* payloads; see
	// sdk/go/factschema/workitem/v1/README.md), so the manifest gate must scan
	// it alongside ReducerDir and ProjectorDir.
	QueryDir string
	// QueryDecodeFiles is the set of query-layer decode-seam files to parse.
	// When empty, ResolvePaths does NOT default it (same rationale as
	// ProjectorDecodeFiles); resolveQueryDecodeFiles fills it by globbing
	// factschema_decode*.go under QueryDir. An empty match is valid (today only
	// the work_item family is typed at the query layer), mirroring the
	// projector's non-fail-closed glob.
	QueryDecodeFiles []string
}

// ResolvePaths fills every empty DIRECTORY/RepoRoot field of p with its default
// relative to RepoRoot (defaulting RepoRoot itself to "." when empty) and
// returns the resolved copy. p is not mutated.
//
// It deliberately does NOT resolve DecodeFile or DecodeFiles: the decode-seam
// source is a glob whose resolution can fail, and ResolvePaths returns no error.
// resolveDecodeFiles (called from Load) fills them — from an explicit
// DecodeFile/DecodeFiles override, or by globbing factschema_decode*.go under
// ReducerDir. So a caller inspecting the returned Paths sees resolved
// directories but empty DecodeFile/DecodeFiles unless it set them.
func ResolvePaths(p Paths) Paths {
	resolved := p
	resolved.RepoRoot = strings.TrimSpace(resolved.RepoRoot)
	if resolved.RepoRoot == "" {
		resolved.RepoRoot = "."
	}
	if strings.TrimSpace(resolved.ReducerDir) == "" {
		resolved.ReducerDir = filepath.Join(resolved.RepoRoot, "go", "internal", "reducer")
	}
	if strings.TrimSpace(resolved.SchemaDir) == "" {
		resolved.SchemaDir = filepath.Join(resolved.RepoRoot, "sdk", "go", "factschema", "schema")
	}
	// Each family's *StructDir defaults to sdk/go/factschema/<family>/v1 when
	// left empty. A data-driven fill (rather than one repeated 3-line
	// if-block per family) keeps this function's length proportional to the
	// number of typed families, not 3x that, as new families are added.
	for _, family := range []struct {
		dir  *string
		name string
	}{
		{&resolved.AWSStructDir, "aws"},
		{&resolved.IAMStructDir, "iam"},
		{&resolved.IncidentStructDir, "incident"},
		{&resolved.GCPStructDir, "gcp"},
		{&resolved.AzureStructDir, "azure"},
		{&resolved.KubernetesLiveStructDir, "kuberneteslive"},
		{&resolved.OCIRegistryStructDir, "ociregistry"},
		{&resolved.TerraformStateStructDir, "terraformstate"},
		{&resolved.PackageRegistryStructDir, "packageregistry"},
		{&resolved.SBOMStructDir, "sbom"},
		{&resolved.VulnerabilityStructDir, "vulnerability"},
		{&resolved.CICDRunStructDir, "cicdrun"},
		{&resolved.SecretsIAMStructDir, "secretsiam"},
		{&resolved.WorkItemStructDir, "workitem"},
		{&resolved.SecurityAlertStructDir, "securityalert"},
	} {
		if strings.TrimSpace(*family.dir) == "" {
			*family.dir = filepath.Join(resolved.RepoRoot, "sdk", "go", "factschema", family.name, "v1")
		}
	}
	if strings.TrimSpace(resolved.ProjectorDir) == "" {
		resolved.ProjectorDir = filepath.Join(resolved.RepoRoot, "go", "internal", "projector")
	}
	if strings.TrimSpace(resolved.QueryDir) == "" {
		resolved.QueryDir = filepath.Join(resolved.RepoRoot, "go", "internal", "query")
	}
	// DecodeFile / DecodeFiles are intentionally NOT defaulted here: the glob
	// path can fail, and ResolvePaths returns no error. resolveDecodeFiles (from
	// Load) fills them — from an explicit DecodeFile/DecodeFiles override, or by
	// globbing every factschema_decode*.go under ReducerDir. Defaulting
	// DecodeFile to the single legacy file here would defeat the glob and
	// silently drop the per-family split files (the false-green this closes).
	return resolved
}
