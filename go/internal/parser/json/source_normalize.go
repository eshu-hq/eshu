// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package json

import (
	"bytes"
	"strings"
)

// normalizeJSONSource strips a UTF-8 BOM, skips a leading `{{ ... }}`
// templating banner line, trims leading blank lines, and (for JSONC
// filenames such as tsconfig*.json) removes `/* */`/`//` comments and
// trailing commas so encoding/json can decode the result. It also returns an
// offsetTranslator that maps a byte offset in the returned buffer back to the
// corresponding real on-disk offset in source (issue #5358): every
// transformation above can remove or shift '\n' bytes relative to source (a
// trimmed blank line, a skipped banner line, or a stripped block comment all
// consume real newlines that never reach the returned buffer), so a
// newlineIndex built directly from the returned buffer would answer every
// downstream line_number lookup with a too-small, wrong on-disk line for any
// entity after the stripped region. Callers that need real line numbers must
// build their index via buildTranslatedNewlineIndex(source, translator)
// rather than buildNewlineIndex(normalizedBytes).
func normalizeJSONSource(source []byte, filename string) (string, offsetTranslator) {
	afterBOM := bytes.TrimPrefix(source, []byte("\xef\xbb\xbf"))
	afterBOMStr := string(afterBOM)
	trimmed := strings.TrimLeft(afterBOMStr, "\ufeff")
	// prefixOffset accumulates every byte removed from the FRONT of source by
	// a pure-prefix transform (BOM, `{{ }}` banner lines, leading blank
	// lines). Every such transform only truncates a prefix -- it never
	// reorders or drops interior bytes -- so normalized[i] always corresponds
	// to source[prefixOffset+i] once JSONC stripping (if any) is accounted
	// for separately below.
	prefixOffset := int64(len(source)-len(afterBOM)) + int64(len(afterBOMStr)-len(trimmed))

	if strings.TrimSpace(trimmed) == "" {
		return "", identityOffsetTranslator
	}

	lines := strings.Split(trimmed, "\n")
	start := 0
	for start < len(lines) {
		candidate := strings.TrimSpace(lines[start])
		if strings.HasPrefix(candidate, "{{") && strings.HasSuffix(candidate, "}}") {
			prefixOffset += int64(len(lines[start])) + 1 // +1 for the '\n' Split consumed
			start++
			continue
		}
		break
	}

	rest := strings.Join(lines[start:], "\n")
	leadingTrimmed := strings.TrimLeft(rest, " \t\r\n")
	prefixOffset += int64(len(rest) - len(leadingTrimmed))

	if !isJSONCConfigFilename(filename) {
		translate := func(offset int64) int64 { return offset + prefixOffset }
		return leadingTrimmed, translate
	}

	withoutComments, commentOffsets := stripJSONCCommentsWithOffsets(leadingTrimmed)
	withoutCommas, commaOffsets := stripTrailingCommasWithOffsets(withoutComments)
	translate := func(offset int64) int64 {
		return prefixOffset + mapOffset(commentOffsets, mapOffset(commaOffsets, offset))
	}
	return withoutCommas, translate
}

func identityOffsetTranslator(offset int64) int64 { return offset }

// mapOffset looks up offset in a WithOffsets map (see
// stripJSONCCommentsWithOffsets / stripTrailingCommasWithOffsets), clamping
// out-of-range offsets to the nearest valid entry so a caller-supplied offset
// at or past the end of the buffer degrades to the last known position
// instead of panicking.
func mapOffset(offsets []int64, offset int64) int64 {
	if len(offsets) == 0 {
		return 0
	}
	if offset < 0 {
		offset = 0
	}
	if int(offset) >= len(offsets) {
		return offsets[len(offsets)-1]
	}
	return offsets[offset]
}
