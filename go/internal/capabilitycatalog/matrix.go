// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package capabilitycatalog

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Matrix is the parsed capability matrix: the machine-readable source of truth
// in specs/capability-matrix.v1.yaml plus specs/capability-matrix/*.yaml
// fragments. Capabilities are sorted by id for deterministic downstream output.
type Matrix struct {
	Capabilities []MatrixCapability
}

// MatrixCapability is one capability row from the matrix.
type MatrixCapability struct {
	// Capability is the stable capability id used by response truth envelopes.
	Capability string
	// Tools are the MCP or API surface names the matrix declares for the
	// capability.
	Tools []string
	// AnsweringStores names the store or stores that answer the capability's
	// reads: graph, fact_records_reducer, fact_records_collector,
	// read_table:<table>, content_search, or status_fixed. Mixed reads name
	// every store. A read that uses an undeclared store fails the verify
	// gate through FindingMissingAnsweringStore.
	AnsweringStores []string
	// Profiles maps a runtime profile id to its support row.
	Profiles map[string]MatrixProfile
}

// MatrixProfile is the per-profile support row.
type MatrixProfile struct {
	// Status is supported, experimental, or unsupported.
	Status string
	// MaxTruthLevel is the highest truth level allowed in the profile.
	MaxTruthLevel string
	// RequiredRuntime is the runtime shape required for the row.
	RequiredRuntime string
	// P95LatencyMS is the declared p95 latency budget in milliseconds. A nil
	// value means the row explicitly has no latency budget, usually because the
	// capability is unsupported for that profile.
	P95LatencyMS *int
	// MaxScopeSize is the declared maximum scope size for the profile.
	MaxScopeSize string
	// Verification lists the proof signals declared for the row.
	Verification []MatrixVerification
}

// MatrixVerification is one proof signal: a kind (go_test, integration_test,
// compose_e2e, remote_validation) and its reference.
type MatrixVerification struct {
	Kind string
	Ref  string
}

// matrixFile is the on-disk YAML shape for the main matrix and each fragment.
type matrixFile struct {
	Capabilities []matrixFileCapability `yaml:"capabilities"`
}

type matrixFileCapability struct {
	Capability      string                          `yaml:"capability"`
	Tools           []string                        `yaml:"tools"`
	AnsweringStores []string                        `yaml:"answering_stores"`
	Profiles        map[string]matrixFileProfileRow `yaml:"profiles"`
}

type matrixFileProfileRow struct {
	Status          string              `yaml:"status"`
	MaxTruthLevel   string              `yaml:"max_truth_level"`
	RequiredRuntime string              `yaml:"required_runtime"`
	P95LatencyMS    *int                `yaml:"p95_latency_ms"`
	MaxScopeSize    string              `yaml:"max_scope_size"`
	Verification    []map[string]string `yaml:"verification"`
}

// allowedVerificationKinds is the closed set of verification kinds a matrix
// row may declare. An unlisted key is a hard load error (#5407) rather than a
// silently accepted proof signal: mirrors
// go/internal/backendconformance/matrix.go's allowedVerificationKeys so the
// two matrices agree on the proof-kind vocabulary.
var allowedVerificationKinds = map[string]struct{}{
	"go_test":           {},
	"integration_test":  {},
	"compose_e2e":       {},
	"remote_validation": {},
}

// allowedAnsweringStores is the closed set of answering-store tokens a matrix
// row may declare (#10880). The fact-table tokens split by producer so a
// reducer-result read and a collector-fact read can never share a
// declaration; dedicated read-model and operational tables are named through
// the read_table: prefix; content_search covers the content store and the
// search index; status_fixed covers persisted status snapshots, static
// policy, generated artifacts, caller-supplied derivations, and external
// backends with no eshu store read. An unlisted token is a hard load error,
// like an unknown verification kind.
var allowedAnsweringStores = map[string]struct{}{
	"graph":                  {},
	"fact_records_reducer":   {},
	"fact_records_collector": {},
	"content_search":         {},
	"status_fixed":           {},
}

// ValidAnsweringStore reports whether store is in the closed answering-store
// vocabulary: one of the fixed tokens or read_table:<table> naming a
// dedicated Postgres table or view.
func ValidAnsweringStore(store string) bool {
	if _, ok := allowedAnsweringStores[store]; ok {
		return true
	}
	name, ok := strings.CutPrefix(store, "read_table:")
	if !ok || name == "" {
		return false
	}
	for _, r := range name {
		if r != '_' && (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	return name[0] >= 'a' && name[0] <= 'z'
}

// MatrixFileName is the main capability matrix file inside the specs directory.
const MatrixFileName = "capability-matrix.v1.yaml"

// MatrixFragmentDir is the directory of capability matrix fragments inside the
// specs directory.
const MatrixFragmentDir = "capability-matrix"

// LoadMatrix reads the capability matrix from specsDir, merging the main file
// with every fragment under capability-matrix/. It rejects duplicate
// capability ids across files and returns capabilities sorted by id.
func LoadMatrix(specsDir string) (Matrix, error) {
	main, err := readMatrixFile(filepath.Join(specsDir, MatrixFileName))
	if err != nil {
		return Matrix{}, err
	}

	seen := map[string]struct{}{}
	capabilities := make([]MatrixCapability, 0, len(main.Capabilities))
	appendCapability := func(raw matrixFileCapability) error {
		if _, ok := seen[raw.Capability]; ok {
			return fmt.Errorf("duplicate capability %q", raw.Capability)
		}
		seen[raw.Capability] = struct{}{}
		converted, err := convertCapability(raw)
		if err != nil {
			return err
		}
		capabilities = append(capabilities, converted)
		return nil
	}
	for _, raw := range main.Capabilities {
		if err := appendCapability(raw); err != nil {
			return Matrix{}, err
		}
	}

	fragments, err := readFragmentFiles(filepath.Join(specsDir, MatrixFragmentDir))
	if err != nil {
		return Matrix{}, err
	}
	for _, fragment := range fragments {
		for _, raw := range fragment.Capabilities {
			if err := appendCapability(raw); err != nil {
				return Matrix{}, err
			}
		}
	}

	sort.Slice(capabilities, func(i, j int) bool {
		return capabilities[i].Capability < capabilities[j].Capability
	})
	return Matrix{Capabilities: capabilities}, nil
}

// readFragmentFiles reads every .yaml/.yml fragment in dir in sorted order. A
// missing directory is treated as no fragments.
func readFragmentFiles(dir string) ([]matrixFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read fragment dir %s: %w", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if ext := filepath.Ext(entry.Name()); ext != ".yaml" && ext != ".yml" {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)

	files := make([]matrixFile, 0, len(names))
	for _, name := range names {
		file, err := readMatrixFile(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	return files, nil
}

func readMatrixFile(path string) (matrixFile, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- path is program-constructed from the operator-configured specsDir joined with a fixed filename or ReadDir-enumerated fragment name
	if err != nil {
		return matrixFile{}, fmt.Errorf("read matrix %s: %w", path, err)
	}
	var parsed matrixFile
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		return matrixFile{}, fmt.Errorf("parse matrix %s: %w", path, err)
	}
	return parsed, nil
}

// convertAnsweringStores validates the matrix row's answering-store
// declarations against the closed vocabulary. An unlisted token is a hard
// load error, like an unknown verification kind: a read must never silently
// claim a store outside the declared contract.
func convertAnsweringStores(raw matrixFileCapability) ([]string, error) {
	stores := append([]string(nil), raw.AnsweringStores...)
	for _, store := range stores {
		if !ValidAnsweringStore(store) {
			return nil, fmt.Errorf("capability %q: unknown answering store %q", raw.Capability, store)
		}
	}
	return stores, nil
}

func convertCapability(raw matrixFileCapability) (MatrixCapability, error) {
	profiles := make(map[string]MatrixProfile, len(raw.Profiles))
	for id, row := range raw.Profiles {
		verification, err := convertVerification(row.Verification)
		if err != nil {
			return MatrixCapability{}, fmt.Errorf("capability %q profile %q: %w", raw.Capability, id, err)
		}
		profiles[id] = MatrixProfile{
			Status:          row.Status,
			MaxTruthLevel:   row.MaxTruthLevel,
			RequiredRuntime: row.RequiredRuntime,
			P95LatencyMS:    row.P95LatencyMS,
			MaxScopeSize:    row.MaxScopeSize,
			Verification:    verification,
		}
	}
	stores, err := convertAnsweringStores(raw)
	if err != nil {
		return MatrixCapability{}, err
	}
	tools := append([]string(nil), raw.Tools...)
	return MatrixCapability{
		Capability:      raw.Capability,
		Tools:           tools,
		AnsweringStores: stores,
		Profiles:        profiles,
	}, nil
}

// convertVerification flattens the YAML list of single-key maps into ordered
// kind/ref pairs. Each entry like {go_test: ./internal/query} yields one
// MatrixVerification; multi-key maps are expanded in sorted key order so the
// output stays deterministic. A key outside allowedVerificationKinds is a hard
// error (#5407): an unrecognized proof kind must not be silently accepted as
// verification evidence.
func convertVerification(raw []map[string]string) ([]MatrixVerification, error) {
	out := make([]MatrixVerification, 0, len(raw))
	for _, entry := range raw {
		keys := make([]string, 0, len(entry))
		for key := range entry {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if _, ok := allowedVerificationKinds[key]; !ok {
				return nil, fmt.Errorf("unknown verification kind %q (allowed: %s)", key, strings.Join(sortedVerificationKinds(), ", "))
			}
			out = append(out, MatrixVerification{Kind: key, Ref: entry[key]})
		}
	}
	return out, nil
}

// sortedVerificationKinds returns allowedVerificationKinds' keys sorted, for a
// deterministic error message.
func sortedVerificationKinds() []string {
	kinds := make([]string, 0, len(allowedVerificationKinds))
	for kind := range allowedVerificationKinds {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	return kinds
}
