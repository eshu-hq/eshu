package googleworkspace

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	sourceSystem          = "google_workspace"
	defaultMaxExportBytes = 10 * 1024 * 1024
)

// Collect emits source-neutral documentation facts from a mocked Workspace client.
func Collect(ctx context.Context, req Request) (Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if req.Client == nil {
		return Result{}, FailureAuthMissing
	}
	allowlist, allowlistKind, err := validateAllowlist(req.Allowlist)
	if err != nil {
		return Result{}, err
	}

	files, err := req.Client.ListFiles(ctx, allowlist)
	if err != nil {
		return Result{}, classifyFailure(err)
	}

	observedAt := req.ObservedAt
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}
	scopeID := firstNonEmpty(req.ScopeID, "doc-source:google_workspace:"+safeFingerprint(allowlistIdentity(allowlist)))
	generationID := firstNonEmpty(req.GenerationID, "gws-generation:"+safeFingerprint(scopeID+observedAt.Format(time.RFC3339Nano)))
	maxExportBytes := req.MaxExportBytes
	if maxExportBytes <= 0 {
		maxExportBytes = defaultMaxExportBytes
	}

	documents := make([]facts.Envelope, 0, len(files)*2)
	failures := 0
	for _, file := range files {
		envelopes, failed, buildErr := collectFile(ctx, req, scopeID, generationID, observedAt, maxExportBytes, file)
		if buildErr != nil {
			return Result{}, buildErr
		}
		if failed {
			failures++
		}
		documents = append(documents, envelopes...)
	}

	sourcePayload := sourcePayload(req, scopeID, allowlist, allowlistKind, len(files), failures)
	sourceEnvelope, err := envelope(scopeID, generationID, observedAt, facts.DocumentationSourceFactKind, facts.DocumentationSourceStableID(sourcePayload), sourcePayload, "", sourcePayload.ExternalID)
	if err != nil {
		return Result{}, err
	}
	out := []facts.Envelope{sourceEnvelope}
	out = append(out, documents...)
	return Result{Envelopes: out}, nil
}

func validateAllowlist(allowlist Allowlist) (Allowlist, string, error) {
	allowlist.FileIDs = cleanStrings(allowlist.FileIDs)
	allowlist.FolderIDs = cleanStrings(allowlist.FolderIDs)
	allowlist.SharedDriveIDs = cleanStrings(allowlist.SharedDriveIDs)
	allowlist.SharedDriveQuery = strings.TrimSpace(allowlist.SharedDriveQuery)
	if allowlist.AllowAllDrive {
		return Allowlist{}, "", FailureAllowlistUnsupportedScope
	}
	if len(allowlist.FileIDs)+len(allowlist.FolderIDs)+len(allowlist.SharedDriveIDs) == 0 {
		return Allowlist{}, "", FailureAllowlistEmpty
	}
	if len(allowlist.SharedDriveIDs) > 0 && allowlist.SharedDriveQuery == "" {
		return Allowlist{}, "", FailureAllowlistUnsupportedScope
	}
	switch {
	case len(allowlist.FileIDs) > 0 && len(allowlist.FolderIDs)+len(allowlist.SharedDriveIDs) == 0:
		return allowlist, "file", nil
	case len(allowlist.FolderIDs) > 0 && len(allowlist.FileIDs)+len(allowlist.SharedDriveIDs) == 0:
		return allowlist, "folder", nil
	case len(allowlist.SharedDriveIDs) > 0 && len(allowlist.FileIDs)+len(allowlist.FolderIDs) == 0:
		return allowlist, "shared_drive", nil
	default:
		return allowlist, "mixed", nil
	}
}

func collectFile(
	ctx context.Context,
	req Request,
	scopeID string,
	generationID string,
	observedAt time.Time,
	maxExportBytes int,
	file File,
) ([]facts.Envelope, bool, error) {
	acl := PermissionSummary{Visibility: "unknown"}
	failure := fileStateFailure(file, req.ExpectedRevisions)
	exportMIME := ""
	var export Export
	if failure == "" {
		var err error
		acl, err = req.Client.PermissionSummary(ctx, file.ID)
		if err != nil {
			failure = classifyFailure(err)
			acl = PermissionSummary{Visibility: "unknown", IsPartial: true, PartialReason: string(failure)}
		}
	}
	if failure == "" {
		var ok bool
		exportMIME, ok = exportMIMEForFile(file)
		if !ok {
			failure = FailureExportFormatUnsupported
		}
	}
	if failure == "" {
		var err error
		export, err = req.Client.Export(ctx, file.ID, exportMIME)
		if err != nil {
			failure = classifyFailure(err)
		}
	}
	if failure == "" && len(export.Bytes) > maxExportBytes {
		failure = FailureResourceLimitExceeded
	}

	document := documentPayload(scopeID, file, acl, exportMIME, failure)
	documentEnvelope, err := envelope(scopeID, generationID, observedAt, facts.DocumentationDocumentFactKind, facts.DocumentationDocumentStableID(document), document, file.WebURL, document.ExternalID)
	if err != nil {
		return nil, false, err
	}
	out := []facts.Envelope{documentEnvelope}
	if failure != "" {
		return out, true, nil
	}

	for i, section := range export.Sections {
		payload := sectionPayload(document, file, exportMIME, section, i)
		sectionEnvelope, err := envelope(scopeID, generationID, observedAt, facts.DocumentationSectionFactKind, facts.DocumentationSectionStableID(payload), payload, file.WebURL, document.ExternalID)
		if err != nil {
			return nil, false, err
		}
		out = append(out, sectionEnvelope)
	}
	for i, link := range export.Links {
		payload := linkPayload(document, link, i)
		linkEnvelope, err := envelope(scopeID, generationID, observedAt, facts.DocumentationLinkFactKind, facts.DocumentationLinkStableID(payload), payload, file.WebURL, document.ExternalID)
		if err != nil {
			return nil, false, err
		}
		out = append(out, linkEnvelope)
	}
	return out, false, nil
}

func fileStateFailure(file File, expected map[string]string) FailureClass {
	switch {
	case file.Deleted:
		return FailureSourceDeleted
	case file.Trashed:
		return FailureSourceTrashed
	case expected != nil && strings.TrimSpace(expected[file.ID]) != "" && expected[file.ID] != file.RevisionID:
		return FailureSourceRevisionStale
	default:
		return ""
	}
}

func exportMIMEForFile(file File) (string, bool) {
	return exportMIME(file.Kind)
}

func classifyFailure(err error) FailureClass {
	for _, class := range []FailureClass{
		FailurePermissionDenied,
		FailureProviderRateLimited,
		FailureProviderQuotaExceeded,
		FailureDownloadNotAllowed,
		FailureResourceLimitExceeded,
		FailureExportFormatUnsupported,
		FailureAllowlistUnsupportedScope,
		FailureAllowlistEmpty,
	} {
		if errors.Is(err, class) {
			return class
		}
	}
	return FailurePermissionDenied
}

func sourcePayload(req Request, scopeID string, allowlist Allowlist, allowlistKind string, fileCount int, failureCount int) facts.DocumentationSourcePayload {
	status := "completed"
	if failureCount > 0 {
		status = "partial"
	}
	return facts.DocumentationSourcePayload{
		SourceID:     scopeID,
		SourceSystem: sourceSystem,
		ExternalID:   "gws-allowlist:" + safeFingerprint(allowlistIdentity(allowlist)),
		DisplayName:  safeDisplayName(req.SourceName),
		SourceType:   allowlistKind,
		ACLSummary: &facts.DocumentationACLSummary{
			Visibility:    "unknown",
			IsPartial:     true,
			PartialReason: "runtime_not_enabled",
		},
		SourceMetadata: map[string]string{
			"allowlist_kind": allowlistKind,
			"file_count":     strconv.Itoa(fileCount),
			"failure_count":  strconv.Itoa(failureCount),
			"sync_status":    status,
			"runtime_status": "default_off_mocked_client",
		},
	}
}

func allowlistIdentity(allowlist Allowlist) string {
	return fmt.Sprintf(
		"files=%s;folders=%s;drives=%s;query=%s",
		strings.Join(allowlist.FileIDs, ","),
		strings.Join(allowlist.FolderIDs, ","),
		strings.Join(allowlist.SharedDriveIDs, ","),
		allowlist.SharedDriveQuery,
	)
}
