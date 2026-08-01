// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestContainerImageIdentitySupportCursorUsesDecodedTupleOrder(t *testing.T) {
	t.Parallel()

	short := containerImageIdentitySupportFactID("a", "sha256:cursor", 1)
	long := containerImageIdentitySupportFactID("aa", "sha256:cursor", 1)
	if strings.Compare(short, long) <= 0 {
		t.Fatalf("encoded test precondition failed: %q must sort after %q", short, long)
	}
	shortCursor, err := parseContainerImageIdentitySupportCursor(short)
	if err != nil {
		t.Fatalf("parse short cursor: %v", err)
	}
	longCursor, err := parseContainerImageIdentitySupportCursor(long)
	if err != nil {
		t.Fatalf("parse long cursor: %v", err)
	}
	if compareContainerImageIdentitySupportCursors(shortCursor, longCursor) >= 0 {
		t.Fatalf("decoded tuple order did not preserve short scope before its longer prefix")
	}
}

func TestCurrentContainerImageIdentitySupportLoaderRejectsNonAdvancingPage(t *testing.T) {
	baseTime := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	firstPage := make([][]any, listFactsByKindPageSize)
	for index := range firstPage {
		firstPage[index] = containerImageIdentitySupportFactRow("scope:cursor", "sha256:cursor", index+1, baseTime)
	}
	db := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: firstPage},
		{rows: [][]any{containerImageIdentitySupportFactRow("scope:cursor", "sha256:cursor", listFactsByKindPageSize, baseTime)}},
	}}

	_, err := NewFactStore(db).listCurrentContainerImageIdentitySupportFacts(
		context.Background(),
		containerImageIdentitySupportFactFilter{digests: []string{"sha256:cursor"}},
	)
	if err == nil || !strings.Contains(err.Error(), "cursor did not advance") {
		t.Fatalf("loader error = %v, want non-advancing cursor rejection", err)
	}
}

func TestCurrentContainerImageIdentitySupportLoaderRejectsMalformedFactID(t *testing.T) {
	row := containerImageIdentitySupportFactRow("scope:cursor", "sha256:cursor", 1, time.Now().UTC())
	row[0] = "reducer_container_image_identity_support:zz:zz:zz"
	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{rows: [][]any{row}}}}

	_, err := NewFactStore(db).listCurrentContainerImageIdentitySupportFacts(
		context.Background(),
		containerImageIdentitySupportFactFilter{digests: []string{"sha256:cursor"}},
	)
	if err == nil || !strings.Contains(err.Error(), "invalid fact ID") {
		t.Fatalf("loader error = %v, want malformed fact ID rejection", err)
	}
}

func TestCombineDistinctFactStreamsRejectsCollision(t *testing.T) {
	t.Parallel()

	_, err := combineDistinctFactStreams(
		[]facts.Envelope{{FactID: "fact:collision"}},
		[]facts.Envelope{{FactID: "fact:collision"}},
	)
	if err == nil || !strings.Contains(err.Error(), "duplicate fact ID") {
		t.Fatalf("combine error = %v, want duplicate fact ID rejection", err)
	}
}

func containerImageIdentitySupportFactID(scopeID string, digest string, supportNumber int) string {
	return containerImageIdentitySupportFactIDPrefix +
		hex.EncodeToString([]byte(scopeID)) + ":" +
		hex.EncodeToString([]byte(digest)) + ":" +
		fmt.Sprintf("%064x", supportNumber)
}

func containerImageIdentitySupportFactRow(
	scopeID string,
	digest string,
	supportNumber int,
	observedAt time.Time,
) []any {
	factID := containerImageIdentitySupportFactID(scopeID, digest, supportNumber)
	return []any{
		factID,
		scopeID,
		"generation:cursor",
		"reducer_container_image_identity",
		"stable:" + factID,
		"1.0.0",
		"git",
		int64(1),
		"inferred",
		"git",
		"intent:cursor",
		"",
		"",
		observedAt,
		false,
		[]byte(`{"digest":"` + digest + `"}`),
	}
}
