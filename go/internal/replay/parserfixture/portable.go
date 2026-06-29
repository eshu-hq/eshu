// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parserfixture

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// repoRootSentinel is the placeholder a committed parser fixture stores in place
// of the absolute repository root. Parser file facts carry absolute provenance
// (SourceURI) and absolute payload paths (the parser stamps the file's absolute
// path into its payload), so a fixture recorded on one machine would otherwise
// embed that machine's checkout path and fail to replay byte-identically on
// another. Tokenizing the root keeps committed fixtures portable while the
// replay side rehydrates them against the local checkout so provenance is exact.
//
// The sentinel is chosen to never collide with real path content: no filesystem
// path or parser payload value contains the literal "{{REPO_ROOT}}".
const repoRootSentinel = "{{REPO_ROOT}}"

// cleanRoot normalizes a repository root for prefix substitution: it cleans the
// path and strips any trailing separator so the sentinel substitution leaves the
// separator that joins root and relative path intact (root "/a/b" + "/c" stays
// "{{REPO_ROOT}}/c", not "{{REPO_ROOT}}c").
func cleanRoot(repoRoot string) (string, error) {
	trimmed := strings.TrimSpace(repoRoot)
	if trimmed == "" {
		return "", fmt.Errorf("parserfixture: repo root is empty; a portable fixture needs an absolute repository root")
	}
	root := strings.TrimRight(filepath.Clean(trimmed), string(filepath.Separator))
	if root == "" || root == "." {
		return "", fmt.Errorf("parserfixture: repo root %q is empty after cleaning; a portable fixture needs an absolute repository root", repoRoot)
	}
	return root, nil
}

// portableize replaces every occurrence of the absolute repository root in
// canonical fixture bytes with repoRootSentinel, making the committed artifact
// machine-independent. It returns an error if any occurrence of the raw root
// survives (it never should after ReplaceAll), which would mean the committed
// fixture still leaks a checkout path. Substitution is byte-level on the
// canonical JSON so it covers provenance (SourceURI) and every absolute path the
// parser embedded in its payload uniformly, regardless of where they appear.
func portableize(canonical []byte, repoRoot string) ([]byte, error) {
	root, err := cleanRoot(repoRoot)
	if err != nil {
		return nil, err
	}
	out := bytes.ReplaceAll(canonical, []byte(root), []byte(repoRootSentinel))
	if bytes.Contains(out, []byte(root)) {
		return nil, fmt.Errorf("parserfixture: fixture still contains the repository root %q after portableizing; the recorded tree is not under the repo root", root)
	}
	return out, nil
}

// rehydrate replaces repoRootSentinel in fixture bytes with the local absolute
// repository root, reversing portableize so a committed fixture replays with the
// exact absolute provenance and payload paths the live parser produces on this
// machine. A fixture that carries no sentinel (an absolute-path temp-dir
// recording) is returned unchanged, so rehydrate is safe to apply unconditionally.
func rehydrate(data []byte, repoRoot string) ([]byte, error) {
	if !bytes.Contains(data, []byte(repoRootSentinel)) {
		return data, nil
	}
	root, err := cleanRoot(repoRoot)
	if err != nil {
		return nil, err
	}
	return bytes.ReplaceAll(data, []byte(repoRootSentinel), []byte(root)), nil
}

// errNoRepoRoot is returned when a rehydrating load is asked for without a root.
var errNoRepoRoot = errors.New("parserfixture: repo root is required to rehydrate a portable fixture")
