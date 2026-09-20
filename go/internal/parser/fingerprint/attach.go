// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fingerprint

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"time"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// MinTokenCount is the emission floor from the #6834 theory proof: function
// bodies with fewer leaf tokens are not fingerprinted. Absent fingerprint
// keys mean "not fingerprinted", never "unique".
const MinTokenCount = 50

// Entity-metadata keys carried on function bucket items, through the
// collector snapshot and content materialization into the entity metadata
// JSONB the #6836 grouping path reads (never source_cache).
const (
	KeyExact      = "body_fp_exact"
	KeyRenamed    = "body_fp_renamed"
	KeySketch     = "body_sketch"
	KeyTokenCount = "body_token_count"
	// StatsKey is the top-level parser payload key carrying the per-file
	// aggregate Stats for collector-side telemetry. It is not an entity
	// key and never enters entity metadata.
	StatsKey = "fingerprint_stats"
)

// Skip reasons for the fingerprinted-vs-skipped telemetry counter.
const (
	ReasonBelowFloor = "below_floor"
	ReasonHasError   = "has_error"
	ReasonNoBody     = "no_body"
)

// Stats aggregates one file's fingerprint outcomes for the collector-side
// telemetry emission (counter by reason, per-file fingerprint-time
// histogram). Parser Parse functions own one Stats per file; the collector
// reads it back from payload[StatsKey].
type Stats struct {
	Fingerprinted   int
	BelowFloor      int
	HasErrorSkipped int
	NoBody          int
	MicrosTotal     int64
}

// Record logs one outcome with its fingerprinting latency. Fingerprinted
// and below-floor outcomes accumulate their latency: below-floor bodies
// still pay the leaf walk that produced their token count, and the
// per-file duration histogram must reflect that cost.
func (s *Stats) Record(reason string, elapsed time.Duration) {
	if s == nil {
		return
	}
	micros := elapsed.Microseconds()
	switch reason {
	case "":
		s.Fingerprinted++
		s.MicrosTotal += micros
	case ReasonBelowFloor:
		s.BelowFloor++
		s.MicrosTotal += micros
	case ReasonHasError:
		s.HasErrorSkipped++
	case ReasonNoBody:
		s.NoBody++
	}
}

// Map renders the stats for payload[StatsKey].
func (s *Stats) Map() map[string]any {
	if s == nil {
		return nil
	}
	return map[string]any{
		"fingerprinted": s.Fingerprinted,
		"below_floor":   s.BelowFloor,
		"has_error":     s.HasErrorSkipped,
		"no_body":       s.NoBody,
		"micros_total":  s.MicrosTotal,
	}
}

// StatsFromMap reads back a Stats rendered by Map, tolerating the JSON
// number widening a cassette or snapshot round trip may apply. A nil or
// unrecognized map yields zero Stats so pre-fingerprint payloads stay silent.
func StatsFromMap(m map[string]any) Stats {
	if m == nil {
		return Stats{}
	}
	return Stats{
		Fingerprinted:   intField(m, "fingerprinted"),
		BelowFloor:      intField(m, "below_floor"),
		HasErrorSkipped: intField(m, "has_error"),
		NoBody:          intField(m, "no_body"),
		MicrosTotal:     int64Field(m, "micros_total"),
	}
}

func intField(m map[string]any, key string) int {
	switch v := m[key].(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}

func int64Field(m map[string]any, key string) int64 {
	switch v := m[key].(type) {
	case int:
		return int64(v)
	case int32:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	default:
		return 0
	}
}

// Attach fingerprints one function body node and sets the Key* entity
// metadata on item. It returns "" when attached, otherwise the skip reason
// (ReasonBelowFloor, ReasonHasError, ReasonNoBody). hasError must report
// whether the file's parse tree contains an error: error graphs are
// excluded per the #6834 verdict. Bodies below MinTokenCount leave no keys.
// Exact-only tiers set KeyExact and KeyTokenCount without renamed or sketch.
func Attach(lang string, hasError bool, body *tree_sitter.Node, src []byte, item map[string]any, stats *Stats) string {
	start := time.Now()
	if body == nil {
		stats.Record(ReasonNoBody, 0)
		return ReasonNoBody
	}
	if hasError {
		stats.Record(ReasonHasError, 0)
		return ReasonHasError
	}
	res := FingerprintBody(lang, body, src)
	if res.TokenCount < MinTokenCount {
		stats.Record(ReasonBelowFloor, time.Since(start))
		return ReasonBelowFloor
	}
	item[KeyExact] = res.Exact
	item[KeyTokenCount] = res.TokenCount
	if res.RenamedSupported {
		item[KeyRenamed] = res.Renamed
		item[KeySketch] = EncodeSketch(res.Sketch)
	}
	stats.Record("", time.Since(start))
	return ""
}

// EncodeSketch renders sketch registers as lowercase hex (16 chars per
// little-endian register) for the KeySketch metadata string.
func EncodeSketch(sketch []uint64) string {
	raw := make([]byte, 0, len(sketch)*8)
	var buf [8]byte
	for _, v := range sketch {
		binary.LittleEndian.PutUint64(buf[:], v)
		raw = append(raw, buf[:]...)
	}
	return hex.EncodeToString(raw)
}

// DecodeSketch parses an EncodeSketch string back into registers.
func DecodeSketch(enc string) ([]uint64, error) {
	raw, err := hex.DecodeString(enc)
	if err != nil {
		return nil, fmt.Errorf("decode fingerprint sketch: %w", err)
	}
	if len(raw) == 0 || len(raw)%8 != 0 {
		return nil, fmt.Errorf("decode fingerprint sketch: %d bytes is not a register multiple", len(raw))
	}
	sketch := make([]uint64, 0, len(raw)/8)
	for off := 0; off < len(raw); off += 8 {
		var v uint64
		for i := 0; i < 8; i++ {
			v |= uint64(raw[off+i]) << (8 * i)
		}
		sketch = append(sketch, v)
	}
	return sketch, nil
}

// BandHashes derives the LSHBands band keys for one sketch. The #6837
// reducer expands bands from the persisted sketch with this function, so
// the band hashes stored in code_fingerprint_band always match a
// re-derivation from body_sketch. A sketch with the wrong register count
// (e.g. hand-crafted entity metadata) yields no bands rather than panicking.
func BandHashes(sketch []uint64) []string {
	if len(sketch) != SketchRegs {
		return nil
	}
	return bands(sketch)
}
