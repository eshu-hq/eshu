// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schema

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
)

// ValidateCassetteBytes checks one cassette document's envelope shape entirely
// offline, in microseconds, with no Docker and no graph. It enforces the same
// contract the committed cassette-format.v1 JSON Schema declares, in two passes:
//
//  1. full validation against the generated schema (checkAgainstSchema): types,
//     required fields, enums/const, minimum/minLength/minItems, and
//     additionalProperties:false. This is the load-bearing pass — it rejects
//     both field-name typos that JSON decoding silently drops (e.g. "source_ur"
//     for "source_uri") AND schema-only constraints the permissive Go loader
//     never checks (a negative fencing_token, a null metadata/partition_key),
//     so the author-time gate cannot drift from the published schema; and
//  2. the canonical loader's semantic checks (cassette.ParseAndValidate) for the
//     few rules the schema cannot express — most importantly that observed_at is
//     a non-zero instant.
//
// name is used only for error context (typically the cassette's file path).
// Errors are field-level: each names the JSON path at which it occurs.
func ValidateCassetteBytes(name string, data []byte) error {
	doc, err := decodeWithNumbers(data)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	if errs := checkAgainstSchema(doc); len(errs) > 0 {
		return fmt.Errorf("%s: %s", name, strings.Join(errs, "; "))
	}
	if _, err := cassette.ParseAndValidate(data); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}
