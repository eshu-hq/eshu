// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tesseract

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// EngineName identifies this backend in OCR region and section metadata.
const EngineName = "tesseract"

// Defaults follow the Image/OCR column of design 1737 section 6. A caller may
// lower any cap; raising one needs a design update.
const (
	DefaultMaxInputBytes  = 25 << 20
	DefaultMaxPixels      = 50_000_000
	DefaultTimeout        = 30 * time.Second
	DefaultMaxRegions     = 500
	DefaultMaxOutputBytes = 8 << 20
	DefaultMaxStderrBytes = 64 << 10
	// DefaultMaxAddressSpaceBytes is the RLIMIT_AS applied to each child.
	// tesseract 5.5.0 with eng.traineddata peaks near 80 MiB RSS on a
	// 900x200 screenshot; virtual size runs higher, so the cap leaves room.
	DefaultMaxAddressSpaceBytes = 2 << 30
	DefaultLanguage             = "eng"
	DefaultPageSegMode          = 3
)

// Config pins and bounds one tesseract installation. BinaryPath,
// BinarySHA256, TessdataDir, ModelSHA256, and EngineVersion are required;
// non-positive caps take the package defaults.
type Config struct {
	// BinaryPath is the absolute path of the tesseract executable. It is
	// executed directly, never through a shell or PATH lookup.
	BinaryPath string
	// BinarySHA256 is the pinned hex SHA-256 of BinaryPath, optionally
	// prefixed with "sha256:". Construction fails on a mismatch.
	BinarySHA256 string
	// TessdataDir holds <Language>.traineddata.
	TessdataDir string
	// ModelSHA256 is the pinned hex SHA-256 of <Language>.traineddata.
	ModelSHA256 string
	// EngineVersion is the reviewed tesseract release string, such as
	// "5.5.0". It is recorded with both digests in region metadata.
	EngineVersion string
	// Language is the single traineddata name; only "eng" is reviewed.
	Language string
	// PageSegMode is the tesseract --psm value; 3 is fully automatic.
	PageSegMode int
	// ScratchDir is the parent of the private per-run working directory.
	// Empty uses os.TempDir(). In a read-only sandbox it must be a
	// writable, noexec tmpfs.
	ScratchDir string

	MaxInputBytes  int64
	MaxPixels      int64
	Timeout        time.Duration
	MaxRegions     int
	MaxOutputBytes int64
	MaxStderrBytes int64
	// MaxAddressSpaceBytes is the RLIMIT_AS set on each child on Linux,
	// applied just after exec. An allocation past it fails inside tesseract,
	// which then exits non-zero or by signal. The sandbox cgroup memory
	// limit stays the authoritative ceiling.
	MaxAddressSpaceBytes int64

	// Instruments, Tracer, and Logger are optional; nil disables that signal.
	Instruments *telemetry.Instruments
	Tracer      trace.Tracer
	Logger      *slog.Logger
}

// Engine runs a pinned tesseract binary out of process behind ocr.Engine.
// It is safe for concurrent use; each Recognize call starts its own process
// with its own scratch directory.
type Engine struct {
	cfg        Config
	version    string
	binaryInfo os.FileInfo
	modelPath  string
	modelInfo  os.FileInfo
	args       []string
	execCount  atomic.Int64
}

// New validates cfg, verifies both pinned digests, and returns an Engine.
// Errors carry OutcomeConfigInvalid or OutcomeDigestMismatch.
func New(cfg Config) (*Engine, error) {
	cfg = withDefaults(cfg)
	binaryDigest, err := normalizeDigest(cfg.BinarySHA256)
	if err != nil {
		return nil, failure(OutcomeConfigInvalid, "binary digest", err)
	}
	modelDigest, err := normalizeDigest(cfg.ModelSHA256)
	if err != nil {
		return nil, failure(OutcomeConfigInvalid, "model digest", err)
	}
	if strings.TrimSpace(cfg.EngineVersion) == "" {
		return nil, failure(OutcomeConfigInvalid, "engine version is required", nil)
	}
	if cfg.Language != DefaultLanguage {
		return nil, failure(OutcomeConfigInvalid, fmt.Sprintf("language %q is not reviewed", cfg.Language), nil)
	}
	if cfg.PageSegMode < 0 || cfg.PageSegMode > 13 {
		return nil, failure(OutcomeConfigInvalid, "page segmentation mode out of range", nil)
	}
	if !filepath.IsAbs(cfg.BinaryPath) || !filepath.IsAbs(cfg.TessdataDir) {
		return nil, failure(OutcomeConfigInvalid, "binary path and tessdata dir must be absolute", nil)
	}
	binaryInfo, err := verifyFile(cfg.BinaryPath, binaryDigest, true)
	if err != nil {
		return nil, err
	}
	modelPath := filepath.Join(cfg.TessdataDir, cfg.Language+".traineddata")
	modelInfo, err := verifyFile(modelPath, modelDigest, false)
	if err != nil {
		return nil, err
	}
	engine := &Engine{
		cfg: cfg,
		version: fmt.Sprintf("%s; binary=sha256:%s; %s.traineddata=sha256:%s",
			strings.TrimSpace(cfg.EngineVersion), binaryDigest, cfg.Language, modelDigest),
		binaryInfo: binaryInfo,
		modelPath:  modelPath,
		modelInfo:  modelInfo,
		args: []string{
			"stdin", "stdout",
			"--oem", "1",
			"--psm", fmt.Sprint(cfg.PageSegMode),
			"-l", cfg.Language,
			"--tessdata-dir", cfg.TessdataDir,
			"tsv",
		},
	}
	return engine, nil
}

// EngineVersion returns the recorded version: release plus pinned digests.
func (e *Engine) EngineVersion() string {
	return e.version
}

func withDefaults(cfg Config) Config {
	if cfg.Language == "" {
		cfg.Language = DefaultLanguage
	}
	if cfg.PageSegMode == 0 {
		cfg.PageSegMode = DefaultPageSegMode
	}
	if cfg.MaxInputBytes <= 0 {
		cfg.MaxInputBytes = DefaultMaxInputBytes
	}
	if cfg.MaxPixels <= 0 {
		cfg.MaxPixels = DefaultMaxPixels
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultTimeout
	}
	if cfg.MaxRegions <= 0 {
		cfg.MaxRegions = DefaultMaxRegions
	}
	if cfg.MaxOutputBytes <= 0 {
		cfg.MaxOutputBytes = DefaultMaxOutputBytes
	}
	if cfg.MaxStderrBytes <= 0 {
		cfg.MaxStderrBytes = DefaultMaxStderrBytes
	}
	if cfg.MaxAddressSpaceBytes <= 0 {
		cfg.MaxAddressSpaceBytes = DefaultMaxAddressSpaceBytes
	}
	return cfg
}

func normalizeDigest(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "sha256:")
	if len(value) != sha256.Size*2 {
		return "", fmt.Errorf("want %d hex characters", sha256.Size*2)
	}
	if _, err := hex.DecodeString(value); err != nil {
		return "", fmt.Errorf("not hex")
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

// unchangedSinceConstruction re-stats the pinned files and rejects a swap.
// It compares identity, size, and modification time, which is cheap enough
// for every call; a full re-hash happens only at construction.
func (e *Engine) unchangedSinceConstruction() error {
	for _, pinned := range []struct {
		path string
		info os.FileInfo
	}{{e.cfg.BinaryPath, e.binaryInfo}, {e.modelPath, e.modelInfo}} {
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
