// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite/testutil"
)

// TestSBOMMultiSubjectWriterKeepsFullSetAndRefreshesWinner proves one writer
// call carries both image identities, replay keeps their IDs stable, and a
// later one-subject pass retracts only the absent subject.
func TestSBOMMultiSubjectWriterKeepsFullSetAndRefreshesWinner(t *testing.T) {
	t.Parallel()
	facts := sbomVersionGuardEnvelopes("4.17.12", "pkg:npm/lodash@4.17.12")
	const digestFixed = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	facts = append(facts,
		sbomComponentImpactFactWithPackageIDVersion("fixed-writer", "doc-fixed-writer", "pkg:npm/lodash@4.17.21", crossScopeAffectedPackageID, "4.17.21"),
		sbomAttachmentImpactFact("attachment-fixed-writer", "doc-fixed-writer", digestFixed),
		containerImageIdentityImpactFact("image-fixed-writer", digestFixed, testImpactRepositoryID),
	)
	findings := BuildSupplyChainImpactFindings(facts)
	if len(findings) != 2 {
		t.Fatalf("findings=%#v", findings)
	}
	beginner := newFakeImpactBeginner(&testutil.FakeExecer{})
	writer := PostgresSupplyChainImpactWriter{DB: beginner}
	write := retractTestWrite()
	write.Findings = findings
	ids := []string{supplyChainImpactFactID(write, findings[0]), supplyChainImpactFactID(write, findings[1])}
	for pass := range 2 {
		write.FencingToken = int64(pass + 7)
		if _, err := writer.WriteSupplyChainImpactFindings(context.Background(), write); err != nil {
			t.Fatalf("pass %d: %v", pass, err)
		}
		keep := lastSupplyChainImpactKeepSet(t, beginner)
		if !slices.Equal(keep, ids) {
			t.Fatalf("pass %d keep=%q, want %q", pass, keep, ids)
		}
	}
	write.Findings = findings[:1]
	write.FencingToken++
	if _, err := writer.WriteSupplyChainImpactFindings(context.Background(), write); err != nil {
		t.Fatal(err)
	}
	if keep := lastSupplyChainImpactKeepSet(t, beginner); !slices.Equal(keep, ids[:1]) {
		t.Fatalf("winner refresh keep=%q, want %q", keep, ids[:1])
	}
	if beginner.state.commits != 3 {
		t.Fatalf("commits=%d, want one per complete pass", beginner.state.commits)
	}
}

func lastSupplyChainImpactKeepSet(t *testing.T, beginner fakeImpactBeginner) []string {
	t.Helper()
	for i := len(beginner.state.all) - 1; i >= 0; i-- {
		call := beginner.state.all[i]
		if call.Query == retractSupersededSupplyChainImpactFindingsQuery {
			return call.Args[3].([]string)
		}
	}
	t.Fatal("no retraction statement")
	return nil
}
