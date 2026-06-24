// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package exportmanifestpreflight

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

const (
	// SourceSystemGitHub identifies an offline GitHub issue or discussion export.
	SourceSystemGitHub = "github"
	// SourceSystemJira identifies an offline Jira issue export.
	SourceSystemJira = "jira"
	// SourceSystemSlack identifies an offline Slack export.
	SourceSystemSlack = "slack"
	// SourceSystemTeams identifies an offline Teams export.
	SourceSystemTeams = "teams"
	// SourceSystemGoogleWorkspaceExport identifies an offline Google Workspace export.
	SourceSystemGoogleWorkspaceExport = "google_workspace_export"
	// SourceSystemGenericDocumentationExport identifies a generic documentation export.
	SourceSystemGenericDocumentationExport = "generic_documentation_export"
)

const (
	// ACLPolicyEvaluated marks a source whose ACL was evaluated before import.
	ACLPolicyEvaluated = "source_acl_evaluated"
	// ACLPolicyPartial marks a source whose ACL evidence is partial.
	ACLPolicyPartial = "source_acl_partial"
	// ACLPolicyUnavailable marks a source whose ACL evidence is unavailable.
	ACLPolicyUnavailable = "source_acl_unavailable"
)

const (
	defaultMaxSourceBytes = int64(1 << 20)
	defaultMaxFiles       = 1000
)

// WarningClass is a stable, low-cardinality export manifest preflight class.
type WarningClass string

const (
	// WarningExportManifestInvalid marks malformed or incomplete import manifests.
	WarningExportManifestInvalid WarningClass = "export_manifest_invalid"
	// WarningExportFormatUnsupported marks unsupported source systems or formats.
	WarningExportFormatUnsupported WarningClass = "export_format_unsupported"
	// WarningAllowlistRequired marks missing explicit files or source scopes.
	WarningAllowlistRequired WarningClass = "allowlist_required"
	// WarningAllowlistEmpty marks manifests with no explicit file entries.
	WarningAllowlistEmpty WarningClass = "allowlist_empty"
	// WarningAllowlistUnsupportedScope marks broad live-provider-style scopes.
	WarningAllowlistUnsupportedScope WarningClass = "allowlist_unsupported_scope"
	// WarningACLUnavailable marks missing or unavailable source ACL evidence.
	WarningACLUnavailable WarningClass = "acl_unavailable"
	// WarningACLPartial marks partial source ACL evidence.
	WarningACLPartial WarningClass = "acl_partial"
	// WarningExportPathEscape marks absolute, parent-traversing, or non-local paths.
	WarningExportPathEscape WarningClass = "export_path_escape"
	// WarningExportArchiveMalformed marks nested archive members in manifests.
	WarningExportArchiveMalformed WarningClass = "export_archive_malformed"
	// WarningAttachmentMetadataOnly marks attachment binaries kept metadata-only.
	WarningAttachmentMetadataOnly WarningClass = "attachment_metadata_only"
	// WarningPrivateChannelMetadataOnly marks private conversation metadata without content approval.
	WarningPrivateChannelMetadataOnly WarningClass = "export_private_channel_metadata_only"
	// WarningSensitiveValueRedacted marks credential-looking manifest entries.
	WarningSensitiveValueRedacted WarningClass = "sensitive_value_redacted"
	// WarningDuplicateSourceItem marks repeated source item identities.
	WarningDuplicateSourceItem WarningClass = "duplicate_source_item"
	// WarningResourceLimitExceeded marks source-byte or file-count limits.
	WarningResourceLimitExceeded WarningClass = "resource_limit_exceeded"
	// WarningTimeout marks caller cancellation or deadline during preflight.
	WarningTimeout WarningClass = "timeout"
)

// Options bounds offline export manifest preflight work.
type Options struct {
	MaxSourceBytes int64
	MaxFiles       int
}

// Warning records one bounded manifest preflight class.
type Warning struct {
	Class WarningClass `json:"class"`
	Count int          `json:"count"`
}

// Result summarizes metadata-only export manifest preflight.
type Result struct {
	SourceSystem                 string    `json:"source_system,omitempty"`
	ACLPolicy                    string    `json:"acl_policy,omitempty"`
	Safe                         bool      `json:"safe"`
	Warnings                     []Warning `json:"warnings,omitempty"`
	SourceBytes                  int64     `json:"source_bytes"`
	FileCount                    int       `json:"file_count"`
	AttachmentReferenceCount     int       `json:"attachment_reference_count"`
	DeletedItemCount             int       `json:"deleted_item_count"`
	EditedItemCount              int       `json:"edited_item_count"`
	PrivateMetadataCount         int       `json:"private_metadata_count"`
	NestedArchiveCount           int       `json:"nested_archive_count"`
	SensitiveValueCount          int       `json:"sensitive_value_count"`
	PathEscapeCount              int       `json:"path_escape_count"`
	BroadScopeCount              int       `json:"broad_scope_count"`
	AllowlistEmptyCount          int       `json:"allowlist_empty_count"`
	ACLUnavailableCount          int       `json:"acl_unavailable_count"`
	ACLPartialCount              int       `json:"acl_partial_count"`
	DuplicateSourceItemCount     int       `json:"duplicate_source_item_count"`
	UnsupportedSourceSystemCount int       `json:"unsupported_source_system_count"`
}

type recorder struct {
	result *Result
	seen   map[WarningClass]int
}

type manifest struct {
	SourceSystem    string         `json:"source_system"`
	SourceScopeID   string         `json:"source_scope_id"`
	SourceScopeKind string         `json:"source_scope_kind"`
	ExportedAt      string         `json:"exported_at"`
	SourceRevision  string         `json:"source_revision"`
	SourceCursor    string         `json:"source_cursor"`
	ACLPolicy       string         `json:"acl_policy"`
	Files           []manifestFile `json:"files"`
	Metadata        manifestMeta   `json:"metadata"`
}

type manifestFile struct {
	Path           string `json:"path"`
	Kind           string `json:"kind"`
	ContentType    string `json:"content_type"`
	SourceItemID   string `json:"source_item_id"`
	URL            string `json:"url"`
	Deleted        bool   `json:"deleted"`
	Edited         bool   `json:"edited"`
	PrivateChannel bool   `json:"private_channel"`
}

type manifestMeta struct {
	PrivateChannel bool `json:"private_channel"`
}

// Preflight classifies an offline documentation export manifest without reading export content.
func Preflight(ctx context.Context, sourceName string, reader io.Reader, options Options) (Result, error) {
	_ = sourceName
	if reader == nil {
		return Result{}, fmt.Errorf("reader must not be nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	opts := normalizeOptions(options)
	rec := recorder{result: &Result{Safe: true}, seen: map[WarningClass]int{}}

	if err := ctx.Err(); err != nil {
		rec.warn(WarningTimeout)
		return rec.finalize(), err
	}
	body, ok := readBoundedManifest(reader, opts.MaxSourceBytes, &rec)
	if !ok {
		return rec.finalize(), nil
	}

	var decoded manifest
	if err := json.Unmarshal(body, &decoded); err != nil {
		rec.warn(WarningExportManifestInvalid)
		return rec.finalize(), nil
	}
	rec.classifyManifest(ctx, decoded, opts)
	return rec.finalize(), nil
}

func normalizeOptions(options Options) Options {
	if options.MaxSourceBytes <= 0 {
		options.MaxSourceBytes = defaultMaxSourceBytes
	}
	if options.MaxFiles <= 0 {
		options.MaxFiles = defaultMaxFiles
	}
	return options
}

func readBoundedManifest(reader io.Reader, maxBytes int64, rec *recorder) ([]byte, bool) {
	body, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		rec.warn(WarningExportManifestInvalid)
		return nil, false
	}
	rec.result.SourceBytes = int64(len(body))
	if int64(len(body)) > maxBytes {
		rec.warn(WarningResourceLimitExceeded)
		return nil, false
	}
	return body, true
}

func (r *recorder) classifyManifest(ctx context.Context, decoded manifest, options Options) {
	if err := ctx.Err(); err != nil {
		r.warn(WarningTimeout)
		return
	}
	r.classifySource(decoded)
	r.classifyACL(decoded.ACLPolicy)
	if decoded.ExportedAt == "" && decoded.SourceRevision == "" && decoded.SourceCursor == "" {
		r.warn(WarningExportManifestInvalid)
	}
	if decoded.SourceScopeID == "" {
		r.warn(WarningAllowlistRequired)
	}
	if len(decoded.Files) == 0 {
		r.result.AllowlistEmptyCount++
		r.warn(WarningAllowlistRequired)
		r.warn(WarningAllowlistEmpty)
	}
	if broadScope(decoded.SourceScopeID, decoded.SourceScopeKind) {
		r.result.BroadScopeCount++
		r.warn(WarningAllowlistUnsupportedScope)
	}
	r.result.FileCount = len(decoded.Files)
	if len(decoded.Files) > options.MaxFiles {
		r.warn(WarningResourceLimitExceeded)
	}
	if decoded.Metadata.PrivateChannel {
		r.result.PrivateMetadataCount++
		r.warn(WarningPrivateChannelMetadataOnly)
	}
	seenSourceItems := map[string]struct{}{}
	limit := len(decoded.Files)
	if limit > options.MaxFiles {
		limit = options.MaxFiles
	}
	for i := 0; i < limit; i++ {
		if err := ctx.Err(); err != nil {
			r.warn(WarningTimeout)
			return
		}
		r.classifyFile(decoded.Files[i], seenSourceItems)
	}
}

func (r *recorder) classifySource(decoded manifest) {
	if supportedSourceSystem(decoded.SourceSystem) {
		r.result.SourceSystem = decoded.SourceSystem
		return
	}
	r.result.UnsupportedSourceSystemCount++
	r.warn(WarningExportFormatUnsupported)
}

func (r *recorder) classifyACL(policy string) {
	switch policy {
	case ACLPolicyEvaluated:
		r.result.ACLPolicy = policy
	case ACLPolicyPartial:
		r.result.ACLPolicy = policy
		r.result.ACLPartialCount++
		r.warn(WarningACLPartial)
	case "", ACLPolicyUnavailable:
		if policy != "" {
			r.result.ACLPolicy = policy
		}
		r.result.ACLUnavailableCount++
		r.warn(WarningACLUnavailable)
	default:
		r.warn(WarningExportManifestInvalid)
	}
}

func (r *recorder) classifyFile(file manifestFile, seenSourceItems map[string]struct{}) {
	r.classifySourceItem(file.SourceItemID, seenSourceItems)
	if unsafeManifestPath(file.Path) {
		r.result.PathEscapeCount++
		r.warn(WarningExportPathEscape)
	}
	if nestedArchivePath(file.Path) {
		r.result.NestedArchiveCount++
		r.warn(WarningExportArchiveMalformed)
	}
	if credentialPath(file.Path) {
		r.result.SensitiveValueCount++
		r.warn(WarningSensitiveValueRedacted)
	}
	if tokenBearingURL(file.URL) {
		r.result.SensitiveValueCount++
		r.warn(WarningSensitiveValueRedacted)
	}
	if attachmentReference(file) {
		r.result.AttachmentReferenceCount++
		r.warn(WarningAttachmentMetadataOnly)
	}
	if file.PrivateChannel {
		r.result.PrivateMetadataCount++
		r.warn(WarningPrivateChannelMetadataOnly)
	}
	if file.Deleted {
		r.result.DeletedItemCount++
	}
	if file.Edited {
		r.result.EditedItemCount++
	}
}

func (r *recorder) classifySourceItem(sourceItemID string, seenSourceItems map[string]struct{}) {
	id := strings.TrimSpace(sourceItemID)
	if id == "" {
		return
	}
	if _, ok := seenSourceItems[id]; ok {
		r.result.DuplicateSourceItemCount++
		r.warn(WarningDuplicateSourceItem)
		return
	}
	seenSourceItems[id] = struct{}{}
}

func supportedSourceSystem(sourceSystem string) bool {
	switch sourceSystem {
	case SourceSystemGitHub, SourceSystemJira, SourceSystemSlack, SourceSystemTeams,
		SourceSystemGoogleWorkspaceExport, SourceSystemGenericDocumentationExport:
		return true
	default:
		return false
	}
}

func broadScope(scopeID, scopeKind string) bool {
	id := strings.ToLower(strings.TrimSpace(scopeID))
	kind := strings.ToLower(strings.TrimSpace(scopeKind))
	if id == "*" || id == "all" || id == "all_drives" || id == "alldrives" {
		return true
	}
	switch kind {
	case "all", "domain", "workspace", "organization", "org", "tenant", "user_root", "alldrives", "all_drives":
		return true
	default:
		return false
	}
}

func unsafeManifestPath(name string) bool {
	if name == "" || strings.ContainsRune(name, 0) || strings.Contains(name, "\\") {
		return true
	}
	trimmed := strings.TrimSuffix(name, "/")
	if trimmed == "" || strings.HasPrefix(trimmed, "/") || hasWindowsDrivePrefix(trimmed) {
		return true
	}
	for _, part := range strings.Split(trimmed, "/") {
		if part == "" || part == "." || part == ".." {
			return true
		}
	}
	cleaned := path.Clean(trimmed)
	return cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../")
}

func hasWindowsDrivePrefix(name string) bool {
	return len(name) >= 2 && name[1] == ':' &&
		((name[0] >= 'A' && name[0] <= 'Z') || (name[0] >= 'a' && name[0] <= 'z'))
}

func nestedArchivePath(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".zip") || strings.HasSuffix(lower, ".tar") ||
		strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz") ||
		strings.HasSuffix(lower, ".rar") || strings.HasSuffix(lower, ".7z")
}

func credentialPath(name string) bool {
	lower := strings.ToLower(filepath.Base(name))
	full := strings.ToLower(name)
	return lower == ".env" || strings.Contains(full, "secret") ||
		strings.Contains(full, "credential") || strings.Contains(full, "private_key") ||
		strings.Contains(full, "id_rsa") || strings.HasSuffix(lower, ".pem") ||
		strings.HasSuffix(lower, ".p12") || strings.HasSuffix(lower, ".pfx") ||
		strings.Contains(full, "token")
}

func tokenBearingURL(rawURL string) bool {
	if strings.TrimSpace(rawURL) == "" || !strings.Contains(rawURL, "?") {
		return false
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	for key := range parsed.Query() {
		if sensitiveQueryKey(key) {
			return true
		}
	}
	return false
}

func sensitiveQueryKey(key string) bool {
	switch strings.ToLower(key) {
	case "token", "access_token", "id_token", "refresh_token", "signature",
		"x-amz-signature", "sig", "secret", "password", "api_key", "key":
		return true
	default:
		return false
	}
}

func attachmentReference(file manifestFile) bool {
	if strings.EqualFold(file.Kind, "attachment") {
		return true
	}
	contentType := strings.ToLower(file.ContentType)
	return contentType != "" && !strings.HasPrefix(contentType, "text/") &&
		contentType != "application/json" && contentType != "application/x-ndjson"
}

func (r *recorder) warn(class WarningClass) {
	if count, ok := r.seen[class]; ok {
		r.seen[class] = count + 1
		for i := range r.result.Warnings {
			if r.result.Warnings[i].Class == class {
				r.result.Warnings[i].Count++
				break
			}
		}
	} else {
		r.seen[class] = 1
		r.result.Warnings = append(r.result.Warnings, Warning{Class: class, Count: 1})
	}
	r.result.Safe = false
}

func (r *recorder) finalize() Result {
	if len(r.result.Warnings) > 0 {
		r.result.Safe = false
		sort.Slice(r.result.Warnings, func(left, right int) bool {
			return r.result.Warnings[left].Class < r.result.Warnings[right].Class
		})
	}
	return *r.result
}
