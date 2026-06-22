package proofofvalue

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/iacreachability"
)

// GroundTruth is one corpus-derived expected answer. It mirrors the fields of
// the dead-IaC product-truth assertions
// (tests/fixtures/product_truth/expected/dead_iac.json) that the harness
// scores against.
type GroundTruth struct {
	// ID is the assertion identifier, used as the question ID.
	ID string `json:"id"`
	// Family is the IaC family of the artifact.
	Family string `json:"family"`
	// Artifact is the "repoID/path" key of the artifact under test.
	Artifact string `json:"artifact"`
	// ExpectedReachability is the correct answer: used, unused, or ambiguous.
	ExpectedReachability string `json:"expected_reachability"`
}

// BuildRun derives the proof-of-value question set and both strategies'
// predictions from a fixture corpus and its ground truth. It returns the
// questions and predictions ready for Score.
//
// For every ground-truth assertion it:
//   - emits one Question whose label is the assertion's expected reachability,
//   - records the Eshu prediction from the real iacreachability analyzer row
//     for that artifact, and
//   - records the baseline prediction from the grep model over the same files.
//
// It returns an error if the analyzer produced no row for an asserted
// artifact, so a corpus/analyzer drift fails loudly instead of silently
// dropping a question.
func BuildRun(filesByRepo map[string][]iacreachability.File, truths []GroundTruth) ([]Question, []Prediction, error) {
	rows := iacreachability.Analyze(filesByRepo, iacreachability.Options{IncludeAmbiguous: true})
	rowByArtifact := make(map[string]iacreachability.Row, len(rows))
	for _, row := range rows {
		rowByArtifact[row.RepoID+"/"+row.ArtifactPath] = row
	}

	definers := definingFilesByArtifact(rows, filesByRepo)

	questions := make([]Question, 0, len(truths))
	predictions := make([]Prediction, 0, len(truths)*2)
	for _, truth := range truths {
		row, ok := rowByArtifact[truth.Artifact]
		if !ok {
			return nil, nil, fmt.Errorf("proofofvalue: analyzer produced no row for artifact %q", truth.Artifact)
		}
		questions = append(questions, Question{
			ID:       truth.ID,
			Artifact: truth.Artifact,
			Family:   truth.Family,
			Prompt:   prompt(truth.Family, row.ArtifactName),
			Label:    truth.ExpectedReachability,
		})

		baselineAnswer := BaselineReachability(filesByRepo, row.ArtifactName, definers[truth.Artifact])
		predictions = append(predictions,
			Prediction{QuestionID: truth.ID, Strategy: StrategyBaseline, Answer: baselineAnswer},
			Prediction{QuestionID: truth.ID, Strategy: StrategyEshu, Answer: string(row.Reachability)},
		)
	}
	return questions, predictions, nil
}

// prompt renders the natural-language question an agent would be asked for an
// artifact. It is informational; scoring uses the label, not the prompt text.
func prompt(family, name string) string {
	return fmt.Sprintf("Is the %s artifact %q still used by anything in this corpus, or is it safe to delete?", family, name)
}

// definingFilesByArtifact maps each artifact key to the set of corpus files,
// per repo, that live under the artifact's path. Those files define the
// artifact and must be excluded from the baseline reference search.
func definingFilesByArtifact(rows []iacreachability.Row, filesByRepo map[string][]iacreachability.File) map[string]map[string]map[string]struct{} {
	out := make(map[string]map[string]map[string]struct{}, len(rows))
	for _, row := range rows {
		key := row.RepoID + "/" + row.ArtifactPath
		perRepo := map[string]map[string]struct{}{row.RepoID: {}}
		prefix := strings.TrimSuffix(row.ArtifactPath, "/") + "/"
		for _, file := range filesByRepo[row.RepoID] {
			if file.RelativePath == row.ArtifactPath || strings.HasPrefix(file.RelativePath, prefix) {
				perRepo[row.RepoID][file.RelativePath] = struct{}{}
			}
		}
		out[key] = perRepo
	}
	return out
}
