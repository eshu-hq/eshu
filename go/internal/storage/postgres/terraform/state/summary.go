// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package statestore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/status/summary"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

// summaryEncodingTag names the stored entry encoding. It is part of the source
// digest, so changing the encoding makes a reader refuse rows written by an
// older writer instead of decoding them.
const summaryEncodingTag = "terraform-state-summary/1"

// The two sections of the stored terraform_state model: one entry per last
// serial row and one per recent warning row, in the order the live statements
// return them.
const (
	summarySectionSerial  = "last_serial"
	summarySectionWarning = "recent_warning"
)

// serialWire and warningWire are the stored JSON shapes of the live rows. They
// carry exactly the fields of statuspkg.TerraformStateLocatorSerial and
// TerraformStateLocatorWarning; observed_at is RFC 3339 with its sub-second
// part, or null when the live row had none.
type serialWire struct {
	SafeLocatorHash string  `json:"safe_locator_hash"`
	BackendKind     string  `json:"backend_kind"`
	Lineage         string  `json:"lineage"`
	Serial          int64   `json:"serial"`
	GenerationID    string  `json:"generation_id"`
	ObservedAt      *string `json:"observed_at"`
}

type warningWire struct {
	SafeLocatorHash string  `json:"safe_locator_hash"`
	BackendKind     string  `json:"backend_kind"`
	WarningKind     string  `json:"warning_kind"`
	Reason          string  `json:"reason"`
	Severity        string  `json:"severity"`
	Actionability   string  `json:"actionability"`
	Source          string  `json:"source"`
	SourceHandle    string  `json:"source_handle"`
	GenerationID    string  `json:"generation_id"`
	ObservedAt      *string `json:"observed_at"`
}

// SummaryEntries runs the two live admin-evidence statements with the live
// decoder, byte for byte, and returns their rows as entries for the status
// summary writer (#7009 terraform_state model). Rows the live read skips are
// skipped here, so a stored row decodes to exactly what the live read
// returns. The statements take no clock and return no ages, so the stored
// rows need no age correction when they are read.
func SummaryEntries(ctx context.Context, queryer db.Queryer, limit int) ([]summary.Entry, error) {
	evidence, err := ReadTerraformStateAdminEvidence(ctx, queryer, limit, time.Time{})
	if err != nil {
		return nil, err
	}
	entries := make([]summary.Entry, 0, len(evidence.LastSerials)+len(evidence.RecentWarnings))
	for i, row := range evidence.LastSerials {
		encoded, err := json.Marshal(serialWire{
			SafeLocatorHash: row.SafeLocatorHash, BackendKind: row.BackendKind, Lineage: row.Lineage,
			Serial: row.Serial, GenerationID: row.GenerationID, ObservedAt: wireTime(row.ObservedAt),
		})
		if err != nil {
			return nil, fmt.Errorf("encode terraform state serial: %w", err)
		}
		entries = append(entries, summary.Entry{Section: summarySectionSerial, Ordinal: int64(i + 1), JSON: string(encoded)})
	}
	for i, row := range evidence.RecentWarnings {
		encoded, err := json.Marshal(warningWire{
			SafeLocatorHash: row.SafeLocatorHash, BackendKind: row.BackendKind, WarningKind: row.WarningKind,
			Reason: row.Reason, Severity: row.Severity, Actionability: row.Actionability, Source: row.Source,
			SourceHandle: row.SourceHandle, GenerationID: row.GenerationID, ObservedAt: wireTime(row.ObservedAt),
		})
		if err != nil {
			return nil, fmt.Errorf("encode terraform state warning: %w", err)
		}
		entries = append(entries, summary.Entry{Section: summarySectionWarning, Ordinal: int64(i + 1), JSON: string(encoded)})
	}
	return entries, nil
}

// DecodeSummaryEntries decodes stored entries back into the admin evidence the
// live read returns, with empty non-nil slices when a section has no rows. It
// is strict: an unknown section, an unknown field, a wrong type, a bad
// timestamp, or trailing data is an error, so a reader falls back instead of
// serving a row it only half understands.
func DecodeSummaryEntries(entries []summary.Entry) (TerraformStateAdminEvidence, error) {
	evidence := TerraformStateAdminEvidence{
		LastSerials:    []statuspkg.TerraformStateLocatorSerial{},
		RecentWarnings: []statuspkg.TerraformStateLocatorWarning{},
	}
	for _, entry := range entries {
		switch entry.Section {
		case summarySectionSerial:
			var wire serialWire
			if err := decodeStrict(entry.JSON, &wire); err != nil {
				return TerraformStateAdminEvidence{}, fmt.Errorf("decode terraform state serial %d: %w", entry.Ordinal, err)
			}
			observed, err := parseWireTime(wire.ObservedAt)
			if err != nil {
				return TerraformStateAdminEvidence{}, fmt.Errorf("decode terraform state serial %d: %w", entry.Ordinal, err)
			}
			evidence.LastSerials = append(evidence.LastSerials, statuspkg.TerraformStateLocatorSerial{
				SafeLocatorHash: wire.SafeLocatorHash, BackendKind: wire.BackendKind, Lineage: wire.Lineage,
				Serial: wire.Serial, GenerationID: wire.GenerationID, ObservedAt: observed,
			})
		case summarySectionWarning:
			var wire warningWire
			if err := decodeStrict(entry.JSON, &wire); err != nil {
				return TerraformStateAdminEvidence{}, fmt.Errorf("decode terraform state warning %d: %w", entry.Ordinal, err)
			}
			observed, err := parseWireTime(wire.ObservedAt)
			if err != nil {
				return TerraformStateAdminEvidence{}, fmt.Errorf("decode terraform state warning %d: %w", entry.Ordinal, err)
			}
			evidence.RecentWarnings = append(evidence.RecentWarnings, statuspkg.TerraformStateLocatorWarning{
				SafeLocatorHash: wire.SafeLocatorHash, BackendKind: wire.BackendKind, WarningKind: wire.WarningKind,
				Reason: wire.Reason, Severity: wire.Severity, Actionability: wire.Actionability, Source: wire.Source,
				SourceHandle: wire.SourceHandle, GenerationID: wire.GenerationID, ObservedAt: observed,
			})
		default:
			return TerraformStateAdminEvidence{}, fmt.Errorf("decode terraform state summary: unknown section %q", entry.Section)
		}
	}
	return evidence, nil
}

// SummarySourceSHA256 returns the hex SHA-256 identifying what this binary
// would store: both statement texts, the warning limit it passes, and the
// entry encoding. The writer stores it with the row and the reader compares it
// with its own, so a row from another statement or encoding is never decoded.
func SummarySourceSHA256() string {
	return summarySourceSHA256(terraformStateLastSerialQuery, terraformStateRecentWarningsQuery, statuspkg.MaxTerraformStateRecentWarnings)
}

func summarySourceSHA256(serialQuery, warningQuery string, limit int) string {
	digest := sha256.New()
	for _, part := range []string{summaryEncodingTag, serialQuery, warningQuery, strconv.Itoa(limit)} {
		_, _ = digest.Write([]byte(part))
		_, _ = digest.Write([]byte{0})
	}
	return hex.EncodeToString(digest.Sum(nil))
}

func wireTime(t time.Time) *string {
	if t.IsZero() {
		return nil
	}
	text := t.UTC().Format(time.RFC3339Nano)
	return &text
}

func parseWireTime(text *string) (time.Time, error) {
	if text == nil {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, *text)
	if err != nil {
		return time.Time{}, fmt.Errorf("observed_at: %w", err)
	}
	return parsed.UTC(), nil
}

func decodeStrict(text string, target any) error {
	decoder := json.NewDecoder(bytes.NewReader([]byte(text)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errors.New("trailing data after the entry")
	}
	return nil
}
