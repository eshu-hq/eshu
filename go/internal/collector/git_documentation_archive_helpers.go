// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"archive/zip"
	"io"
	"io/fs"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/archivepreflight"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func recordArchivePreflightMetadata(metadata map[string]string, result archivepreflight.Result) {
	metadata["archive_format"] = result.Format
	metadata["entry_count"] = strconv.Itoa(result.EntryCount)
	metadata["regular_file_count"] = strconv.Itoa(result.RegularFileCount)
	metadata["directory_count"] = strconv.Itoa(result.DirectoryCount)
	metadata["source_bytes"] = strconv.FormatInt(result.SourceBytes, 10)
	metadata["expanded_bytes"] = strconv.FormatInt(result.ExpandedBytes, 10)
	metadata["nested_member_count"] = strconv.Itoa(result.NestedCount)
	metadata["credential_member_count"] = strconv.Itoa(result.CredentialCount)
	metadata["symlink_member_count"] = strconv.Itoa(result.SymlinkCount)
	metadata["special_file_count"] = strconv.Itoa(result.SpecialFileCount)
}

func archivePreflightWarningStrings(result archivepreflight.Result) []string {
	warnings := make([]string, 0, len(result.Warnings))
	for _, warning := range result.Warnings {
		if warning.Count > 0 {
			warnings = append(warnings, string(warning.Class))
		}
	}
	return warnings
}

func archivePreflightHasFatalWarning(result archivepreflight.Result) bool {
	for _, warning := range result.Warnings {
		switch warning.Class {
		case archivepreflight.WarningMalformedContainer,
			archivepreflight.WarningResourceLimitExceeded,
			archivepreflight.WarningCompressionRatioExceeded,
			archivepreflight.WarningTimeout,
			archivepreflight.WarningArchivePathEscape:
			return true
		}
	}
	return false
}

func normalizeArchiveMemberPath(name string) (string, bool) {
	if name == "" || strings.ContainsRune(name, 0) || strings.Contains(name, "\\") {
		return "", false
	}
	trimmed := strings.TrimSuffix(name, "/")
	if trimmed == "" || strings.HasPrefix(trimmed, "/") || hasArchiveWindowsDrivePrefix(trimmed) {
		return "", false
	}
	for _, part := range strings.Split(trimmed, "/") {
		if part == "" || part == "." || part == ".." {
			return "", false
		}
	}
	cleaned := path.Clean(trimmed)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", false
	}
	return cleaned, true
}

func hasArchiveWindowsDrivePrefix(name string) bool {
	if len(name) < 2 || name[1] != ':' {
		return false
	}
	first := name[0]
	return (first >= 'A' && first <= 'Z') || (first >= 'a' && first <= 'z')
}

func archiveZipModeIsUnsafe(mode fs.FileMode) bool {
	return mode&(fs.ModeSymlink|fs.ModeDevice|fs.ModeCharDevice|fs.ModeNamedPipe|fs.ModeSocket|fs.ModeIrregular) != 0
}

func archiveZipModeWarning(mode fs.FileMode) string {
	if mode&fs.ModeSymlink != 0 {
		return string(archivepreflight.WarningArchiveSymlinkSkipped)
	}
	return string(archivepreflight.WarningArchiveSpecialFileSkipped)
}

func archiveMemberIsNested(name string) bool {
	lower := strings.ToLower(name)
	for _, suffix := range []string{".zip", ".tar", ".tar.gz", ".tgz", ".tar.bz2", ".tbz2", ".tar.xz", ".txz"} {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	return false
}

func archiveMemberLooksCredential(name string) bool {
	lower := strings.ToLower(filepath.ToSlash(name))
	for _, segment := range strings.Split(lower, "/") {
		if archiveCredentialSegment(segment) {
			return true
		}
	}
	return false
}

func archiveCredentialSegment(segment string) bool {
	switch segment {
	case ".env", ".netrc", "credentials", "credential", "secrets", "secret",
		"passwd", "password", "shadow", "token", "id_rsa", "id_dsa", "id_ecdsa", "id_ed25519":
		return true
	}
	if strings.HasPrefix(segment, ".env") {
		return true
	}
	base := strings.TrimSuffix(segment, path.Ext(segment))
	if archiveCredentialBase(base) {
		return true
	}
	return strings.HasSuffix(segment, ".pem") ||
		strings.HasSuffix(segment, ".key") ||
		strings.HasSuffix(segment, ".p12") ||
		strings.HasSuffix(segment, ".pfx")
}

func archiveCredentialBase(base string) bool {
	if base == "" {
		return false
	}
	for _, token := range []string{"secret", "secrets", "credential", "credentials", "password", "passwd", "token"} {
		if base == token ||
			strings.HasPrefix(base, token+"-") ||
			strings.HasPrefix(base, token+"_") ||
			strings.HasSuffix(base, "-"+token) ||
			strings.HasSuffix(base, "_"+token) ||
			strings.Contains(base, "-"+token+"-") ||
			strings.Contains(base, "_"+token+"_") {
			return true
		}
	}
	return false
}

func archiveUnsupportedWarning(memberPath string) string {
	switch strings.ToLower(path.Ext(memberPath)) {
	case ".docm", ".xlsm", ".pptm":
		return "unsupported_macro_enabled"
	case ".xls":
		return "unsupported_legacy_binary"
	default:
		return string(archivepreflight.WarningUnsupportedFormat)
	}
}

func readArchiveZIPMember(file *zip.File, maxBytes int) ([]byte, bool) {
	reader, err := file.Open()
	if err != nil {
		return nil, false
	}
	defer func() { _ = reader.Close() }()
	body, err := io.ReadAll(io.LimitReader(reader, int64(maxBytes+1)))
	if err != nil {
		return nil, false
	}
	if len(body) > maxBytes {
		return nil, false
	}
	return body, true
}

func archiveMemberSourceURI(archivePath string, memberPath string) string {
	return archivePath + "!/" + memberPath
}

func archiveMemberMetadata(
	archivePath string,
	memberPath string,
	ordinal int,
	memberHash string,
) map[string]string {
	return map[string]string{
		"archive_path":           archivePath,
		"archive_member_path":    memberPath,
		"archive_member_ordinal": strconv.Itoa(ordinal),
		"archive_member_hash":    memberHash,
	}
}

func decorateArchiveDocument(
	document *facts.DocumentationDocumentPayload,
	outerDocumentID string,
	outerCanonicalURI string,
	archiveMetadata map[string]string,
) {
	document.ParentDocumentID = outerDocumentID
	document.CanonicalURI = outerCanonicalURI + "!/" + archiveMetadata["archive_member_path"]
	for key, value := range archiveMetadata {
		document.SourceMetadata[key] = value
	}
}

func decorateArchiveSection(section *facts.DocumentationSectionPayload, archiveMetadata map[string]string) {
	for key, value := range archiveMetadata {
		section.SourceMetadata[key] = value
	}
}

func decorateArchiveLink(link *facts.DocumentationLinkPayload, archiveMetadata map[string]string) {
	if link.SourceMetadata == nil {
		link.SourceMetadata = map[string]string{}
	}
	for key, value := range archiveMetadata {
		link.SourceMetadata[key] = value
	}
}

func isDocumentationArchivePath(relativePath string) bool {
	clean := strings.ToLower(path.Clean(filepathToSourceURI(relativePath)))
	base := strings.TrimSuffix(path.Base(clean), path.Ext(clean))
	for _, segment := range strings.Split(path.Dir(clean), "/") {
		switch segment {
		case "doc", "docs", "documentation", "runbook", "runbooks", "adr", "adrs":
			return true
		}
	}
	for _, token := range []string{
		"archive", "audit", "bundle", "documentation", "docs", "evidence",
		"migration", "packet", "runbook", "support",
	} {
		if strings.Contains(base, token) {
			return true
		}
	}
	return false
}
