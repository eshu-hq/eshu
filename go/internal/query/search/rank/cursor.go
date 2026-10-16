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

const (
	cursorVersion  byte = 1
	cursorBodySize      = 36
	cursorSumSize       = 8
	cursorSize          = cursorBodySize + cursorSumSize
)

// served is a combinable digest of a set of row ids: each id contributes its
// sha256 as two 64-bit lanes, and lanes add modulo 2^64. Addition commutes,
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
	count  int
	basis  [16]byte
	digest served
}

func (c cursor) encode() string {
	payload := make([]byte, cursorBodySize, cursorSize)
	payload[0] = cursorVersion
	payload[1] = c.tool
	binary.BigEndian.PutUint16(payload[2:4], uint16(c.count))
	copy(payload[4:20], c.basis[:])
	binary.BigEndian.PutUint64(payload[20:28], c.digest.a)
	binary.BigEndian.PutUint64(payload[28:36], c.digest.b)
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
		count: int(binary.BigEndian.Uint16(payload[2:4])),
		digest: served{
			a: binary.BigEndian.Uint64(payload[20:28]),
			b: binary.BigEndian.Uint64(payload[28:36]),
		},
	}
	copy(c.basis[:], payload[4:20])
	if c.count > WindowSize {
		return cursor{}, malformed
	}
	return c, nil
}

// Advance returns the cursor that resumes after ids, given the page cursor
// the page was served from (Page.PageCursor). ids must be a prefix of that
// page's rows, in any order. The MCP response budget uses it to cut a page
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
	c.digest = c.digest.add(ids)
	c.count += len(ids)
	return c.encode(), nil
}
