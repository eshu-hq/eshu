// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

// serviceEvidenceOverflowHostname lives in exactly one seeded evidence file:
// the one that sorts last. Whether that file falls inside or outside
// serviceEvidenceFileLimit is the only difference between the two halves of
// every case below.
const serviceEvidenceOverflowHostname = "legacy-billing.acme.test"

// serviceEvidenceFileCorpus builds count candidate evidence files in the order
// ContentReader.ListRepoFiles returns them (`ORDER BY relative_path`, see
// content_reader.go). Only the last file carries a hostname, so the hostname
// is extracted when the corpus fits under the bound and silently absent when
// it does not.
func serviceEvidenceFileCorpus(count int) []FileContent {
	files := make([]FileContent, 0, count)
	for index := range count {
		file := FileContent{
			RepoID:       "repository:orders",
			RelativePath: fmt.Sprintf("deploy/values-%05d.yaml", index),
			Content:      "replicas: 1\n",
		}
		if index == count-1 {
			file.Content = "host: " + serviceEvidenceOverflowHostname + "\n"
		}
		files = append(files, file)
	}
	return files
}

// limitHonoringServiceEvidenceReader applies the caller's limit the way the
// real SQL `LIMIT $2` does. GetFileContent fails the test rather than
// answering: every seeded file already carries content, so a hydration call
// would mean the corpus is not exercising the path this case is about.
type limitHonoringServiceEvidenceReader struct {
	t     *testing.T
	files []FileContent
}

func (r limitHonoringServiceEvidenceReader) ListRepoFiles(
	_ context.Context,
	_ string,
	limit int,
) ([]FileContent, error) {
	if limit > 0 && limit < len(r.files) {
		return append([]FileContent(nil), r.files[:limit]...), nil
	}
	return append([]FileContent(nil), r.files...), nil
}

func (r limitHonoringServiceEvidenceReader) GetFileContent(
	_ context.Context,
	_ string,
	relativePath string,
) (*FileContent, error) {
	r.t.Fatalf("GetFileContent(%q) called; every seeded file already carries content", relativePath)
	return nil, nil
}

// TestLoadServiceQueryEvidenceDisclosesTheFileListBound is the #5720 round-9
// P1-1 regression.
//
// loadServiceQueryEvidence read the repository file list at a bare
// serviceEvidenceFileLimit (5000) -- a real SQL LIMIT -- and every hostname it
// returns is extracted from those files. A hostname living only in a file past
// the cut is therefore never extracted, never reaches
// searchConsumerEvidenceAnyRepo, and a consumer repository reachable only
// through it never enters the merged consumer set. That is the same criterion
// rounds 7 and 8 used to justify disclosing source 2 and the hostname affinity
// narrowing, but this read
// could not reach any flag at all: no over-fetch probe, no length check, and no
// truncated field on ServiceQueryEvidence.
//
// The two subtests seed the same corpus one file apart, so the only variable is
// which side of the bound the hostname-bearing file lands on.
func TestLoadServiceQueryEvidenceDisclosesTheFileListBound(t *testing.T) {
	t.Parallel()

	t.Run("a full page discloses, and the overflow hostname is gone", func(t *testing.T) {
		t.Parallel()

		reader := limitHonoringServiceEvidenceReader{t: t, files: serviceEvidenceFileCorpus(serviceEvidenceFileLimit + 1)}
		evidence, err := loadServiceQueryEvidence(context.Background(), reader, "repository:orders", "orders-api")
		if err != nil {
			t.Fatalf("loadServiceQueryEvidence() error = %v, want nil", err)
		}
		if !evidence.filesTruncated {
			t.Fatalf(
				"filesTruncated = false, want true (the repository holds serviceEvidenceFileLimit+1 = %d candidate files, so the page came back full)",
				serviceEvidenceFileLimit+1,
			)
		}
		if got := serviceEvidenceHostnames(evidence); slices.Contains(got, serviceEvidenceOverflowHostname) {
			t.Fatalf(
				"serviceEvidenceHostnames() = %#v, want %q absent (it lives only in the file past the cut)",
				got, serviceEvidenceOverflowHostname,
			)
		}
	})

	t.Run("the same corpus one file shorter keeps the hostname and discloses nothing", func(t *testing.T) {
		t.Parallel()

		reader := limitHonoringServiceEvidenceReader{t: t, files: serviceEvidenceFileCorpus(serviceEvidenceFileLimit)}
		evidence, err := loadServiceQueryEvidence(context.Background(), reader, "repository:orders", "orders-api")
		if err != nil {
			t.Fatalf("loadServiceQueryEvidence() error = %v, want nil", err)
		}
		if evidence.filesTruncated {
			t.Fatal("filesTruncated = true, want false (the repository holds exactly serviceEvidenceFileLimit files, so nothing was dropped)")
		}
		// Without this the subtest above could pass because the fixture never
		// produced the hostname at all rather than because the bound dropped it.
		if got := serviceEvidenceHostnames(evidence); !slices.Contains(got, serviceEvidenceOverflowHostname) {
			t.Fatalf(
				"serviceEvidenceHostnames() = %#v, want %q present (the same file is inside the cut here)",
				got, serviceEvidenceOverflowHostname,
			)
		}
	})
}

// TestLoadConsumerRepositoryEnrichmentDisclosesTheServiceEvidenceFileBound
// pins source 0 on the merge itself: the file-list bound must reach the
// returned bool with every other source of the enumeration held false.
func TestLoadConsumerRepositoryEnrichmentDisclosesTheServiceEvidenceFileBound(t *testing.T) {
	t.Parallel()

	oneCandidate := []provisioningRepositoryCandidate{
		{RepoID: "repository:consumer-1", RepoName: "consumer-1", RelationshipTypes: []string{"USES_MODULE"}},
	}

	consumers, truncated, err := loadConsumerRepositoryEnrichmentFromCandidates(
		context.Background(), nil, fakePortContentStore{}, "repository:orders", "orders-api",
		nil, defaultIndirectEvidenceSearchLimit, oneCandidate, false, true,
	)
	if err != nil {
		t.Fatalf("loadConsumerRepositoryEnrichmentFromCandidates() error = %v, want nil", err)
	}
	if got, want := len(consumers), defaultIndirectEvidenceSearchLimit; got >= want {
		t.Fatalf("len(consumers) = %d, want well under the limit of %d so only the file bound can set the flag", got, want)
	}
	if !truncated {
		t.Fatal("truncated = false, want true (the service evidence file list came back full, so hostnames past the cut were never searched)")
	}
}

// TestLoadConsumerRepositoryEnrichmentDisclosesTheHostnameLimitCut is the
// #5720 round-9 P2-1 regression for source 3.
//
// The mutant that deletes `truncated = true` from the trimmedHostnames[:limit]
// cut survived the full suite. Source 2 has already capped the surviving
// hostnames at indirectEvidenceHostnameLimit (4), and every production limit
// comes from boundedTraceEnrichmentLimit, whose smallest result is 10, so the
// branch needs a limit of 1, 2, or 3 -- reachable today only through the
// test-only wrappers. Over-disclosure, never a false negative, but the
// enumeration and the public reference both present it as a live bound, so it
// gets exercised rather than deleted.
func TestLoadConsumerRepositoryEnrichmentDisclosesTheHostnameLimitCut(t *testing.T) {
	t.Parallel()

	// Two hostnames, both carrying the service's own token so the affinity
	// filter keeps both, and both under the 4-cap: source 2 and the affinity
	// narrowing are silent, and only the cut against a limit of 1 can set the
	// flag.
	hostnames := []string{"orders-a.example.test", "orders-b.example.test"}
	kept, hostnamesTruncated := boundedIndirectEvidenceHostnamesForService(hostnames, "orders-api")
	if got, want := len(kept), len(hostnames); got != want {
		t.Fatalf("len(kept hostnames) = %d, want %d (both carry the service token and sit under the 4-cap)", got, want)
	}
	if hostnamesTruncated {
		t.Fatal("boundedIndirectEvidenceHostnamesForService() truncated = true, want false so neither source 2 nor the affinity narrowing can be the one reporting")
	}

	oneCandidate := []provisioningRepositoryCandidate{
		{RepoID: "repository:consumer-1", RepoName: "consumer-1", RelationshipTypes: []string{"USES_MODULE"}},
	}
	consumers, truncated, err := loadConsumerRepositoryEnrichmentFromCandidates(
		context.Background(), nil, fakePortContentStore{}, "repository:orders", "orders-api",
		hostnames, 1, oneCandidate, false, false,
	)
	if err != nil {
		t.Fatalf("loadConsumerRepositoryEnrichmentFromCandidates() error = %v, want nil", err)
	}
	if got, want := len(consumers), 1; got != want {
		t.Fatalf("len(consumers) = %d, want %d", got, want)
	}
	if !truncated {
		t.Fatalf(
			"truncated = false, want true (%d hostnames survived both hostname filters and were cut to the caller's limit of 1)",
			len(kept),
		)
	}
}

// traceWithServiceEvidenceCorpus drives the real POST
// /api/v0/impact/trace-deployment-chain handler over a content store holding
// fileCount candidate evidence files and an untruncated provisioning-candidate
// read, and returns the decoded response body.
func traceWithServiceEvidenceCorpus(t *testing.T, fileCount int) map[string]any {
	t.Helper()

	workload := provisioningTruncationWorkload()
	handler := &ImpactHandler{
		Neo4j:   provisioningCandidateGraphReader(workload, provisioningCandidateRows(serviceEvidenceCandidateRowCount)),
		Content: fakePortContentStore{repoFiles: serviceEvidenceFileCorpus(fileCount)},
	}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/trace-deployment-chain",
		strings.NewReader(`{"service_name":"orders-api","include_related_module_usage":true}`),
	)
	w := httptest.NewRecorder()

	handler.traceDeploymentChain(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("traceDeploymentChain status = %d, body = %s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode trace response: %v", err)
	}
	return body
}

// serviceEvidenceCandidateRowCount keeps the provisioning-candidate read far
// under its own bound so source 1 stays false in both directions.
const serviceEvidenceCandidateRowCount = 2

// TestTraceDeploymentChainDisclosesTheServiceEvidenceFileBound proves the
// production wiring of source 0, which the helper-level cases above cannot see:
// enrichServiceQueryContextWithOptions has to hand evidence.filesTruncated to
// loadConsumerRepositoryEnrichmentFromCandidates. Passing a literal false there
// leaves every other test in the package green.
//
// The flags are deliberately opposed, the same shape round 8 used for the
// per-search cap: the provisioning-candidate read is untruncated, so
// dependents_truncated and provisioning_source_chains_truncated must be absent
// while consumer_repositories_truncated is true. That also pins the narrower
// half of the fix -- the file bound belongs to the consumer path only, and
// stamping the other two would report a bound that never applied to them.
func TestTraceDeploymentChainDisclosesTheServiceEvidenceFileBound(t *testing.T) {
	t.Parallel()

	t.Run("a full evidence file page discloses only the consumer flag", func(t *testing.T) {
		t.Parallel()

		body := traceWithServiceEvidenceCorpus(t, serviceEvidenceFileLimit+1)
		if !BoolVal(body, "consumer_repositories_truncated") {
			t.Fatalf(
				"consumer_repositories_truncated = %#v, want true (the service evidence file read returned serviceEvidenceFileLimit+1 = %d rows)",
				body["consumer_repositories_truncated"], serviceEvidenceFileLimit+1,
			)
		}
		for _, field := range []string{"dependents_truncated", "provisioning_source_chains_truncated"} {
			if value, present := body[field]; present {
				t.Fatalf(
					"%s = %#v, want the key absent (the provisioning-candidate read returned %d rows and the file bound does not touch that list)",
					field, value, serviceEvidenceCandidateRowCount,
				)
			}
		}
		// Guards the assertions above against passing for the wrong reason: the
		// merged set must be the graph candidates alone and stay far under the
		// final cap, so source 5 is not what set the flag.
		if got, want := len(mapSliceValue(body, "consumer_repositories")), serviceEvidenceCandidateRowCount; got != want {
			t.Fatalf("len(consumer_repositories) = %d, want %d", got, want)
		}
	})

	t.Run("one file fewer discloses nothing", func(t *testing.T) {
		t.Parallel()

		body := traceWithServiceEvidenceCorpus(t, serviceEvidenceFileLimit)
		for _, field := range provisioningTruncationResponseFields {
			if value, present := body[field]; present {
				t.Fatalf(
					"%s = %#v, want the key absent (the repository holds exactly serviceEvidenceFileLimit = %d candidate files, so nothing was dropped)",
					field, value, serviceEvidenceFileLimit,
				)
			}
		}
		if got := len(mapSliceValue(body, "consumer_repositories")); got == 0 {
			t.Fatal("len(consumer_repositories) = 0, want the untruncated read to still return rows")
		}
	})
}
