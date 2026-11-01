// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tesseract

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// EngineName identifies this backend in OCR region and section metadata.
const EngineName = "tesseract"

// Defaults follow the Image/OCR column of design 1737 section 6: 25 MiB
// source bytes, 128 MiB decoded bytes, 50 megapixels, 30 s wall time, and 500
// regions. A caller may lower any cap; raising one needs a design update.
const (
	DefaultMaxInputBytes = 25 << 20
	DefaultMaxPixels     = 50_000_000
	// DefaultMaxDecodedBytes caps the decoded image size, estimated from the
	// header as pixels x 4 bytes (leptonica's 32 bpp RGB form, the largest
	// it holds for any supported input). It binds before the pixel cap: at 4
	// bytes per pixel, 128 MiB is about 33.5 megapixels.
	DefaultMaxDecodedBytes = 128 << 20
	DefaultTimeout         = 30 * time.Second
	DefaultMaxRegions      = 500
	DefaultMaxOutputBytes  = 8 << 20
	DefaultMaxStderrBytes  = 64 << 10
	// DefaultMaxAddressSpaceBytes is the RLIMIT_AS applied to each child.
	// tesseract 5.5.0 with eng.traineddata peaks near 80 MiB RSS on a
	// 900x200 screenshot; virtual size runs higher, so the cap leaves room.
	DefaultMaxAddressSpaceBytes = 2 << 30
	DefaultLanguage             = "eng"
	DefaultPageSegMode          = 3
)

// decodedBytesPerPixel is the per-pixel cost DefaultMaxDecodedBytes assumes.
const decodedBytesPerPixel = 4

// PinnedFile is one runtime file the engine depends on, with its pinned
// SHA-256 (hex, optionally prefixed with "sha256:").
type PinnedFile struct {
	Path   string
	SHA256 string
}

// Config pins and bounds one tesseract installation. BinaryPath,
// BinarySHA256, Libraries, TessdataDir, ModelSHA256, and EngineVersion are
// required; non-positive caps take the package defaults.
type Config struct {
	// BinaryPath is the absolute path of the tesseract executable. It is
	// executed directly, never through a shell or PATH lookup.
	BinaryPath string
	// BinarySHA256 is the pinned hex SHA-256 of BinaryPath. The executable
	// is a small CLI front end; the OCR code lives in Libraries.
	BinarySHA256 string
	// Libraries are the shared objects the dynamic loader maps for
	// BinaryPath (libtesseract, libleptonica, and their codec and runtime
	// dependencies), each with a pinned digest. At least one is required.
	// The pin list comes from ldd in the reviewed image; per-file hashes are
	// the runtime tamper check, and the image digest plus apk versions are
	// the provenance of record.
	Libraries []PinnedFile
	// TessdataDir holds <Language>.traineddata.
	TessdataDir string
	// ModelSHA256 is the pinned hex SHA-256 of <Language>.traineddata.
	ModelSHA256 string
	// EngineVersion is the reviewed tesseract release, such as "5.5.0". New
	// runs the binary's --version once and fails unless it reports exactly
	// this release.
	EngineVersion string
	// Language is the single traineddata name; only "eng" is reviewed.
	Language string
	// PageSegMode is the tesseract --psm value; 3 is fully automatic. Modes
	// that load osd.traineddata (1, 12) or are not implemented (2) are
	// refused, as is OSD-only mode 0, which zero selects the default for.
	PageSegMode int
	// ScratchDir is the parent of the private per-run working directory.
	// Empty uses os.TempDir(). In a read-only sandbox it must be a
	// writable, noexec tmpfs.
	ScratchDir string

	MaxInputBytes   int64
	MaxPixels       int64
	MaxDecodedBytes int64
	Timeout         time.Duration
	MaxRegions      int
	MaxOutputBytes  int64
	MaxStderrBytes  int64
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
// with its own scratch directory. It does not bound concurrency: N parallel
// calls run N processes, each allowed MaxAddressSpaceBytes, so the caller
// must size its worker count against the sandbox memory limit.
type Engine struct {
	cfg       Config
	version   string
	pinned    []pinnedIdentity
	args      []string
	execCount atomic.Int64
}

// allowedPageSegModes are the --psm values that need no OSD model.
var allowedPageSegModes = map[int]bool{3: true, 4: true, 5: true, 6: true, 7: true, 8: true, 9: true, 10: true, 11: true, 13: true}

// New validates cfg, verifies every pinned digest, confirms the binary
// reports EngineVersion, and returns an Engine. Errors carry
// OutcomeConfigInvalid or OutcomeDigestMismatch.
func New(cfg Config) (*Engine, error) {
	cfg = withDefaults(cfg)
	release := strings.TrimSpace(cfg.EngineVersion)
	if release == "" {
		return nil, failure(OutcomeConfigInvalid, "engine version is required", nil)
	}
	if cfg.Language != DefaultLanguage {
		return nil, failure(OutcomeConfigInvalid, fmt.Sprintf("language %q is not reviewed", cfg.Language), nil)
	}
	if !allowedPageSegModes[cfg.PageSegMode] {
		return nil, failure(OutcomeConfigInvalid, fmt.Sprintf("page segmentation mode %d is not allowed", cfg.PageSegMode), nil)
	}
	if !filepath.IsAbs(cfg.BinaryPath) || !filepath.IsAbs(cfg.TessdataDir) {
		return nil, failure(OutcomeConfigInvalid, "binary path and tessdata dir must be absolute", nil)
	}
	pins, err := verifyInstallation(cfg)
	if err != nil {
		return nil, err
	}
	if err := verifyReportedVersion(cfg.BinaryPath, release); err != nil {
		return nil, err
	}
	return &Engine{
		cfg: cfg,
		version: fmt.Sprintf("%s; binary=sha256:%s; libs=sha256:%s (%d files); %s.traineddata=sha256:%s",
			release, pins.binaryDigest, pins.libraryDigest, len(cfg.Libraries), cfg.Language, pins.modelDigest),
		pinned: pins.identities,
		args: []string{
			"stdin", "stdout",
			"--oem", "1",
			"--psm", fmt.Sprint(cfg.PageSegMode),
			"-l", cfg.Language,
			"--tessdata-dir", cfg.TessdataDir,
			// Select TSV with -c instead of the "tsv" config name, which
			// would read the unpinned file tessdata/configs/tsv.
			"-c", "tessedit_create_tsv=1",
			"-c", "tessedit_create_txt=0",
		},
	}, nil
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
	if cfg.MaxDecodedBytes <= 0 {
		cfg.MaxDecodedBytes = DefaultMaxDecodedBytes
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
