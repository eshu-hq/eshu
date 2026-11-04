// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/document/ocr/tesseract"
)

// Environment variables for the document-import collector. Every variable is
// documented in README.md; the enable flag defaults the collector to off.
const (
	envEnabled       = "ESHU_DOC_IMPORT_ENABLED"
	envTier          = "ESHU_DOC_IMPORT_TIER"
	envArtifactDir   = "ESHU_DOC_IMPORT_ARTIFACT_DIR"
	envBinaryPath    = "ESHU_DOC_IMPORT_TESSERACT_BINARY_PATH"
	envBinarySHA256  = "ESHU_DOC_IMPORT_TESSERACT_BINARY_SHA256"
	envLibraries     = "ESHU_DOC_IMPORT_TESSERACT_LIBRARIES"
	envTessdataDir   = "ESHU_DOC_IMPORT_TESSERACT_TESSDATA_DIR"
	envModelSHA256   = "ESHU_DOC_IMPORT_TESSERACT_MODEL_SHA256"
	envEngineVersion = "ESHU_DOC_IMPORT_TESSERACT_ENGINE_VERSION"
	envScopePrefix   = "ESHU_DOC_IMPORT_SCOPE_PREFIX"
	envSourceSystem  = "ESHU_DOC_IMPORT_SOURCE_SYSTEM"
	envMaxArtifacts  = "ESHU_DOC_IMPORT_MAX_ARTIFACTS"
)

const (
	defaultScopePrefix  = "document-import-ocr"
	defaultSourceSystem = "document_import"
	defaultMaxArtifacts = 1024
)

// Config is the validated startup configuration. Enabled is false unless the
// operator explicitly opts in; every other field is required only when
// Enabled is true. Tier is the only deployment tier this caller may inject
// the engine on: the QA tier (#8879). No production caller beyond
// the QA tier is claimed.
type Config struct {
	Enabled      bool
	Tier         string
	ArtifactDir  string
	Tesseract    tesseract.Config
	ScopePrefix  string
	SourceSystem string
	MaxArtifacts int
}

// loadImportConfig reads the collector configuration from the environment.
// A missing or non-true enable flag disables the collector without requiring
// any other variable. An enabled collector fails closed: a missing artifact
// directory or engine pin is a named startup error, never a quiet no-op.
func loadImportConfig(getenv func(string) string) (Config, error) {
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
	tesseractCfg, err := loadTesseractConfig(getenv)
	if err != nil {
		return Config{}, err
	}
	if tesseractCfg.MaxInputBytes <= 0 {
		tesseractCfg.MaxInputBytes = tesseract.DefaultMaxInputBytes
	}
	maxArtifacts, err := loadMaxArtifacts(getenv)
	if err != nil {
		return Config{}, err
	}
	return Config{
		Enabled:      true,
		Tier:         tier,
		ArtifactDir:  artifactDir,
		Tesseract:    tesseractCfg,
		ScopePrefix:  firstNonBlank(getenv(envScopePrefix), defaultScopePrefix),
		SourceSystem: firstNonBlank(getenv(envSourceSystem), defaultSourceSystem),
		MaxArtifacts: maxArtifacts,
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

// loadTesseractConfig validates the pinned engine installation. Every field
// is required: there is no default engine and no silent fallback.
func loadTesseractConfig(getenv func(string) string) (tesseract.Config, error) {
	binaryPath := strings.TrimSpace(getenv(envBinaryPath))
	if binaryPath == "" {
		return tesseract.Config{}, fmt.Errorf("%s is required when %s is true", envBinaryPath, envEnabled)
	}
	if !filepath.IsAbs(binaryPath) {
		return tesseract.Config{}, fmt.Errorf("%s must be an absolute path", envBinaryPath)
	}
	binarySHA256, err := loadHexDigest(getenv, envBinarySHA256)
	if err != nil {
		return tesseract.Config{}, err
	}
	libraries, err := loadLibraryPins(getenv)
	if err != nil {
		return tesseract.Config{}, err
	}
	tessdataDir := strings.TrimSpace(getenv(envTessdataDir))
	if tessdataDir == "" {
		return tesseract.Config{}, fmt.Errorf("%s is required when %s is true", envTessdataDir, envEnabled)
	}
	if !filepath.IsAbs(tessdataDir) {
		return tesseract.Config{}, fmt.Errorf("%s must be an absolute path", envTessdataDir)
	}
	modelSHA256, err := loadHexDigest(getenv, envModelSHA256)
	if err != nil {
		return tesseract.Config{}, err
	}
	engineVersion := strings.TrimSpace(getenv(envEngineVersion))
	if engineVersion == "" {
		return tesseract.Config{}, fmt.Errorf("%s is required when %s is true", envEngineVersion, envEnabled)
	}
	return tesseract.Config{
		BinaryPath:    binaryPath,
		BinarySHA256:  binarySHA256,
		Libraries:     libraries,
		TessdataDir:   tessdataDir,
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
	return digest, nil
}

// loadLibraryPins parses the semicolon-separated path=digest pin list. At
// least one pin is required; entries must be absolute paths with hex digests.
func loadLibraryPins(getenv func(string) string) ([]tesseract.PinnedFile, error) {
	raw := strings.TrimSpace(getenv(envLibraries))
	if raw == "" {
		return nil, fmt.Errorf("%s is required when %s is true", envLibraries, envEnabled)
	}
	var pins []tesseract.PinnedFile
	for _, entry := range strings.Split(raw, ";") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		path, digest, found := strings.Cut(entry, "=")
		if !found {
			return nil, fmt.Errorf("%s entry %q must be path=digest", envLibraries, entry)
		}
		path = strings.TrimSpace(path)
		digest = strings.TrimSpace(digest)
		if !filepath.IsAbs(path) {
			return nil, fmt.Errorf("%s entry %q must use an absolute path", envLibraries, entry)
		}
		stripped := strings.TrimPrefix(strings.ToLower(digest), "sha256:")
		decoded, err := hex.DecodeString(stripped)
		if err != nil || len(decoded) != 32 {
			return nil, fmt.Errorf("%s entry %q must carry a 64-character hex SHA-256", envLibraries, entry)
		}
		pins = append(pins, tesseract.PinnedFile{Path: path, SHA256: stripped})
	}
	if len(pins) == 0 {
		return nil, fmt.Errorf("%s must list at least one path=digest pin", envLibraries)
	}
	return pins, nil
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
