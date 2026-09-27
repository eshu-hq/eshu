// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/impact/oci"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// TestOCIRegistryTruthStatementsCarryRecordedRowLimit pins that every OCI
// registry-truth statement carries LIMIT $row_limit (#6590) and that the
// enforced bound matches the recorded max_keys*fan_out_multiplier derivation
// in query-source-coverage.yaml (testOCIRecordedMaxKeys is defined in
// trace_deployment_oci_key_batch_test.go).
func TestOCIRegistryTruthStatementsCarryRecordedRowLimit(t *testing.T) {
	t.Parallel()

	for name, cypher := range map[string]string{
		"tag-observation": ociTagObservationByRefCypher,
		"image-by-digest": ociImageByDigestCypher,
	} {
		if !strings.Contains(cypher, "LIMIT $row_limit") {
			t.Errorf("%s statement = %q, want LIMIT $row_limit", name, cypher)
		}
	}
	if got, want := oci.RegistryTruthRowLimit, testOCIRecordedMaxKeys*3; got != want {
		t.Fatalf("oci.RegistryTruthRowLimit = %d, want %d (testOCIRecordedMaxKeys*3)", got, want)
	}
}

// ociBoundTestReader builds a graph.FakeWorkloadGraphReader that answers the
// two bounded OCI registry-truth statements from tagRows/imageRows (through
// graph.OCIBoundedFakeReader, which enforces $row_limit and sorts/slices like
// the real backend) and the unbounded repository lookup from repoRows.
func ociBoundTestReader(
	t *testing.T,
	tagRows map[string][]map[string]any,
	imageRows map[string][]map[string]any,
	repoRows map[string]map[string]any,
) graph.FakeWorkloadGraphReader {
	t.Helper()
	bounded := graph.OCIBoundedFakeReader{
		T: t,
		Statements: []graph.OCIBoundedStatementFixture{
			{
				CypherContains: "MATCH (tag:ContainerImageTagObservation)",
				KeyParam:       "image_refs",
				KeyField:       "image_ref",
				RowsByKey:      tagRows,
			},
			{
				CypherContains: "MATCH (image:ContainerImage)",
				KeyParam:       "digests",
				KeyField:       "digest",
				RowsByKey:      imageRows,
			},
		},
	}
	return graph.FakeWorkloadGraphReader{
		RunFn: func(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
			if strings.Contains(cypher, "MATCH (repo:OciRegistryRepository)") {
				ids, _ := params["repository_ids"].([]string)
				rows := make([]map[string]any, 0, len(ids))
				for _, id := range ids {
					if row, ok := repoRows[id]; ok {
						rows = append(rows, row)
					}
				}
				return rows, nil
			}
			return bounded.Run(ctx, cypher, params)
		},
	}
}

func ociRepoRow(id, registry, repository, provider string) map[string]any {
	return map[string]any{"repository_id": id, "registry": registry, "repository": repository, "provider": provider}
}

func ociImageRow(digest, repositoryID string) map[string]any {
	return map[string]any{
		"image_id":      "oci-img://" + digest,
		"digest":        digest,
		"repository_id": repositoryID,
		"media_type":    "application/vnd.oci.image.manifest.v1+json",
	}
}

func ociTagRow(imageRef, digest, repositoryID string) map[string]any {
	return map[string]any{"image_ref": imageRef, "tag": "latest", "digest": digest, "repository_id": repositoryID}
}

// TestFetchOCIImageRegistryTruthRowLimitKeepsAmbiguousPairAcrossBoundary is
// decision test 2 (#6590): ref A has 749 tag observations at one digest and
// ref B has 2 observations at two digests. The first statement's batch
// [A,B] (sorted) fills to the 750-row bound entirely with A's rows plus one
// of B's, so advanceOCIBoundedRead must recognize A is already complete (the
// read reached a later key) and retry only B in a continuation statement.
func TestFetchOCIImageRegistryTruthRowLimitKeepsAmbiguousPairAcrossBoundary(t *testing.T) {
	t.Parallel()

	const (
		refA = "ghcr.io/acme/a:latest"
		refB = "ghcr.io/acme/b:latest"
	)
	digestA := "sha256:" + strings.Repeat("a", 64)
	digestB1 := "sha256:" + strings.Repeat("b", 64)
	digestB2 := "sha256:" + strings.Repeat("c", 64)

	aRows := make([]map[string]any, 0, 749)
	for i := 0; i < 749; i++ {
		aRows = append(aRows, ociTagRow(refA, digestA, "repo:a"))
	}

	reader := ociBoundTestReader(t,
		map[string][]map[string]any{
			refA: aRows,
			refB: {ociTagRow(refB, digestB1, "repo:b"), ociTagRow(refB, digestB2, "repo:b")},
		},
		map[string][]map[string]any{
			digestA:  {ociImageRow(digestA, "repo:a")},
			digestB1: {ociImageRow(digestB1, "repo:b")},
			digestB2: {ociImageRow(digestB2, "repo:b")},
		},
		map[string]map[string]any{
			"repo:a": ociRepoRow("repo:a", "ghcr.io", "acme/a", "ghcr"),
			"repo:b": ociRepoRow("repo:b", "ghcr.io", "acme/b", "ghcr"),
		},
	)

	result, err := FetchOCIImageRegistryTruthResult(t.Context(), reader, []string{refA, refB})
	if err != nil {
		t.Fatalf("FetchOCIImageRegistryTruthResult() error = %v, want nil", err)
	}
	if len(result.TruncatedImageRefs) != 0 {
		t.Fatalf("TruncatedImageRefs = %#v, want none: the boundary must not truncate either ref", result.TruncatedImageRefs)
	}
	if got, want := querycontract.BoolVal(result.Limits, "image_registry_truth_complete"), true; got != want {
		t.Fatalf("Limits.image_registry_truth_complete = %v, want %v: %#v", got, want, result.Limits)
	}

	var aRow, bRow map[string]any
	for _, row := range result.Rows {
		switch querycontract.StringVal(row, "image_ref") {
		case refA:
			aRow = row
		case refB:
			bRow = row
		}
	}
	if aRow == nil {
		t.Fatalf("no truth row for ref A (749 observations) in %#v", result.Rows)
	}
	if got := querycontract.StringVal(aRow, "match_strength"); got != oci.TagMatchStrength {
		t.Errorf("ref A match_strength = %q, want %q", got, oci.TagMatchStrength)
	}
	if bRow == nil {
		t.Fatalf("no truth row for ref B (2 observations, 2 digests) in %#v", result.Rows)
	}
	if got := querycontract.StringVal(bRow, "match_strength"); got != oci.AmbiguousMatchStrength {
		t.Errorf("ref B match_strength = %q, want %q", got, oci.AmbiguousMatchStrength)
	}
	candidates, _ := bRow["digest_candidates"].([]string)
	if len(candidates) != 2 {
		t.Fatalf("ref B digest_candidates = %#v, want 2 candidates", bRow["digest_candidates"])
	}
}

// TestFetchOCIImageRegistryTruthDisclosesIrreducibleTagOverflow is decision
// test 3 (#6590): ref C alone produces exactly oci.RegistryTruthRowLimit rows
// -- an irreducible overflow no continuation statement can resolve, because a
// same-sized retry can never tell whether C has exactly the limit or more.
// It must be withheld (never a placeholder row) regardless of whether its
// observations share one digest or split across two; ref D, sorted after C,
// must still resolve normally out of the continuation statement.
func TestFetchOCIImageRegistryTruthDisclosesIrreducibleTagOverflow(t *testing.T) {
	const (
		refC = "ghcr.io/acme/c:latest"
		refD = "ghcr.io/acme/d:latest"
	)
	digestD := "sha256:" + strings.Repeat("9", 64)

	for name, cRows := range map[string][]map[string]any{
		"single-digest": repeatOCITagRow(refC, "sha256:"+strings.Repeat("1", 64), "repo:c", 750),
		"two-digests": append(
			repeatOCITagRow(refC, "sha256:"+strings.Repeat("1", 64), "repo:c", 400),
			repeatOCITagRow(refC, "sha256:"+strings.Repeat("2", 64), "repo:c", 350)...,
		),
	} {
		t.Run(name, func(t *testing.T) {
			reader := ociBoundTestReader(t,
				map[string][]map[string]any{
					refC: cRows,
					refD: {ociTagRow(refD, digestD, "repo:d")},
				},
				map[string][]map[string]any{
					digestD: {ociImageRow(digestD, "repo:d")},
				},
				map[string]map[string]any{
					"repo:d": ociRepoRow("repo:d", "ghcr.io", "acme/d", "ghcr"),
				},
			)

			result, err := FetchOCIImageRegistryTruthResult(t.Context(), reader, []string{refC, refD})
			if err != nil {
				t.Fatalf("FetchOCIImageRegistryTruthResult() error = %v, want nil", err)
			}
			for _, row := range result.Rows {
				if querycontract.StringVal(row, "image_ref") == refC {
					t.Fatalf("got a truth row for irreducibly-overflowed ref C: %#v", row)
				}
			}
			if got, want := result.TruncatedImageRefs, []string{refC}; len(got) != 1 || got[0] != want[0] {
				t.Fatalf("TruncatedImageRefs = %#v, want %#v", got, want)
			}
			if got, want := querycontract.BoolVal(result.Limits, "image_registry_truth_complete"), false; got != want {
				t.Fatalf("Limits.image_registry_truth_complete = %v, want %v: %#v", got, want, result.Limits)
			}
			if got, want := querycontract.StringVal(result.Limits, "image_registry_truth_incomplete_reason"), oci.RegistryTruthRowLimitReason; got != want {
				t.Errorf("Limits.image_registry_truth_incomplete_reason = %q, want %q", got, want)
			}

			var dRow map[string]any
			for _, row := range result.Rows {
				if querycontract.StringVal(row, "image_ref") == refD {
					dRow = row
				}
			}
			if dRow == nil {
				t.Fatalf("no truth row for ref D (resolved out of the continuation statement) in %#v", result.Rows)
			}
			if got := querycontract.StringVal(dRow, "match_strength"); got != oci.TagMatchStrength {
				t.Errorf("ref D match_strength = %q, want %q", got, oci.TagMatchStrength)
			}
		})
	}
}

// TestFetchOCIImageRegistryTruthDigestRowLimitWithholdsAffectedRefs is
// decision test 4 (#6590): digest E overflows the image-by-digest read (750
// rows across distinct repository ids); digest F, sorted after E, resolves
// out of the continuation statement. Tag ref G resolves to two observations,
// one at the withheld digest E and one at digest H: G must be withheld
// entirely rather than silently resolving to H alone, which would be a wrong
// answer through the pre-existing inner join (a tag row whose digest lookup
// found no image row is normally just dropped as "no image found", the same
// shape a genuinely-truncated digest now produces).
func TestFetchOCIImageRegistryTruthDigestRowLimitWithholdsAffectedRefs(t *testing.T) {
	t.Parallel()

	digestE := "sha256:" + strings.Repeat("e", 64)
	digestF := "sha256:" + strings.Repeat("f", 64)
	digestH := "sha256:" + strings.Repeat("1", 64)
	refE := "ghcr.io/acme/x@" + digestE
	refF := "ghcr.io/acme/x@" + digestF
	const refG = "ghcr.io/acme/x:g"

	eRows := make([]map[string]any, 0, 750)
	for i := 0; i < 750; i++ {
		eRows = append(eRows, ociImageRow(digestE, "repo:e"))
	}

	reader := ociBoundTestReader(t,
		map[string][]map[string]any{
			refG: {ociTagRow(refG, digestE, "repo:e"), ociTagRow(refG, digestH, "repo:h")},
		},
		map[string][]map[string]any{
			digestE: eRows,
			digestF: {ociImageRow(digestF, "repo:f")},
			digestH: {ociImageRow(digestH, "repo:h")},
		},
		map[string]map[string]any{
			"repo:e": ociRepoRow("repo:e", "ghcr.io", "acme/x", "ghcr"),
			"repo:f": ociRepoRow("repo:f", "ghcr.io", "acme/x", "ghcr"),
			"repo:h": ociRepoRow("repo:h", "ghcr.io", "acme/x", "ghcr"),
		},
	)

	result, err := FetchOCIImageRegistryTruthResult(t.Context(), reader, []string{refE, refF, refG})
	if err != nil {
		t.Fatalf("FetchOCIImageRegistryTruthResult() error = %v, want nil", err)
	}

	var eRow, fRow, gRow map[string]any
	for _, row := range result.Rows {
		switch querycontract.StringVal(row, "image_ref") {
		case refE:
			eRow = row
		case refF:
			fRow = row
		case refG:
			gRow = row
		}
	}
	if eRow != nil {
		t.Fatalf("got a truth row for digest-addressed ref E (truncated digest): %#v", eRow)
	}
	if gRow != nil {
		t.Fatalf("got a truth row for tag ref G (references truncated digest E): %#v", gRow)
	}
	if fRow == nil {
		t.Fatalf("no truth row for digest-addressed ref F (resolved out of the continuation statement) in %#v", result.Rows)
	}

	withheld := make(map[string]bool, len(result.TruncatedImageRefs))
	for _, ref := range result.TruncatedImageRefs {
		withheld[ref] = true
	}
	if !withheld[refE] {
		t.Errorf("TruncatedImageRefs = %#v, want refE present", result.TruncatedImageRefs)
	}
	if !withheld[refG] {
		t.Errorf("TruncatedImageRefs = %#v, want refG present (references truncated digest E)", result.TruncatedImageRefs)
	}
	if withheld[refF] {
		t.Errorf("TruncatedImageRefs = %#v, want refF absent", result.TruncatedImageRefs)
	}
}

func repeatOCITagRow(imageRef, digest, repositoryID string, n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, ociTagRow(imageRef, digest, repositoryID))
	}
	return rows
}
