// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/secretsiam"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// assetTypeDataformRepository is the CAI asset type for a Dataform repository.
// Its optional CMEK edge reuses assetTypeKMSCryptoKey and cloudKMSResourceNamePrefix
// declared elsewhere in this package.
const assetTypeDataformRepository = "dataform.googleapis.com/Repository"

// relationshipTypeRepositoryEncryptedByKMSKey is the bounded provider
// relationship type for the Dataform repository CMEK edge. It is the only typed
// edge a repository's own resource.data can resolve: the git remote is an
// external (non-CAI) endpoint, the service account is an email (not an exactly
// resolvable CAI endpoint, carried as a fingerprint anchor), and workspaces and
// compiled BigQuery datasets are child/derived resources that reference the
// repository from their own assets rather than being named here.
const relationshipTypeRepositoryEncryptedByKMSKey = "dataform_repository_encrypted_by_kms_key"

func init() {
	RegisterAssetExtractor(assetTypeDataformRepository, extractDataformRepository)
}

// dataformRepositoryData is the bounded view of a CAI
// dataform.googleapis.com/Repository resource.data blob. The git auth token
// secret version, npmrc secret version, and SSH key material are intentionally
// not decoded; the git remote URL is reduced to a host fingerprint and the
// service account to an email fingerprint.
type dataformRepositoryData struct {
	GitRemoteSettings *struct {
		URL           string `json:"url"`
		DefaultBranch string `json:"defaultBranch"`
	} `json:"gitRemoteSettings"`
	ServiceAccount                string `json:"serviceAccount"`
	WorkspaceCompilationOverrides *struct {
		DefaultDatabase string `json:"defaultDatabase"`
	} `json:"workspaceCompilationOverrides"`
	KMSKeyName string `json:"kmsKeyName"`
	CreateTime string `json:"createTime"`
}

// extractDataformRepository extracts bounded, redaction-safe typed depth for one
// Dataform Repository CAI asset. It returns the Terraform/drift/monitoring
// attribute set (git default branch, git-remote host fingerprint, fingerprinted
// runtime service-account email, workspace default database, CMEK posture, and
// creation time), the CMEK CryptoKey and fingerprinted anchors, and the optional
// typed CMEK edge.
func extractDataformRepository(ctx ExtractContext) (AttributeExtraction, error) {
	var data dataformRepositoryData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode dataform repository data: %w", err)
	}

	hostFP := dataformGitHostFingerprint(gitRemoteURL(data))
	attrs := dataformRepositoryAttributes(data, hostFP)

	var anchors []string
	var rels []RelationshipObservation
	if hostFP != "" {
		anchors = append(anchors, hostFP)
	}
	if saFP := secretsiam.GCPServiceAccountEmailDigest(data.ServiceAccount); saFP != "" {
		anchors = append(anchors, saFP)
	}
	if kms := dataformKMSKeyFullName(data.KMSKeyName); kms != "" {
		anchors = append(anchors, kms)
		rels = append(rels, dataformRepositoryEdge(ctx, relationshipTypeRepositoryEncryptedByKMSKey, kms, assetTypeKMSCryptoKey))
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
		Relationships:      rels,
	}, nil
}

// dataformRepositoryAttributes assembles the bounded attribute map. Empty or
// absent fields are omitted rather than written as zero values so a partial CAI
// page does not fabricate a posture.
func dataformRepositoryAttributes(data dataformRepositoryData, gitHostFingerprint string) map[string]any {
	attrs := map[string]any{}

	if g := data.GitRemoteSettings; g != nil {
		if v := strings.TrimSpace(g.DefaultBranch); v != "" {
			attrs["git_default_branch"] = v
		}
	}
	if gitHostFingerprint != "" {
		attrs["git_remote_host_fingerprint"] = gitHostFingerprint
	}
	if fp := secretsiam.GCPServiceAccountEmailDigest(data.ServiceAccount); fp != "" {
		attrs["service_account_fingerprint"] = fp
	}
	if w := data.WorkspaceCompilationOverrides; w != nil {
		// defaultDatabase is the compilation target project/dataset name (a GCP
		// project-class identifier that already appears throughout resource names),
		// not a secret, so it is stored verbatim.
		if v := strings.TrimSpace(w.DefaultDatabase); v != "" {
			attrs["workspace_default_database"] = v
		}
	}
	if strings.TrimSpace(data.KMSKeyName) != "" {
		attrs["customer_managed_encryption"] = true
	}
	if v, ok := normalizeRFC3339(data.CreateTime); ok {
		attrs["creation_time"] = v
	}
	return attrs
}

// gitRemoteURL returns the configured git remote URL, or "".
func gitRemoteURL(data dataformRepositoryData) string {
	if data.GitRemoteSettings == nil {
		return ""
	}
	return data.GitRemoteSettings.URL
}

// dataformGitHostFingerprint reduces a git remote URL to a deterministic
// fingerprint of its host so repositories sharing a git host can be correlated
// without persisting the raw URL or host (a DNS name the collector contract
// fingerprints). The path (org/repo) and any embedded credentials are dropped.
// A blank URL or one with no host fingerprints to "".
func dataformGitHostFingerprint(rawURL string) string {
	host := gitRemoteHost(rawURL)
	if host == "" {
		return ""
	}
	return "sha256:" + facts.StableID("GCPDataformGitRemoteHost", map[string]any{"host": host})
}

// gitRemoteHost extracts the lowercase host from a git remote reference,
// supporting both URL forms (https://, ssh://, git://) and the SCP-like syntax
// git remotes commonly use ([user@]host.xz:path/to/repo.git). It returns "" when
// no host can be determined. The path and any embedded credentials are dropped.
func gitRemoteHost(rawURL string) string {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return ""
	}
	// SCP-like syntax has no scheme and a ":" that precedes the first "/".
	if !strings.Contains(trimmed, "://") {
		if colon := strings.Index(trimmed, ":"); colon >= 0 {
			if slash := strings.Index(trimmed, "/"); slash == -1 || colon < slash {
				hostPart := trimmed[:colon]
				if at := strings.LastIndex(hostPart, "@"); at >= 0 {
					hostPart = hostPart[at+1:]
				}
				return strings.ToLower(strings.TrimSpace(hostPart))
			}
		}
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(parsed.Hostname()))
}

// dataformKMSKeyFullName builds the CAI CryptoKey full resource name from a
// relative KMS key name. An already-normalized CAI full resource name is
// returned unchanged. It returns "" for a blank reference.
func dataformKMSKeyFullName(kmsKeyName string) string {
	trimmed := strings.TrimSpace(kmsKeyName)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "//") {
		return trimmed
	}
	// TrimPrefix guards against a hypothetical leading slash on the relative
	// resource name (Dataform reports it without one); the // form is already
	// handled above so the prefix is never doubled.
	return cloudKMSResourceNamePrefix + strings.TrimPrefix(trimmed, "/")
}

// dataformRepositoryEdge builds one typed provider relationship observation
// anchored on the repository's CAI full resource name.
func dataformRepositoryEdge(ctx ExtractContext, relationshipType, targetName, targetType string) RelationshipObservation {
	return RelationshipObservation{
		SourceFullResourceName: ctx.FullResourceName,
		SourceAssetType:        ctx.AssetType,
		RelationshipType:       relationshipType,
		TargetFullResourceName: targetName,
		TargetAssetType:        targetType,
		SupportState:           RelationshipSupportSupported,
	}
}
