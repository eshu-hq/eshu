// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rank

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"strings"
)

// StartToken is the rank_cursor value that opens a walk at the first row.
const StartToken = "start"

// cursorPrefix names the token version. A token without it is malformed.
const cursorPrefix = "rc1."

// Cursor layout, before base64url: version, tool, lane, served count
// (uint16), 16-byte basis fingerprint, the two 8-byte digest lanes, then an
// 8-byte checksum over everything before it.
const (
	cursorVersion  byte = 1
	cursorBodySize      = 37
	cursorSumSize       = 8
	cursorSize          = cursorBodySize + cursorSumSize
)

// served is a combinable digest of a set of row ids: each id contributes the
// disjoint byte ranges [0,8) and [8,16) of its sha256 as two 64-bit lanes, and
// lanes add modulo 2^64. Addition commutes,
// so the digest of a set does not depend on the order rows were served in,
// and the digest after k more rows is the old digest plus their lanes.
type served struct {
	a, b uint64
}

func idLanes(id string) served {
	sum := sha256.Sum256([]byte("eshu-rank-walk-id\x00" + id))
	return served{a: binary.BigEndian.Uint64(sum[0:8]), b: binary.BigEndian.Uint64(sum[8:16])}
}

func (s served) add(ids []string) served {
	for _, id := range ids {
		lanes := idLanes(id)
		s.a += lanes.a
		s.b += lanes.b
	}
	return s
}

// cursor is the decoded walk state: which tool minted it, how many rows were
// served, the basis fingerprint of the window, and the served-set digest.
type cursor struct {
	tool   byte
	lane   byte
	count  int
	basis  [16]byte
	digest served
}

func (c cursor) encode() string {
	payload := make([]byte, cursorBodySize, cursorSize)
	payload[0] = cursorVersion
	payload[1] = c.tool
	payload[2] = c.lane
	// #nosec G115 -- c.count is always 0..WindowSize (200): Walk refuses a window of more than WindowSize rows, and parseCursor and Advance refuse a count past it, so the uint16 conversion cannot truncate.
	binary.BigEndian.PutUint16(payload[3:5], uint16(c.count))
	copy(payload[5:21], c.basis[:])
	binary.BigEndian.PutUint64(payload[21:29], c.digest.a)
	binary.BigEndian.PutUint64(payload[29:37], c.digest.b)
	sum := sha256.Sum256(payload)
	payload = append(payload, sum[:cursorSumSize]...)
	return cursorPrefix + base64.RawURLEncoding.EncodeToString(payload)
}

// parseCursor decodes a minted token. It refuses anything else, including
// the start token, with ReasonMalformed. The checksum detects truncation and
// edits; it is not a secret, and a forged token can only select rows the
// caller is already authorized to read.
func parseCursor(token string) (cursor, error) {
	malformed := invalid(ReasonMalformed, "rank_cursor is not a cursor this server returned; restart with rank_cursor=\"start\"")
	encoded, ok := strings.CutPrefix(token, cursorPrefix)
	if !ok {
		return cursor{}, malformed
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(payload) != cursorSize || payload[0] != cursorVersion {
		return cursor{}, malformed
	}
	sum := sha256.Sum256(payload[:cursorBodySize])
	if !bytes.Equal(sum[:cursorSumSize], payload[cursorBodySize:]) {
		return cursor{}, malformed
	}
	c := cursor{
		tool:  payload[1],
		lane:  payload[2],
		count: int(binary.BigEndian.Uint16(payload[3:5])),
		digest: served{
			a: binary.BigEndian.Uint64(payload[21:29]),
			b: binary.BigEndian.Uint64(payload[29:37]),
		},
	}
	copy(c.basis[:], payload[5:21])
	if c.count > WindowSize || !knownCode(toolCodes, c.tool) || !knownCode(laneCodes, c.lane) {
		return cursor{}, malformed
	}
	return c, nil
}

// knownCode reports whether code is one of the values of codes.
func knownCode(codes map[string]byte, code byte) bool {
	for _, known := range codes {
		if known == code {
			return true
		}
	}
	return false
}

// Advance returns the cursor that resumes after ids, given the page cursor
// the page was served from (Page.PageCursor). ids must be a prefix of that
// page's rows, in any order, each id once; a repeated id is refused because
// it would count one row as two. The MCP response budget uses it to cut a page
// to the rows that fit and still hand the client a cursor that reads every
// dropped row on the next page.
func Advance(pageCursor string, ids []string) (string, error) {
	c, err := parseCursor(pageCursor)
	if err != nil {
		return "", err
	}
	if c.count+len(ids) > WindowSize {
		return "", invalid(ReasonMalformed, "rank_cursor advanced past the ranked window")
	}
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if _, dup := seen[id]; dup || id == "" {
			return "", invalid(ReasonMalformed, "rank_cursor cannot advance over a repeated or empty row id")
		}
		seen[id] = struct{}{}
	}
	c.digest = c.digest.add(ids)
	c.count += len(ids)
	return c.encode(), nil
}
