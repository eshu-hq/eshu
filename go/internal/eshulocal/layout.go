package eshulocal

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const eshuHomeEnvVar = "ESHU_HOME"

// Layout describes the per-workspace local-host filesystem contract.
type Layout struct {
	HomeDir         string
	WorkspaceRoot   string
	WorkspaceID     string
	RootDir         string
	VersionPath     string
	OwnerLockPath   string
	OwnerRecordPath string
	GraphDir        string
	PostgresDir     string
	LogsDir         string
	CacheDir        string
}

// ResolveHomeDir resolves the per-user Eshu home root.
func ResolveHomeDir(getenv func(string) string, userHomeDir func() (string, error), goos string) (string, error) {
	if value := strings.TrimSpace(getenv(eshuHomeEnvVar)); value != "" {
		return expandHome(value, userHomeDir)
	}

	switch goos {
	case "darwin":
		home, err := userHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve user home: %w", err)
		}
		return filepath.Join(home, "Library", "Application Support", "eshu"), nil
	case "windows":
		if value := strings.TrimSpace(getenv("LOCALAPPDATA")); value != "" {
			return filepath.Join(value, "eshu"), nil
		}
		home, err := userHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve user home: %w", err)
		}
		return filepath.Join(home, "AppData", "Local", "eshu"), nil
	default:
		if value := strings.TrimSpace(getenv("XDG_DATA_HOME")); value != "" {
			expanded, err := expandHome(value, userHomeDir)
			if err != nil {
				return "", err
			}
			return filepath.Join(expanded, "eshu"), nil
		}
		home, err := userHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve user home: %w", err)
		}
		return filepath.Join(home, ".local", "share", "eshu"), nil
	}
}

// ResolveWorkspaceRoot resolves the canonical workspace root for a local host.
func ResolveWorkspaceRoot(startPath, explicitRoot string) (string, error) {
	if explicitRoot != "" {
		return resolveExistingDir(explicitRoot)
	}

	startDir, err := resolveExistingDir(startPath)
	if err != nil {
		return "", err
	}

	if root, ok, err := findAncestorWithMarker(startDir, ".eshu.yaml"); err != nil {
		return "", err
	} else if ok {
		return root, nil
	}

	if root, ok, err := findAncestorWithMarker(startDir, ".git"); err != nil {
		return "", err
	} else if ok {
		return root, nil
	}

	return startDir, nil
}

// WorkspaceID derives a stable identifier for a workspace root.
func WorkspaceID(workspaceRoot string) (string, error) {
	root, err := resolveExistingDir(workspaceRoot)
	if err != nil {
		return "", err
	}

	normalized := filepath.ToSlash(root)
	caseInsensitive, err := isCaseInsensitivePath(root)
	if err != nil {
		return "", fmt.Errorf("detect filesystem case sensitivity: %w", err)
	}
	if caseInsensitive {
		normalized = strings.ToLower(normalized)
	}

	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:20]), nil
}

// BuildLayout resolves the filesystem layout for a workspace root.
func BuildLayout(getenv func(string) string, userHomeDir func() (string, error), goos, workspaceRoot string) (Layout, error) {
	homeDir, err := ResolveHomeDir(getenv, userHomeDir, goos)
	if err != nil {
		return Layout{}, err
	}

	root, err := resolveExistingDir(workspaceRoot)
	if err != nil {
		return Layout{}, err
	}

	workspaceID, err := WorkspaceID(root)
	if err != nil {
		return Layout{}, err
	}

	layoutRoot := filepath.Join(homeDir, "local", "workspaces", workspaceID)
	return Layout{
		HomeDir:         homeDir,
		WorkspaceRoot:   root,
		WorkspaceID:     workspaceID,
		RootDir:         layoutRoot,
		VersionPath:     filepath.Join(layoutRoot, "VERSION"),
		OwnerLockPath:   filepath.Join(layoutRoot, "owner.lock"),
		OwnerRecordPath: filepath.Join(layoutRoot, "owner.json"),
		GraphDir:        filepath.Join(layoutRoot, "graph"),
		PostgresDir:     filepath.Join(layoutRoot, "postgres"),
		LogsDir:         filepath.Join(layoutRoot, "logs"),
		CacheDir:        filepath.Join(layoutRoot, "cache"),
	}, nil
}

func resolveExistingDir(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		path = "."
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve absolute path for %q: %w", path, err)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return "", fmt.Errorf("stat %q: %w", absPath, err)
	}
	if !info.IsDir() {
		absPath = filepath.Dir(absPath)
	}

	resolved, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return "", fmt.Errorf("resolve symlinks for %q: %w", absPath, err)
	}
	return resolved, nil
}

func findAncestorWithMarker(startDir, marker string) (string, bool, error) {
	current := startDir
	for {
		markerPath := filepath.Join(current, marker)
		if _, err := os.Stat(markerPath); err == nil {
			return current, true, nil
		} else if !os.IsNotExist(err) {
			return "", false, fmt.Errorf("stat marker %q: %w", markerPath, err)
		}

		parent := filepath.Dir(current)
		if parent == current {
			return "", false, nil
		}
		current = parent
	}
}

func expandHome(path string, userHomeDir func() (string, error)) (string, error) {
	if !strings.HasPrefix(path, "~") {
		return path, nil
	}

	home, err := userHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home: %w", err)
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~")), nil
}

func isCaseInsensitivePath(path string) (bool, error) {
	realPath, err := resolveExistingDir(path)
	if err != nil {
		return false, err
	}

	components := splitPath(realPath)
	for i := len(components) - 1; i >= 0; i-- {
		alternate, ok := toggleFirstLetterCase(components[i])
		if !ok {
			continue
		}

		alternateComponents := append([]string(nil), components...)
		alternateComponents[i] = alternate
		alternatePath := joinPath(alternateComponents)

		originalInfo, err := os.Stat(realPath)
		if err != nil {
			return false, err
		}
		alternateInfo, err := os.Stat(alternatePath)
		if err != nil {
			if os.IsNotExist(err) {
				return false, nil
			}
			return false, err
		}
		return os.SameFile(originalInfo, alternateInfo), nil
	}

	return runtime.GOOS == "windows", nil
}

func splitPath(path string) []string {
	volume := filepath.VolumeName(path)
	trimmed := strings.TrimPrefix(path, volume)
	trimmed = strings.TrimPrefix(trimmed, string(filepath.Separator))
	if trimmed == "" {
		if volume == "" {
			return nil
		}
		return []string{volume}
	}

	parts := strings.Split(trimmed, string(filepath.Separator))
	if volume == "" {
		return parts
	}
	return append([]string{volume}, parts...)
}

func joinPath(parts []string) string {
	if len(parts) == 0 {
		return string(filepath.Separator)
	}

	if len(parts) == 1 && strings.HasSuffix(parts[0], ":") {
		return parts[0] + string(filepath.Separator)
	}

	if strings.HasSuffix(parts[0], ":") {
		return filepath.Join(parts[0]+string(filepath.Separator), filepath.Join(parts[1:]...))
	}

	return string(filepath.Separator) + filepath.Join(parts...)
}

func toggleFirstLetterCase(value string) (string, bool) {
	for i, r := range value {
		switch {
		case 'a' <= r && r <= 'z':
			return value[:i] + strings.ToUpper(string(r)) + value[i+len(string(r)):], true
		case 'A' <= r && r <= 'Z':
			return value[:i] + strings.ToLower(string(r)) + value[i+len(string(r)):], true
		}
	}
	return "", false
}
