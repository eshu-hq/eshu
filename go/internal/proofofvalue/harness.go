package proofofvalue

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/iacreachability"
	"gopkg.in/yaml.v3"
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
// per repo, that define the artifact and must be excluded from the baseline
// reference search so the artifact's own definition never counts as a
// reference to itself.
//
// For Terraform, Helm, Kustomize, and Ansible the definition lives under the
// artifact's path, so a path-prefix match identifies it. Compose services are
// different: the analyzer assigns them a synthetic path ("services/<name>")
// that does not exist on disk, while the real definition is the service key
// inside a compose file. For those rows the matching compose file in the same
// repo is excluded instead. This is sound for the baseline because the
// analyzer only treats a Compose service as referenced when a "docker compose"
// command in another file names it, so excluding the declaring compose file
// cannot hide a legitimate reference.
func definingFilesByArtifact(rows []iacreachability.Row, filesByRepo map[string][]iacreachability.File) map[string]map[string]map[string]struct{} {
	out := make(map[string]map[string]map[string]struct{}, len(rows))
	for _, row := range rows {
		key := row.RepoID + "/" + row.ArtifactPath
		defined := map[string]struct{}{}
		prefix := strings.TrimSuffix(row.ArtifactPath, "/") + "/"
		isCompose := row.Family == "compose"
		for _, file := range filesByRepo[row.RepoID] {
			switch {
			case file.RelativePath == row.ArtifactPath || strings.HasPrefix(file.RelativePath, prefix):
				defined[file.RelativePath] = struct{}{}
			case isCompose && composeFileDeclaresService(file, row.ArtifactName):
				defined[file.RelativePath] = struct{}{}
			}
		}
		out[key] = map[string]map[string]struct{}{row.RepoID: defined}
	}
	return out
}

// composeFileDeclaresService reports whether the given file is a Compose file
// that declares a service with the given name. It parses the top-level
// services map the same way the analyzer does, so the baseline excludes
// exactly the file the analyzer derived the service artifact from. It is used
// to exclude a service's own declaration file from the baseline reference
// search.
func composeFileDeclaresService(file iacreachability.File, service string) bool {
	if !isComposeFilePath(file.RelativePath) {
		return false
	}
	var doc struct {
		Services map[string]yaml.Node `yaml:"services"`
	}
	if err := yaml.Unmarshal([]byte(file.Content), &doc); err != nil {
		return false
	}
	_, ok := doc.Services[service]
	return ok
}

// isComposeFilePath reports whether a relative path names a Docker Compose
// file, mirroring the analyzer's Compose file detection.
func isComposeFilePath(relativePath string) bool {
	base := strings.ToLower(relativePath)
	if idx := strings.LastIndexByte(base, '/'); idx >= 0 {
		base = base[idx+1:]
	}
	switch base {
	case "compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml":
		return true
	default:
		return false
	}
}
