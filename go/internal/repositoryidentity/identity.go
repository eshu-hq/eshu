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
	// For git@-prefixed SCP forms that NormalizeRemoteURL handles natively,
	// route through it directly so the key matches the collector's canonical
	// identity. For other SCP user-prefix forms, construct the https://
	// equivalent and canonicalize through the NormalizeRemoteURL URL path.
	if !strings.Contains(trimmed, "://") {
		return scpNormalizedKey(trimmed)
	}

	// URL-shaped inputs: delegate to NormalizeRemoteURL, then extract the
	// host/path key from the canonical https:// result.
	normalized := NormalizeRemoteURL(trimmed)
	if normalized == "" {
		return ""
	}
	return keyFromNormalizedURL(normalized)
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

// scpNormalizedKey returns a host/path key for SCP-style input.
// For git@-prefixed forms that NormalizeRemoteURL handles natively, it
// delegates to NormalizeRemoteURL directly — the same path the git collector
// uses for identity hashing. For other SCP forms (non-git@ user prefix), it
// constructs the https:// equivalent and canonicalizes through the
// NormalizeRemoteURL URL path, then extracts and validates the key.
func scpNormalizedKey(raw string) string {
	beforeColon, afterColon, ok := strings.Cut(raw, ":")
	if !ok || strings.TrimSpace(afterColon) == "" {
		return ""
	}

	// git@ SCP forms: route through NormalizeRemoteURL directly.
	// The collector hashes NormalizeRemoteURL(raw) for identity, so
	// this key matches.
	if strings.HasPrefix(raw, "git@") {
		normalized := NormalizeRemoteURL(raw)
		if normalized == "" || !strings.HasPrefix(normalized, "https://") {
			return ""
		}
		return keyFromNormalizedURL(normalized)
	}

	// Non-git@ SCP forms: extract host and path, construct the https://
	// equivalent, and canonicalize through the NormalizeRemoteURL URL path
	// (which uses url.Parse for FieldsFunc collapse, percent handling, etc.).
	host := beforeColon
	if at := strings.LastIndex(host, "@"); at >= 0 && at < len(host)-1 {
		host = host[at+1:]
	}
	host = strings.ToLower(strings.TrimSpace(host))
	pathValue := strings.TrimSpace(afterColon)
	if host == "" || pathValue == "" {
		return ""
	}
	normalized := NormalizeRemoteURL("https://" + host + "/" + pathValue)
	if normalized == "" || !strings.HasPrefix(normalized, "https://") {
		return ""
	}
	return keyFromNormalizedURL(normalized)
}

// keyFromNormalizedURL extracts and validates a host/path key from a
// NormalizeRemoteURL-produced https:// URL. It applies the re-parse
// guard (with IPv6 re-bracketing) and host-segment validation.
func keyFromNormalizedURL(normalized string) string {
	if normalized == "" || !strings.HasPrefix(normalized, "https://") {
		return ""
	}

	// Re-parse to reject control characters, bad percent-encoding,
	// and hostless passthrough.
	reparseURL := normalized
	if key := strings.TrimPrefix(normalized, "https://"); key != normalized {
		if h, _, hasSlash := strings.Cut(key, "/"); hasSlash && strings.Contains(h, ":") {
			reparseURL = "https://[" + h + "]" + key[len(h):]
		}
	}
	if _, err := url.Parse(reparseURL); err != nil {
		return ""
	}

	key := strings.TrimPrefix(normalized, "https://")
	if key == normalized || key == "" {
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
