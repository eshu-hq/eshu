// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tesseract

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	// versionTimeout bounds the one --version run in New.
	versionTimeout = 10 * time.Second
	// maxVersionOutput caps the --version stdout New reads.
	maxVersionOutput = 4 << 10
)

// pinnedIdentity is the file identity captured when a pinned file was
// hashed, re-checked on every call.
type pinnedIdentity struct {
	path string
	info os.FileInfo
}

// installationPins is the verified result of hashing every pinned file.
type installationPins struct {
	binaryDigest  string
	libraryDigest string
	modelDigest   string
	identities    []pinnedIdentity
}

// verifyInstallation hashes the binary, every library, and the model, and
// compares each with its pin. The library digest is SHA-256 over the sorted
// lines "<path> <hex>\n", so it does not depend on configuration order and
// scripts/prove-ocr-sandbox.sh can reproduce it with sha256sum and sort.
func verifyInstallation(cfg Config) (installationPins, error) {
	pins := installationPins{}
	binaryDigest, err := normalizeDigest(cfg.BinarySHA256)
	if err != nil {
		return pins, failure(OutcomeConfigInvalid, "binary digest", err)
	}
	modelDigest, err := normalizeDigest(cfg.ModelSHA256)
	if err != nil {
		return pins, failure(OutcomeConfigInvalid, "model digest", err)
	}
	if len(cfg.Libraries) == 0 {
		return pins, failure(OutcomeConfigInvalid, "at least one pinned runtime library is required", nil)
	}
	binaryInfo, err := verifyFile(cfg.BinaryPath, binaryDigest, true)
	if err != nil {
		return pins, err
	}
	pins.identities = append(pins.identities, pinnedIdentity{cfg.BinaryPath, binaryInfo})
	lines := make([]string, 0, len(cfg.Libraries))
	seen := map[string]bool{}
	for _, lib := range cfg.Libraries {
		path := filepath.Clean(lib.Path)
		if !filepath.IsAbs(path) || seen[path] {
			return pins, failure(OutcomeConfigInvalid, "library paths must be absolute and unique", nil)
		}
		seen[path] = true
		digest, err := normalizeDigest(lib.SHA256)
		if err != nil {
			return pins, failure(OutcomeConfigInvalid, "library digest for "+filepath.Base(path), err)
		}
		info, err := verifyFile(path, digest, false)
		if err != nil {
			return pins, err
		}
		pins.identities = append(pins.identities, pinnedIdentity{path, info})
		lines = append(lines, path+" "+digest+"\n")
	}
	sort.Strings(lines)
	libSum := sha256.Sum256([]byte(strings.Join(lines, "")))
	modelPath := filepath.Join(cfg.TessdataDir, cfg.Language+".traineddata")
	modelInfo, err := verifyFile(modelPath, modelDigest, false)
	if err != nil {
		return pins, err
	}
	pins.identities = append(pins.identities, pinnedIdentity{modelPath, modelInfo})
	pins.binaryDigest = binaryDigest
	pins.libraryDigest = hex.EncodeToString(libSum[:])
	pins.modelDigest = modelDigest
	return pins, nil
}

func normalizeDigest(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "sha256:")
	if len(value) != sha256.Size*2 {
		return "", fmt.Errorf("want %d hex characters", sha256.Size*2)
	}
	if _, err := hex.DecodeString(value); err != nil {
		return "", errors.New("not hex")
	}
	return value, nil
}

// verifyFile hashes a regular file and compares it with the pinned digest.
func verifyFile(path, want string, executable bool) (os.FileInfo, error) {
	file, err := os.Open(path) // #nosec G304 -- operator-configured, digest-pinned engine file
	if err != nil {
		return nil, failure(OutcomeConfigInvalid, "open pinned file", err)
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return nil, failure(OutcomeConfigInvalid, "stat pinned file", err)
	}
	if !info.Mode().IsRegular() {
		return nil, failure(OutcomeConfigInvalid, "pinned file is not a regular file", nil)
	}
	if executable && info.Mode().Perm()&0o111 == 0 {
		return nil, failure(OutcomeConfigInvalid, "binary is not executable", nil)
	}
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return nil, failure(OutcomeConfigInvalid, "hash pinned file", err)
	}
	if got := hex.EncodeToString(hasher.Sum(nil)); got != want {
		return nil, failure(OutcomeDigestMismatch, fmt.Sprintf("%s has sha256:%s", filepath.Base(path), got), nil)
	}
	return info, nil
}

// verifyReportedVersion runs "<binary> --version" once, bounded by time and
// output size with an empty environment, and requires its first line to be
// "tesseract <release>". The operator-supplied release is then a checked
// claim, not just a label.
func verifyReportedVersion(binary, release string) error {
	ctx, cancel := context.WithTimeout(context.Background(), versionTimeout)
	defer cancel()
	var stdout bytes.Buffer
	cmd := exec.CommandContext(ctx, binary, "--version") // #nosec G204 -- digest-pinned absolute binary with a fixed argument
	cmd.Env = []string{"LC_ALL=C"}
	cmd.Dir = "/"
	cmd.Stdout = &limitedWriter{buf: &stdout, limit: maxVersionOutput}
	cmd.WaitDelay = waitDelay
	if err := cmd.Run(); err != nil {
		return failure(OutcomeConfigInvalid, "run --version", err)
	}
	first, _, _ := bufio.NewReader(&stdout).ReadLine()
	reported := strings.TrimSpace(string(first))
	if reported != "tesseract "+release {
		if len(reported) > 64 {
			reported = reported[:64]
		}
		return failure(OutcomeConfigInvalid, fmt.Sprintf("binary reports version %q, configured %q", reported, release), nil)
	}
	return nil
}

// limitedWriter keeps the first limit bytes and drops the rest.
type limitedWriter struct {
	buf   *bytes.Buffer
	limit int
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	if room := w.limit - w.buf.Len(); room > 0 {
		w.buf.Write(p[:min(room, len(p))])
	}
	return len(p), nil
}

// unchangedSinceConstruction re-stats every pinned file and rejects a swap.
// It compares identity, size, and modification time, which is cheap enough
// for every call; a full re-hash happens only at construction. The engine
// execs by path, not through the verified descriptor: the loader maps the
// libraries by path regardless, and an in-place write to the held inode
// would defeat fexecve too. The sandbox's read-only root filesystem is what
// prevents in-place modification.
func (e *Engine) unchangedSinceConstruction() error {
	for _, pinned := range e.pinned {
		current, err := os.Stat(pinned.path)
		if err != nil {
			return failure(OutcomeDigestMismatch, "pinned file missing since construction", err)
		}
		if !os.SameFile(current, pinned.info) || current.Size() != pinned.info.Size() ||
			!current.ModTime().Equal(pinned.info.ModTime()) {
			return failure(OutcomeDigestMismatch, filepath.Base(pinned.path)+" changed since construction", nil)
		}
	}
	return nil
}
