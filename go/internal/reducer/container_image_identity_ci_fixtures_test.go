// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "github.com/eshu-hq/eshu/go/internal/facts"

// ciRunFact and ciArtifactFact build minimal ci.run / ci.artifact envelopes
// for container_image_identity's own tests, which anchor their fixtures on
// realistic CI/CD evidence without depending on the ci_cd_run_correlation
// family's decode path.
//
// They mirror the equivalent fixture builders in that family's own test suite
// (go/internal/reducer/cicdrun/ci_cd_run_correlation_test.go): the two
// families cannot share one package-private helper across the root/cicdrun
// seam (issue #6061), so each keeps its own copy of these trivial builders.
func ciRunFact(runID, provider, repositoryID, commitSHA string) facts.Envelope {
	return facts.Envelope{
		FactID:           "ci.run:" + runID,
		FactKind:         facts.CICDRunFactKind,
		SourceRef:        facts.Ref{SourceSystem: "ci_cd_run"},
		SourceConfidence: facts.SourceConfidenceReported,
		Payload: map[string]any{
			"provider":      provider,
			"run_id":        runID,
			"run_attempt":   "1",
			"repository_id": repositoryID,
			"commit_sha":    commitSHA,
			"status":        "completed",
			"result":        "success",
		},
	}
}

func ciArtifactFact(factID, runID, digest string) facts.Envelope {
	return facts.Envelope{
		FactID:           factID,
		FactKind:         facts.CICDArtifactFactKind,
		SourceConfidence: facts.SourceConfidenceReported,
		Payload: map[string]any{
			"provider":        "github_actions",
			"run_id":          runID,
			"run_attempt":     "1",
			"artifact_type":   "container_image",
			"artifact_digest": digest,
		},
	}
}

// containerImageIdentityFact builds a minimal reducer_container_image_identity
// envelope for tests. It mirrors the equivalent fixture builder in the
// ci_cd_run_correlation family's own test suite (issue #6061; see the doc
// comment on ciRunFact above).
func containerImageIdentityFact(factID, repositoryID, imageRef, digest string) facts.Envelope {
	return facts.Envelope{
		FactID:           factID,
		FactKind:         containerImageIdentityFactKind,
		SourceConfidence: facts.SourceConfidenceInferred,
		Payload: map[string]any{
			"repository_id": repositoryID,
			"image_ref":     imageRef,
			"digest":        digest,
		},
	}
}

// stringSliceContains reports whether want appears in values. It mirrors the
// equivalent helper in the ci_cd_run_correlation family's own test suite
// (issue #6061; see the doc comment on ciRunFact above).
func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
