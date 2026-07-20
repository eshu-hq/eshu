// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repositoryidentity

import (
	"crypto/sha1" // #nosec G505 -- non-cryptographic repository identity digest, not a security primitive
	"encoding/hex"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
)

// Metadata describes one canonical repository identity.
type Metadata struct {
	ID        string
	Name      string
	RepoSlug  string
	RemoteURL string
	LocalPath string
	HasRemote bool
}

// MetadataFor returns canonical repository metadata using remote-first identity.
func MetadataFor(name string, localPath string, remoteURL string) (Metadata, error) {
	normalizedLocalPath := ""
	if strings.TrimSpace(localPath) != "" {
		resolved, err := filepath.Abs(localPath)
		if err != nil {
			return Metadata{}, fmt.Errorf("resolve local path: %w", err)
		}
		normalizedLocalPath = resolved
	}

	normalizedRemoteURL := NormalizeRemoteURL(remoteURL)
	repoSlug := RepoSlugFromRemoteURL(normalizedRemoteURL)
	repoID, err := CanonicalRepositoryID(normalizedRemoteURL, normalizedLocalPath)
	if err != nil {
		return Metadata{}, err
	}

	return Metadata{
		ID:        repoID,
		Name:      name,
		RepoSlug:  repoSlug,
		RemoteURL: normalizedRemoteURL,
		LocalPath: normalizedLocalPath,
		HasRemote: normalizedRemoteURL != "",
	}, nil
}

// NormalizeRemoteURL normalizes SSH and HTTPS git remotes into canonical HTTPS.
func NormalizeRemoteURL(remoteURL string) string {
	candidate := strings.TrimSpace(remoteURL)
	if candidate == "" {
		return ""
	}

	host := ""
	path := ""
	switch {
	case strings.HasPrefix(candidate, "git@") && strings.Contains(candidate, ":"):
		remainder := strings.TrimPrefix(candidate, "git@")
		parts := strings.SplitN(remainder, ":", 2)
		if len(parts) == 2 {
			host = parts[0]
			path = parts[1]
		}
	case strings.HasPrefix(candidate, "ssh://"), strings.Contains(candidate, "://"):
		parsed, err := url.Parse(candidate)
		if err == nil {
			host = parsed.Hostname()
			path = parsed.Path
		}
	}

	if host == "" || path == "" {
		return strings.TrimRight(candidate, "/")
	}

	cleanPath := strings.Trim(path, "/")
	cleanPath = strings.TrimSuffix(cleanPath, ".git")
	cleanPath = strings.ToLower(strings.Join(strings.FieldsFunc(cleanPath, func(r rune) bool {
		return r == '/'
	}), "/"))
	if cleanPath == "" {
		return ""
	}

	return fmt.Sprintf("https://%s/%s", strings.ToLower(host), cleanPath)
}

// NormalizedRemoteKey returns the canonical host/path key for a git remote URL.
// It is an aggregation of NormalizeRemoteURL with two extra input classes: the
// git+ prefix (npm-style git dependency URLs) and SCP syntax with any user@
// prefix (not only git@). The result is a lowercase host/path key with the .git
// suffix dropped. Ports are stripped (matching the git collector's identity
// hashing).
//
// The result is idempotent when composed with NormalizeRemoteURL:
//
//	NormalizedRemoteKey(NormalizeRemoteURL(x)) == NormalizedRemoteKey(x)
//
// for every input x that NormalizeRemoteURL accepts.
//
// Empty input, unparseable input, bare-host URLs, and any input whose extracted
// host segment contains %, @, or spaces all produce "".
func NormalizedRemoteKey(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}

	// Strip the git+ prefix that npm-style git dependency URLs carry.
	trimmed = strings.TrimPrefix(trimmed, "git+")
	if trimmed == "" {
		return ""
	}

	// Handle SCP syntax (user@host:path) for forms that do not contain ://.
	// This covers both git@host:path (already handled by NormalizeRemoteURL
	// at the canonical-URL level) and user@host:path (any username prefix).
	if !strings.Contains(trimmed, "://") {
		return scpKey(trimmed)
	}

	// URL-shaped inputs: delegate to NormalizeRemoteURL, then extract the
	// host/path key from the canonical https:// result.
	normalized := NormalizeRemoteURL(trimmed)
	if normalized == "" {
		return ""
	}
	// Re-parse the normalized URL to reject control characters, bad
	// percent-encoding, and hostless passthrough from NormalizeRemoteURL.
	// This closes the leak class where NormalizeRemoteURL passes through
	// garbage untouched because it starts with "https://".
	//
	// NormalizeRemoteURL strips brackets from IPv6 hosts (e.g. [::1]→::1);
	// url.Parse rejects bare "::1" as an invalid port. Re-bracket IPv6
	// hosts before the validation parse so legitimate IPv6 keys are not
	// rejected.
	reparseURL := normalized
	if key := strings.TrimPrefix(normalized, "https://"); key != normalized {
		if host, _, hasSlash := strings.Cut(key, "/"); hasSlash && strings.Contains(host, ":") {
			// Bare : in the host means IPv6 (ports are already stripped
			// by NormalizeRemoteURL's Hostname()).
			reparseURL = "https://[" + host + "]" + key[len(host):]
		}
	}
	if _, err := url.Parse(reparseURL); err != nil {
		return ""
	}
	key := strings.TrimPrefix(normalized, "https://")
	if key == normalized || key == "" {
		return ""
	}
	// Reject keys whose extracted host segment is malformed.
	if !isValidRemoteKey(key) {
		return ""
	}
	return key
}

// isValidRemoteKey reports whether key has the form host/path with a non-empty
// host segment free of %, @, and spaces. The host segment is validated strictly
// because % (un-decoded percent), @ (residual userinfo), and spaces are never
// legitimate in a hostname. The path segment is not validated beyond non-emptiness;
// url.Parse decodes percent-encoding (e.g. %20 → space, %C3%A9 → é) into the
// path, so spaces and non-ASCII in the path are legitimate decoded representations.
func isValidRemoteKey(key string) bool {
	if key == "" {
		return false
	}
	host, _, hasSlash := strings.Cut(key, "/")
	return hasSlash && host != "" && !strings.ContainsAny(host, "%@ ")
}

// scpKey extracts a host/path key from SCP-style syntax (user@host:path).
// It lowercases the host and path, strips the .git suffix, and drops the
// user@ prefix. Empty or malformed input produces "".
func scpKey(raw string) string {
	beforeColon, afterColon, ok := strings.Cut(raw, ":")
	if !ok || strings.TrimSpace(afterColon) == "" {
		return ""
	}
	host := beforeColon
	if at := strings.LastIndex(host, "@"); at >= 0 && at < len(host)-1 {
		host = host[at+1:]
	}
	host = strings.ToLower(strings.TrimSpace(host))
	pathValue := strings.Trim(strings.TrimSpace(afterColon), "/")
	pathValue = strings.TrimSuffix(pathValue, ".git")
	if host == "" || pathValue == "" {
		return ""
	}
	key := host + "/" + strings.ToLower(pathValue)

	// Re-parse as https:// URL to reject control characters and bad
	// percent-encoding in the resulting key.
	if _, err := url.Parse("https://" + key); err != nil {
		return ""
	}
	if !isValidRemoteKey(key) {
		return ""
	}
	return key
}

// RepoSlugFromRemoteURL returns the org/repo slug derived from a remote URL.
func RepoSlugFromRemoteURL(remoteURL string) string {
	normalized := NormalizeRemoteURL(remoteURL)
	if normalized == "" {
		return ""
	}
	parsed, err := url.Parse(normalized)
	if err != nil {
		return ""
	}
	return strings.Trim(parsed.Path, "/")
}

// CanonicalRepositoryID returns the canonical repository identifier.
func CanonicalRepositoryID(remoteURL string, localPath string) (string, error) {
	identity := NormalizeRemoteURL(remoteURL)
	if identity == "" {
		if strings.TrimSpace(localPath) == "" {
			return "", fmt.Errorf("local path is required when remote url is not available")
		}
		identity = localPath
	}

	sum := sha1.Sum([]byte(identity)) // #nosec G401 -- non-cryptographic repository identity digest, not a security primitive
	return fmt.Sprintf("repository:r_%s", hex.EncodeToString(sum[:])[:8]), nil
}
