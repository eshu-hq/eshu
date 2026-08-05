// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"bytes"
	"context"
	"crypto/sha1" // #nosec G505 -- Git SHA-1 object identity compatibility, not a security primitive
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type managedCopyBlobExpectation struct {
	objectID string
	mode     string
	kind     string
}

type managedCopyCommitExpectation struct {
	blobs     map[string]managedCopyBlobExpectation
	remaining map[string]struct{}
	newHash   func() hash.Hash
	invalid   bool
}

// loadManagedCopyCommitExpectation loads the immutable blob identities for the
// clean commit observed immediately before a managed copy. Failure returns nil
// so collection continues without commit attribution.
func loadManagedCopyCommitExpectation(
	ctx context.Context,
	sourcePath string,
	commitSHA string,
) *managedCopyCommitExpectation {
	commitSHA = strings.TrimSpace(commitSHA)
	var newHash func() hash.Hash
	switch len(commitSHA) {
	case sha1.Size * 2:
		newHash = sha1.New // #nosec G401 -- required to reproduce Git SHA-1 blob identities
	case sha256.Size * 2:
		newHash = sha256.New
	default:
		return nil
	}
	output, err := exec.CommandContext(
		ctx,
		"git",
		"-C",
		sourcePath,
		"ls-tree",
		"-rz",
		"--full-tree",
		commitSHA,
	).Output() // #nosec G204 -- fixed Git query over an internally resolved repository and validated object identity
	if err != nil {
		return nil
	}
	blobs := make(map[string]managedCopyBlobExpectation)
	remaining := make(map[string]struct{})
	for _, entry := range bytes.Split(output, []byte{0}) {
		metadata, path, ok := bytes.Cut(entry, []byte{'\t'})
		if !ok {
			continue
		}
		fields := bytes.Fields(metadata)
		if len(fields) != 3 ||
			(!bytes.Equal(fields[1], []byte("blob")) && !bytes.Equal(fields[1], []byte("commit"))) {
			continue
		}
		objectID := strings.ToLower(strings.TrimSpace(string(fields[2])))
		if len(objectID) != len(commitSHA) {
			return nil
		}
		if _, err := hex.DecodeString(objectID); err != nil {
			return nil
		}
		relativePath := string(path)
		blobs[relativePath] = managedCopyBlobExpectation{
			objectID: objectID,
			mode:     string(fields[0]),
			kind:     string(fields[1]),
		}
		remaining[relativePath] = struct{}{}
	}
	return &managedCopyCommitExpectation{blobs: blobs, remaining: remaining, newHash: newHash}
}

// copyManagedRepositoryFile writes the live bytes once while hashing those
// same bytes as a Git blob. Clean-filter, encoding, and line-ending transforms
// fail attribution because validating them through Git could execute an
// untrusted repository-configured filter command.
func copyManagedRepositoryFile(
	source io.Reader,
	target io.Writer,
	relativePath string,
	expectedSize int64,
	expectation *managedCopyCommitExpectation,
) (bool, error) {
	expected, tracked := managedCopyBlobExpectation{}, false
	if expectation != nil {
		expected, tracked = expectation.blobs[relativePath]
	}
	if expectation == nil || !tracked {
		_, err := io.Copy(target, source)
		if expectation != nil {
			expectation.invalid = true
		}
		return expectation == nil, err
	}
	if expected.kind != "blob" || !strings.HasPrefix(expected.mode, "100") {
		_, err := io.Copy(target, source)
		expectation.invalid = true
		return false, err
	}
	objectID, written, err := hashManagedCopyGitBlob(source, target, expectedSize, expectation.newHash)
	if err != nil {
		return false, err
	}
	delete(expectation.remaining, relativePath)
	if written != expectedSize {
		return false, nil
	}
	return objectID == expected.objectID, nil
}

func hashManagedCopyGitBlob(
	source io.Reader,
	target io.Writer,
	expectedSize int64,
	newHash func() hash.Hash,
) (string, int64, error) {
	hasher := newHash()
	header := "blob " + strconv.FormatInt(expectedSize, 10) + "\x00"
	if _, err := io.WriteString(hasher, header); err != nil {
		return "", 0, err
	}
	writer := io.Writer(hasher)
	if target != nil {
		writer = io.MultiWriter(target, hasher)
	}
	written, err := io.Copy(writer, source)
	if err != nil {
		return "", written, err
	}
	return hex.EncodeToString(hasher.Sum(nil)), written, nil
}

// bindEshuignoreControl reads and parses one directory's local policy exactly
// once. Commit-owned controls must match their immutable blob; ignored local
// controls remain authoritative operator policy without entering the copy.
func (e *managedCopyCommitExpectation) bindEshuignoreControl(
	sourceRoot string,
	directoryPath string,
	cache map[string]*collectorGitignoreSpec,
) (bool, error) {
	fullPath := filepath.Clean(filepath.Join(directoryPath, ".eshuignore"))
	relativePath, err := filepath.Rel(sourceRoot, fullPath)
	if err != nil {
		e.invalidate()
		return false, fmt.Errorf("resolve .eshuignore control path: %w", err)
	}
	relativePath = filepath.ToSlash(relativePath)
	expected, tracked := managedCopyBlobExpectation{}, false
	if e != nil {
		expected, tracked = e.blobs[relativePath]
	}
	info, err := os.Lstat(fullPath)
	if err != nil {
		cache[fullPath] = nil
		if os.IsNotExist(err) {
			if tracked {
				e.invalid = true
				return false, nil
			}
			return true, nil
		}
		e.invalidate()
		return false, fmt.Errorf("inspect .eshuignore control %q: %w", fullPath, err)
	}
	if !info.Mode().IsRegular() {
		e.invalidate()
		return false, fmt.Errorf(".eshuignore control %q must be a regular file", fullPath)
	}
	contents, err := os.ReadFile(fullPath) // #nosec G304 -- reads only the directory's operator-owned .eshuignore control
	if err != nil {
		cache[fullPath] = nil
		e.invalidate()
		return false, fmt.Errorf("read .eshuignore control %q: %w", fullPath, err)
	}
	cache[fullPath] = parseCollectorGitignoreSpec(strings.Split(string(contents), "\n"))
	if e == nil || !tracked {
		return true, nil
	}
	objectID, written, err := hashManagedCopyGitBlob(
		bytes.NewReader(contents), nil, int64(len(contents)), e.newHash,
	)
	if err != nil || written != int64(len(contents)) || expected.kind != "blob" ||
		!strings.HasPrefix(expected.mode, "100") || objectID != expected.objectID {
		e.invalid = true
		return false, nil
	}
	return true, nil
}

// dischargeSkippedPath accounts for immutable entries omitted by an observed
// deterministic copy-policy decision without opening operator-excluded data.
func (e *managedCopyCommitExpectation) dischargeSkippedPath(
	relativePath string,
	isDir bool,
) {
	if e == nil {
		return
	}
	if !isDir {
		delete(e.remaining, relativePath)
		return
	}
	prefix := strings.TrimSuffix(filepath.ToSlash(relativePath), "/") + "/"
	for expectedPath := range e.remaining {
		if expectedPath == relativePath || (isDir && strings.HasPrefix(expectedPath, prefix)) {
			delete(e.remaining, expectedPath)
		}
	}
}

func (e *managedCopyCommitExpectation) dischargeSymlink(relativePath string) bool {
	if e == nil {
		return true
	}
	expected, tracked := e.blobs[relativePath]
	if !tracked {
		return true
	}
	if expected.kind != "blob" || expected.mode != "120000" {
		e.invalid = true
		return false
	}
	delete(e.remaining, relativePath)
	return true
}

func (e *managedCopyCommitExpectation) complete() bool {
	return e != nil && !e.invalid && len(e.remaining) == 0
}

func copyRepositoryFile(
	sourcePath string,
	targetPath string,
	relativePath string,
	expectation *managedCopyCommitExpectation,
) (bool, error) {
	sourceFile, err := os.Open(sourcePath) // #nosec G304 -- reads indexed repo file at an internally-constructed source path during filesystem copy, not user-supplied input
	if err != nil {
		return false, err
	}
	defer func() {
		_ = sourceFile.Close()
	}()

	info, err := sourceFile.Stat()
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return expectation == nil, nil
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o750); err != nil { // #nosec G301 -- internal managed workspace directory
		return false, err
	}
	targetFile, err := os.Create(targetPath) // #nosec G304 -- creates file at an internally-constructed target path during filesystem copy, not user-supplied input
	if err != nil {
		return false, err
	}
	defer func() {
		_ = targetFile.Close()
	}()
	fileMatchesCommit, err := copyManagedRepositoryFile(
		sourceFile,
		targetFile,
		filepath.ToSlash(relativePath),
		info.Size(),
		expectation,
	)
	if err != nil {
		return false, err
	}
	if err := targetFile.Chmod(0o644); err != nil {
		return false, err
	}
	return fileMatchesCommit, nil
}

func (e *managedCopyCommitExpectation) invalidate() {
	if e != nil {
		e.invalid = true
	}
}
