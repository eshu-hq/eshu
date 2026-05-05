package repositoryidentity

import (
	"crypto/sha1"
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

	sum := sha1.Sum([]byte(identity))
	return fmt.Sprintf("repository:r_%s", hex.EncodeToString(sum[:])[:8]), nil
}
