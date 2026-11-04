// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/document/media/whispercpp"
)

// Environment variables for the media-import collector. Every variable is
// documented in README.md; the enable flag defaults the collector to off,
// and the tier flag restricts injection to the QA tier.
const (
	envEnabled       = "ESHU_MEDIA_IMPORT_ENABLED"
	envTier          = "ESHU_MEDIA_IMPORT_TIER"
	envArtifactDir   = "ESHU_MEDIA_IMPORT_ARTIFACT_DIR"
	envBinaryPath    = "ESHU_MEDIA_IMPORT_WHISPER_BINARY_PATH"
	envBinarySHA256  = "ESHU_MEDIA_IMPORT_WHISPER_BINARY_SHA256"
	envModelPath     = "ESHU_MEDIA_IMPORT_WHISPER_MODEL_PATH"
	envModelSHA256   = "ESHU_MEDIA_IMPORT_WHISPER_MODEL_SHA256"
	envEngineVersion = "ESHU_MEDIA_IMPORT_WHISPER_ENGINE_VERSION"
	envScopePrefix   = "ESHU_MEDIA_IMPORT_SCOPE_PREFIX"
	envSourceSystem  = "ESHU_MEDIA_IMPORT_SOURCE_SYSTEM"
	envMaxArtifacts  = "ESHU_MEDIA_IMPORT_MAX_ARTIFACTS"
)

const (
	defaultScopePrefix  = "media-import-transcript"
	defaultSourceSystem = "media_import"
	defaultMaxArtifacts = 1024
)

// Config is the validated startup configuration. Enabled is false unless the
// operator explicitly opts in; every other field is required only when
// Enabled is true. Tier is the only deployment tier this caller may inject
// the engine on: the QA tier (#8879, #9729 AC2). No production caller
// beyond the QA tier is claimed.
type Config struct {
	Enabled       bool
	Tier          string
	ArtifactDir   string
	Whisper       whispercpp.Config
	MaxInputBytes int64
	ScopePrefix   string
	SourceSystem  string
	MaxArtifacts  int
}

// loadMediaImportConfig reads the collector configuration from the
// environment. A missing or non-true enable flag disables the collector
// without requiring any other variable. An enabled collector fails closed: a
// missing artifact directory, a non-QA tier, or an engine pin gap is a named
// startup error, never a quiet no-op.
func loadMediaImportConfig(getenv func(string) string) (Config, error) {
	if getenv == nil {
		return Config{}, nil
	}
	if !isEnabled(getenv(envEnabled)) {
		return Config{}, nil
	}
	tier, err := loadTier(getenv)
	if err != nil {
		return Config{}, err
	}
	artifactDir := strings.TrimSpace(getenv(envArtifactDir))
	if artifactDir == "" {
		return Config{}, fmt.Errorf("%s is required when %s is true", envArtifactDir, envEnabled)
	}
	if !filepath.IsAbs(artifactDir) {
		return Config{}, fmt.Errorf("%s must be an absolute path", envArtifactDir)
	}
	whisperCfg, err := loadWhisperConfig(getenv)
	if err != nil {
		return Config{}, err
	}
	maxArtifacts, err := loadMaxArtifacts(getenv)
	if err != nil {
		return Config{}, err
	}
	return Config{
		Enabled:       true,
		Tier:          tier,
		ArtifactDir:   artifactDir,
		Whisper:       whisperCfg,
		MaxInputBytes: whispercpp.DefaultMaxInputBytes,
		ScopePrefix:   firstNonBlank(getenv(envScopePrefix), defaultScopePrefix),
		SourceSystem:  firstNonBlank(getenv(envSourceSystem), defaultSourceSystem),
		MaxArtifacts:  maxArtifacts,
	}, nil
}

// loadTier enforces QA-tier-only engine injection: the enabled collector runs
// only when the operator explicitly declares the QA tier (#8879).
// Any other value fails closed naming the variable.
func loadTier(getenv func(string) string) (string, error) {
	const qaTier = "qa"
	tier := strings.TrimSpace(getenv(envTier))
	if tier == qaTier {
		return tier, nil
	}
	if tier == "" {
		return "", fmt.Errorf("%s is required when %s is true; the only accepted value is %q", envTier, envEnabled, qaTier)
	}
	return "", fmt.Errorf("%s=%q is not accepted; the only accepted value is %q", envTier, tier, qaTier)
}

// loadWhisperConfig validates the pinned engine installation. Every field is
// required: there is no default engine and no silent fallback. Run bounds
// stay at the engine defaults, which sit within the design 1737 section 6
// ceilings; raising one needs a design update and security review.
func loadWhisperConfig(getenv func(string) string) (whispercpp.Config, error) {
	binaryPath := strings.TrimSpace(getenv(envBinaryPath))
	if binaryPath == "" {
		return whispercpp.Config{}, fmt.Errorf("%s is required when %s is true", envBinaryPath, envEnabled)
	}
	if !filepath.IsAbs(binaryPath) {
		return whispercpp.Config{}, fmt.Errorf("%s must be an absolute path", envBinaryPath)
	}
	binarySHA256, err := loadHexDigest(getenv, envBinarySHA256)
	if err != nil {
		return whispercpp.Config{}, err
	}
	modelPath := strings.TrimSpace(getenv(envModelPath))
	if modelPath == "" {
		return whispercpp.Config{}, fmt.Errorf("%s is required when %s is true", envModelPath, envEnabled)
	}
	if !filepath.IsAbs(modelPath) {
		return whispercpp.Config{}, fmt.Errorf("%s must be an absolute path", envModelPath)
	}
	modelSHA256, err := loadHexDigest(getenv, envModelSHA256)
	if err != nil {
		return whispercpp.Config{}, err
	}
	engineVersion := strings.TrimSpace(getenv(envEngineVersion))
	if engineVersion == "" {
		return whispercpp.Config{}, fmt.Errorf("%s is required when %s is true", envEngineVersion, envEnabled)
	}
	return whispercpp.Config{
		BinaryPath:    binaryPath,
		BinarySHA256:  binarySHA256,
		ModelPath:     modelPath,
		ModelSHA256:   modelSHA256,
		EngineVersion: engineVersion,
	}, nil
}

// loadHexDigest reads a required SHA-256 pin: 64 hex characters with an
// optional "sha256:" prefix, which the engine verifier also accepts.
func loadHexDigest(getenv func(string) string, key string) (string, error) {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return "", fmt.Errorf("%s is required when %s is true", key, envEnabled)
	}
	digest := strings.TrimPrefix(strings.ToLower(raw), "sha256:")
	decoded, err := hex.DecodeString(digest)
	if err != nil || len(decoded) != 32 {
		return "", fmt.Errorf("%s must be a 64-character hex SHA-256", key)
	}
	return raw, nil
}

// loadMaxArtifacts bounds the per-run artifact count. The bound cannot be
// disabled through the environment: zero or negative values fail.
func loadMaxArtifacts(getenv func(string) string) (int, error) {
	raw := strings.TrimSpace(getenv(envMaxArtifacts))
	if raw == "" {
		return defaultMaxArtifacts, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", envMaxArtifacts)
	}
	return value, nil
}

// isEnabled reports whether the raw flag value opts in. Only an explicit
// "true" enables the collector; every other value keeps it disabled.
func isEnabled(raw string) bool {
	return strings.EqualFold(strings.TrimSpace(raw), "true")
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
