// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parserfixture

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/replay"
)

// SchemaVersionV1 is the only parser-fixture schema version this package reads
// and writes. A parser fixture is a sibling of the cassette format but records
// parser-emitted facts (with file provenance) rather than live-collector facts,
// so it carries its own version literal.
const SchemaVersionV1 = "1"

// File is the root document of a parser-fixture JSON file. It records the
// envelopes one parser-fact emission pass produced over a source tree, grouped
// into a single scope+generation, so the replay side can reproduce them
// credential-free and a reviewer can audit provenance in the diff.
type File struct {
	// Language is the demo language label that produced this fixture (e.g. "go",
	// "hcl"). Informational; not validated at replay time.
	Language string `json:"language"`
	// SchemaVersion must equal SchemaVersionV1.
	SchemaVersion string `json:"schema_version"`
	// Scope is the single recorded scope+generation worth of parser facts.
	Scope Scope `json:"scope"`
}

// Scope is one recorded scope+generation worth of parser-emitted facts.
type Scope struct {
	// ScopeID is the durable scope identity the parser facts were emitted under
	// (required).
	ScopeID string `json:"scope_id"`
	// SourceSystem names the fact family. Parser file facts are emitted by the Git
	// collector seam, so this is "git".
	SourceSystem string `json:"source_system"`
	// ScopeKind is the scope classification (e.g. "repository").
	ScopeKind string `json:"scope_kind"`
	// CollectorKind is the collector family label (e.g. "git").
	CollectorKind string `json:"collector_kind"`
	// GenerationID is the durable generation identity (required).
	GenerationID string `json:"generation_id"`
	// ObservedAt is the observation timestamp for the generation (required).
	ObservedAt time.Time `json:"observed_at"`
	// RepoID is the stable repository identity used in fact keys and payloads.
	RepoID string `json:"repo_id"`
	// Facts is the ordered list of recorded fact envelopes with provenance.
	Facts []Fact `json:"facts"`
}

// Fact is one recorded parser-emitted fact envelope. Provenance fields
// (SourceURI, SourceRecordID) are recorded explicitly so a regression that drops
// or changes provenance is visible in the fixture diff and caught by the
// round-trip gate.
type Fact struct {
	// FactKind is the durable fact type identifier (required, e.g. "file").
	FactKind string `json:"fact_kind"`
	// StableFactKey is the durable deduplication key (required).
	StableFactKey string `json:"stable_fact_key"`
	// SchemaVersion is the payload schema version, when the emitter set one.
	SchemaVersion string `json:"schema_version,omitempty"`
	// CollectorKind is the collector that produced the fact (e.g. "git").
	CollectorKind string `json:"collector_kind,omitempty"`
	// FencingToken is the generation fencing token. Defaults to 1 at replay.
	FencingToken int64 `json:"fencing_token,omitempty"`
	// SourceConfidence is the evidence quality label (e.g. "observed").
	SourceConfidence string `json:"source_confidence,omitempty"`
	// Payload is the parser-emitted fact payload, preserved verbatim (required).
	Payload map[string]any `json:"payload"`
	// SourceSystem is the provenance source system (e.g. "git"). Defaults to the
	// scope source system when empty.
	SourceSystem string `json:"source_system,omitempty"`
	// SourceURI is the provenance URI — the absolute file path the parser fact was
	// emitted from. First-class for R-7: a dropped/changed value is a regression.
	SourceURI string `json:"source_uri"`
	// SourceRecordID is the collector's source-record identity for the fact.
	// Defaults to StableFactKey at replay when empty (the file-fact emitter sets it
	// equal to the key, so it is recorded only when it diverges).
	SourceRecordID string `json:"source_record_id,omitempty"`
}

// canonicalOptions returns the canonical serialization options for a parser
// fixture: the file-fact emitter sets observed_at and generation_id per
// scope+generation, so those collapse/derive exactly as the cassette format,
// facts order by stable_fact_key, and the parser payload subtree is opaque so
// the parser output (which may itself contain keys named line_number, etc.)
// is preserved verbatim. No secret keys: parser file facts carry source-tree
// content, not credentials.
func canonicalOptions() replay.CanonicalOptions {
	opts := replay.DefaultCanonicalOptions()
	// The cassette default sorts a "scopes" array; a parser fixture has a single
	// "scope" object, so only the per-scope "facts" array ordering applies. The
	// default already maps "facts" -> "stable_fact_key" and marks "payload" opaque.
	return opts
}

// LoadFile reads and validates a parser fixture from path.
func LoadFile(path string) (File, error) {
	// #nosec G304 -- path is an operator-supplied fixture location (the recorder
	// output path / repo-shipped testdata), not user- or request-derived input.
	data, err := os.ReadFile(path)
	if err != nil {
		return File{}, fmt.Errorf("read parser fixture %q: %w", path, err)
	}
	f, err := ParseAndValidate(data)
	if err != nil {
		return File{}, fmt.Errorf("parser fixture %q: %w", path, err)
	}
	return f, nil
}

// LoadFileRehydrated reads a portable parser fixture from path and rehydrates its
// repo-root sentinel against repoRoot before validating, so a committed fixture's
// tokenized provenance and payload paths become the local absolute paths the live
// parser produces. A fixture without a sentinel (an absolute-path temp-dir
// recording) loads unchanged. repoRoot is required.
func LoadFileRehydrated(path, repoRoot string) (File, error) {
	if strings.TrimSpace(repoRoot) == "" {
		return File{}, errNoRepoRoot
	}
	// #nosec G304 -- path is an operator-supplied fixture location (repo-shipped
	// testdata / recorder output), not user- or request-derived input.
	data, err := os.ReadFile(path)
	if err != nil {
		return File{}, fmt.Errorf("read parser fixture %q: %w", path, err)
	}
	data, err = rehydrate(data, repoRoot)
	if err != nil {
		return File{}, fmt.Errorf("parser fixture %q: %w", path, err)
	}
	f, err := ParseAndValidate(data)
	if err != nil {
		return File{}, fmt.Errorf("parser fixture %q: %w", path, err)
	}
	return f, nil
}

// ParseAndValidate decodes parser-fixture bytes and runs structural validation
// without touching the filesystem, so on-disk fixtures and in-memory candidates
// (the recorder's load-back guard) go through one validation path.
func ParseAndValidate(data []byte) (File, error) {
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return File{}, fmt.Errorf("parse parser fixture: %w", err)
	}
	if err := f.validate(); err != nil {
		return File{}, fmt.Errorf("invalid parser fixture: %w", err)
	}
	return f, nil
}

func (f File) validate() error {
	if f.SchemaVersion != SchemaVersionV1 {
		return fmt.Errorf("unsupported schema_version %q (want %q)", f.SchemaVersion, SchemaVersionV1)
	}
	return f.Scope.validate()
}

func (s Scope) validate() error {
	if strings.TrimSpace(s.ScopeID) == "" {
		return errors.New("scope_id is required")
	}
	if strings.TrimSpace(s.SourceSystem) == "" {
		return errors.New("source_system is required")
	}
	if strings.TrimSpace(s.ScopeKind) == "" {
		return errors.New("scope_kind is required")
	}
	if strings.TrimSpace(s.CollectorKind) == "" {
		return errors.New("collector_kind is required")
	}
	if strings.TrimSpace(s.GenerationID) == "" {
		return errors.New("generation_id is required")
	}
	if s.ObservedAt.IsZero() {
		return errors.New("observed_at is required and must be non-zero")
	}
	if len(s.Facts) == 0 {
		return errors.New("scope must contain at least one fact")
	}
	for i, fct := range s.Facts {
		if err := fct.validate(); err != nil {
			return fmt.Errorf("fact[%d]: %w", i, err)
		}
	}
	return nil
}

func (f Fact) validate() error {
	if strings.TrimSpace(f.FactKind) == "" {
		return errors.New("fact_kind is required")
	}
	if strings.TrimSpace(f.StableFactKey) == "" {
		return errors.New("stable_fact_key is required")
	}
	if f.Payload == nil {
		return errors.New("payload is required (use {} for an empty payload)")
	}
	if strings.TrimSpace(f.SourceURI) == "" {
		return errors.New("source_uri is required (parser-fact provenance)")
	}
	return nil
}

// fencingToken returns the effective fencing token.
func (f Fact) fencingToken() int64 {
	if f.FencingToken > 0 {
		return f.FencingToken
	}
	return 1
}

// sourceConfidence returns the effective source confidence label.
func (f Fact) sourceConfidence() string {
	if sc := strings.TrimSpace(f.SourceConfidence); sc != "" {
		return sc
	}
	return "observed"
}

// sourceSystem returns the effective provenance source system, falling back to
// the scope's source system.
func (f Fact) sourceSystem(scopeSourceSystem string) string {
	if ss := strings.TrimSpace(f.SourceSystem); ss != "" {
		return ss
	}
	return scopeSourceSystem
}

// sourceRecordID returns the effective source-record id, defaulting to the
// stable fact key when the fixture omits it.
func (f Fact) sourceRecordID() string {
	if id := strings.TrimSpace(f.SourceRecordID); id != "" {
		return id
	}
	return f.StableFactKey
}

// collectorKind returns the effective collector kind, falling back to the
// scope-level kind.
func (f Fact) collectorKind(scopeCollectorKind string) string {
	if ck := strings.TrimSpace(f.CollectorKind); ck != "" {
		return ck
	}
	return scopeCollectorKind
}
