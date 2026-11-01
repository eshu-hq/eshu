// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package whispercpp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel/trace"

	docmedia "github.com/eshu-hq/eshu/go/internal/collector/document/media"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// EngineName identifies this backend in transcript section metadata.
const EngineName = "whisper.cpp"

const (
	defaultLanguage          = "en"
	defaultThreads           = 4
	maxThreads               = 64
	defaultMaxInputBytes     = int64(64 << 20)
	defaultMaxDurationMillis = int64(10 * 60 * 1000)
	defaultTimeout           = 10 * time.Minute
	defaultMaxStdoutBytes    = int64(16 << 20)
	defaultMaxStderrBytes    = int64(64 << 10)
	defaultMaxSegments       = 1000
	defaultMaxSegmentChars   = 4096
)

// Design ceilings from design 1737 section 6 (audio/video transcript
// column). Runtime config may lower a limit; New rejects any value above its
// ceiling because raising one needs a design update and security review.
const (
	ceilingInputBytes     = int64(250 << 20)
	ceilingDurationMillis = int64(30 * 60 * 1000)
	ceilingTimeout        = 10 * time.Minute
	ceilingSegments       = 1000
	// ceilingSegmentChars keeps a stored chunk within 16 KiB at up to 4
	// bytes per rune.
	ceilingSegmentChars = 4096
	// ceilingStdoutBytes matches the 512 MiB decoded-bytes scratch budget.
	ceilingStdoutBytes = int64(512 << 20)
)

var (
	languagePattern = regexp.MustCompile(`^[a-z]{2,3}$`)
	digestPattern   = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// Limits bounds one engine run. Zero fields take the documented defaults.
// Values above the design 1737 section 6 ceilings are rejected by New.
type Limits struct {
	// MaxInputBytes caps the media body, checked before any decode or exec.
	MaxInputBytes int64
	// MaxDurationMillis caps the header-derived audio duration, checked
	// before exec.
	MaxDurationMillis int64
	// Timeout caps engine wall-clock time; the process group is killed at
	// the deadline.
	Timeout time.Duration
	// MaxStdoutBytes caps engine JSON output; a breach kills the engine.
	MaxStdoutBytes int64
	// MaxStderrBytes caps engine diagnostics; a breach kills the engine.
	// Stderr content is counted, never logged or returned.
	MaxStderrBytes int64
	// MaxSegments caps returned speech segments; overflow fails the run.
	MaxSegments int
	// MaxSegmentChars caps runes per segment; overflow fails the run.
	MaxSegmentChars int
	// MaxAddressSpaceBytes, when positive, sets RLIMIT_AS on the engine
	// process right after start (Linux only). Zero leaves the cap to the
	// container memory limit.
	MaxAddressSpaceBytes uint64
}

// Config pins the engine binary and model and wires optional telemetry.
type Config struct {
	// BinaryPath is the absolute path of the pinned whisper-cli executable.
	BinaryPath string
	// BinarySHA256 is the lowercase hex sha256 of BinaryPath's content.
	BinarySHA256 string
	// ModelPath is the absolute path of the pinned ggml model file.
	ModelPath string
	// ModelSHA256 is the lowercase hex sha256 of ModelPath's content.
	ModelSHA256 string
	// EngineVersion is the pinned upstream release recorded in facts, for
	// example "v1.9.5".
	EngineVersion string
	// Language is the fixed decode language; empty means "en". Automatic
	// language detection is not accepted.
	Language string
	// Threads fixes the decode thread count; zero means 4.
	Threads int
	// Limits bounds each run.
	Limits Limits
	// Instruments, Tracer, and Logger are optional; nil disables each.
	Instruments *telemetry.Instruments
	Tracer      trace.Tracer
	Logger      *slog.Logger
}

// Engine runs the pinned whisper.cpp CLI out of process behind the
// media.Engine contract. It is immutable after New and safe for concurrent
// use; each Transcribe call starts its own bounded process.
type Engine struct {
	cfg         Config
	limits      Limits
	modelName   string
	modelDigest string
	args        []string
}

var _ docmedia.Engine = (*Engine)(nil)

// New validates cfg, verifies the binary and model sha256 digests against
// their pins, and returns a ready Engine. A digest mismatch wraps
// ErrDigestMismatch.
func New(cfg Config) (*Engine, error) {
	if cfg.Language == "" {
		cfg.Language = defaultLanguage
	}
	if !languagePattern.MatchString(cfg.Language) {
		return nil, fmt.Errorf("whisper.cpp language must be a 2-3 letter lowercase code")
	}
	if cfg.Threads == 0 {
		cfg.Threads = defaultThreads
	}
	if cfg.Threads < 1 || cfg.Threads > maxThreads {
		return nil, fmt.Errorf("whisper.cpp threads must be between 1 and %d", maxThreads)
	}
	if strings.TrimSpace(cfg.EngineVersion) == "" {
		return nil, fmt.Errorf("whisper.cpp engine version pin is required")
	}
	if err := verifyPinnedFile("binary", cfg.BinaryPath, cfg.BinarySHA256); err != nil {
		return nil, err
	}
	if err := verifyPinnedFile("model", cfg.ModelPath, cfg.ModelSHA256); err != nil {
		return nil, err
	}
	if err := checkCeilings(cfg.Limits); err != nil {
		return nil, err
	}
	if err := checkPlatform(cfg.Limits); err != nil {
		return nil, err
	}
	return &Engine{
		cfg:         cfg,
		limits:      normalizeLimits(cfg.Limits),
		modelName:   filepath.Base(cfg.ModelPath),
		modelDigest: "sha256:" + cfg.ModelSHA256,
		args:        buildArgs(cfg.ModelPath, cfg.Language, cfg.Threads),
	}, nil
}

// buildArgs returns the fixed whisper-cli argument vector: audio on stdin,
// full JSON on stdout, no prints, CPU only, greedy decoding with temperature
// 0 and no temperature fallback. No element comes from media content.
func buildArgs(model, language string, threads int) []string {
	return []string{
		"-m", model,
		"-f", "-",
		"-of", "-",
		"-ojf",
		"-np",
		"-ng",
		"-l", language,
		"-t", strconv.Itoa(threads),
		"-bs", "1",
		"-bo", "1",
		"-tp", "0",
		"-nf",
	}
}

// checkCeilings rejects any configured limit above its design 1737 section 6
// ceiling. Zero fields take defaults, which sit at or below the ceilings.
func checkCeilings(limits Limits) error {
	switch {
	case limits.MaxInputBytes > ceilingInputBytes:
		return fmt.Errorf("whisper.cpp input cap exceeds the 250 MiB design ceiling")
	case limits.MaxDurationMillis > ceilingDurationMillis:
		return fmt.Errorf("whisper.cpp duration cap exceeds the 30 minute design ceiling")
	case limits.Timeout > ceilingTimeout:
		return fmt.Errorf("whisper.cpp timeout exceeds the 10 minute design ceiling")
	case limits.MaxSegments > ceilingSegments:
		return fmt.Errorf("whisper.cpp segment cap exceeds the 1000 chunk design ceiling")
	case limits.MaxSegmentChars > ceilingSegmentChars:
		return fmt.Errorf("whisper.cpp segment text cap exceeds the 16 KiB chunk design ceiling")
	case limits.MaxStdoutBytes > ceilingStdoutBytes:
		return fmt.Errorf("whisper.cpp stdout cap exceeds the 512 MiB scratch design ceiling")
	}
	return nil
}

func normalizeLimits(limits Limits) Limits {
	if limits.MaxInputBytes <= 0 {
		limits.MaxInputBytes = defaultMaxInputBytes
	}
	if limits.MaxDurationMillis <= 0 {
		limits.MaxDurationMillis = defaultMaxDurationMillis
	}
	if limits.Timeout <= 0 {
		limits.Timeout = defaultTimeout
	}
	if limits.MaxStdoutBytes <= 0 {
		limits.MaxStdoutBytes = defaultMaxStdoutBytes
	}
	if limits.MaxStderrBytes <= 0 {
		limits.MaxStderrBytes = defaultMaxStderrBytes
	}
	if limits.MaxSegments <= 0 {
		limits.MaxSegments = defaultMaxSegments
	}
	if limits.MaxSegmentChars <= 0 {
		limits.MaxSegmentChars = defaultMaxSegmentChars
	}
	return limits
}

func verifyPinnedFile(role, path, digest string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("whisper.cpp %s path must be absolute", role)
	}
	if !digestPattern.MatchString(digest) {
		return fmt.Errorf("whisper.cpp %s sha256 pin must be 64 lowercase hex characters", role)
	}
	file, err := os.Open(path) // #nosec G304 -- operator-pinned absolute path, digest-verified below.
	if err != nil {
		return fmt.Errorf("open whisper.cpp %s: %w", role, err)
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat whisper.cpp %s: %w", role, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("whisper.cpp %s must be a regular file", role)
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return fmt.Errorf("hash whisper.cpp %s: %w", role, err)
	}
	if got := hex.EncodeToString(hash.Sum(nil)); got != digest {
		return fmt.Errorf("whisper.cpp %s: %w", role, ErrDigestMismatch)
	}
	return nil
}

// Transcribe validates and normalizes the WAV input in process, runs the
// pinned engine out of process under the configured caps, and returns timed
// speech segments. It fails closed with a classified *Error; a run that
// hears no speech returns a ClassNoSpeech error wrapping media.ErrNoSpeech.
func (e *Engine) Transcribe(ctx context.Context, input docmedia.Media) (docmedia.EngineResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	run := e.startRun(ctx, input)
	result, err := e.transcribe(run.ctx, input, &run.stats)
	run.finish(err)
	if err != nil {
		return docmedia.EngineResult{}, err
	}
	return result, nil
}

func (e *Engine) transcribe(ctx context.Context, input docmedia.Media, stats *runStats) (docmedia.EngineResult, error) {
	if err := contextError(ctx); err != nil {
		return docmedia.EngineResult{}, err
	}
	stats.inputBytes = int64(len(input.Body))
	if input.Format != "" && input.Format != "wav" {
		return docmedia.EngineResult{}, classified(ClassUnsupportedFormat, "only wav media is accepted")
	}
	if len(input.Body) == 0 {
		return docmedia.EngineResult{}, classified(ClassMalformedMedia, "empty media body")
	}
	if stats.inputBytes > e.limits.MaxInputBytes {
		return docmedia.EngineResult{}, classified(ClassResourceLimit, "input bytes exceed cap")
	}
	info, err := parseWAV(input.Body)
	if err != nil {
		return docmedia.EngineResult{}, err
	}
	stats.audioMillis = info.durationMillis()
	if stats.audioMillis > e.limits.MaxDurationMillis {
		return docmedia.EngineResult{}, classified(ClassResourceLimit, "audio duration exceeds cap")
	}
	audio, err := normalizeWAV(ctx, info)
	if err != nil {
		return docmedia.EngineResult{}, err
	}
	output, err := e.runProcess(ctx, audio, stats)
	if err != nil {
		return docmedia.EngineResult{}, err
	}
	parsed, err := parseOutput(output, e.limits, stats)
	if err != nil {
		return docmedia.EngineResult{}, err
	}
	language := e.cfg.Language
	if languagePattern.MatchString(parsed.language) {
		language = parsed.language
	}
	return docmedia.EngineResult{
		EngineName:    EngineName,
		EngineVersion: e.cfg.EngineVersion,
		ModelName:     e.modelName,
		ModelDigest:   e.modelDigest,
		Language:      language,
		Segments:      parsed.segments,
	}, nil
}

func contextError(ctx context.Context) error {
	err := ctx.Err()
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.DeadlineExceeded):
		return wrapped(ClassTimeout, "caller deadline exceeded", err)
	default:
		return wrapped(ClassCanceled, "caller canceled the run", err)
	}
}
