package proofofvalue

import "sort"

// SchemaVersion identifies the proof-of-value evidence artifact schema. It
// follows the repository convention of a stable "<name>/v<major>" string so
// that artifact consumers can detect breaking changes.
const SchemaVersion = "proof-of-value-evidence/v1"

// Strategy names the answering strategy a prediction came from.
type Strategy string

const (
	// StrategyBaseline is the agent without Eshu: plain text/grep search only.
	StrategyBaseline Strategy = "baseline_grep"
	// StrategyEshu is the agent with Eshu's reachability analysis.
	StrategyEshu Strategy = "eshu"
)

// Question is one ground-truth task in the proof-of-value set. Label is the
// correct answer derived from the fixture corpus; it is the single source of
// truth a prediction is scored against.
type Question struct {
	// ID is the stable identifier of the question, mirrored from the fixture
	// ground-truth assertion it was derived from.
	ID string
	// Artifact is the corpus artifact the question is about, e.g.
	// "terraform-modules/modules/orphan-cache".
	Artifact string
	// Family is the IaC family the artifact belongs to, e.g. "terraform".
	Family string
	// Prompt is the natural-language question an agent would be asked.
	Prompt string
	// Label is the correct answer: "used", "unused", or "ambiguous".
	Label string
}

// Prediction is one strategy's answer to one Question.
type Prediction struct {
	// QuestionID links the prediction back to its Question.
	QuestionID string
	// Strategy is the answering strategy that produced this prediction.
	Strategy Strategy
	// Answer is the predicted reachability: "used", "unused", or "ambiguous".
	Answer string
}

// QuestionScore records, for one question, whether each strategy answered it
// correctly. It is the per-question audit trail behind the aggregate numbers.
type QuestionScore struct {
	// QuestionID is the scored question.
	QuestionID string `json:"question_id"`
	// Artifact is the corpus artifact under test.
	Artifact string `json:"artifact"`
	// Family is the IaC family of the artifact.
	Family string `json:"family"`
	// Label is the correct answer.
	Label string `json:"label"`
	// BaselineAnswer is the baseline strategy's predicted answer.
	BaselineAnswer string `json:"baseline_answer"`
	// EshuAnswer is the Eshu strategy's predicted answer.
	EshuAnswer string `json:"eshu_answer"`
	// BaselineCorrect reports whether the baseline answer matched the label.
	BaselineCorrect bool `json:"baseline_correct"`
	// EshuCorrect reports whether the Eshu answer matched the label.
	EshuCorrect bool `json:"eshu_correct"`
}

// StrategyMetrics is the honest aggregate scorecard for one strategy. It
// reports both hits and misses so a degraded strategy cannot hide behind
// accuracy alone.
type StrategyMetrics struct {
	// Strategy is the strategy these metrics describe.
	Strategy Strategy `json:"strategy"`
	// Total is the number of scored questions.
	Total int `json:"total"`
	// Correct is the number of questions answered with the correct label.
	Correct int `json:"correct"`
	// Accuracy is Correct/Total in [0,1]; zero when Total is zero.
	Accuracy float64 `json:"accuracy"`
	// DeadTruePositive counts artifacts correctly flagged as cleanup
	// candidates (label "unused" answered "unused").
	DeadTruePositive int `json:"dead_true_positive"`
	// DeadFalsePositive counts artifacts wrongly flagged as cleanup
	// candidates (label not "unused" answered "unused"). These are the
	// dangerous "delete a live artifact" mistakes.
	DeadFalsePositive int `json:"dead_false_positive"`
	// DeadFalseNegative counts dead artifacts the strategy missed (label
	// "unused" answered otherwise).
	DeadFalseNegative int `json:"dead_false_negative"`
	// DeadPrecision is DeadTruePositive/(DeadTruePositive+DeadFalsePositive);
	// zero when the denominator is zero.
	DeadPrecision float64 `json:"dead_precision"`
	// DeadRecall is DeadTruePositive/(DeadTruePositive+DeadFalseNegative);
	// zero when the denominator is zero.
	DeadRecall float64 `json:"dead_recall"`
}

// Delta is the with-Eshu minus without-Eshu comparison. Positive values mean
// Eshu did better on that metric.
type Delta struct {
	// AccuracyDelta is EshuAccuracy minus BaselineAccuracy.
	AccuracyDelta float64 `json:"accuracy_delta"`
	// DeadPrecisionDelta is Eshu dead precision minus baseline dead precision.
	DeadPrecisionDelta float64 `json:"dead_precision_delta"`
	// DeadRecallDelta is Eshu dead recall minus baseline dead recall.
	DeadRecallDelta float64 `json:"dead_recall_delta"`
	// DangerousMistakesAvoided is baseline dead false positives minus Eshu
	// dead false positives: how many "delete a live artifact" errors Eshu
	// avoided relative to grep.
	DangerousMistakesAvoided int `json:"dangerous_mistakes_avoided"`
}

// Report is the full proof-of-value scorecard for a single run over a question
// set. It is the marshaled evidence artifact body.
type Report struct {
	// SchemaVersion is the artifact schema identifier.
	SchemaVersion string `json:"schema_version"`
	// Corpus names the fixture corpus the questions were derived from.
	Corpus string `json:"corpus"`
	// QuestionCount is the number of scored questions.
	QuestionCount int `json:"question_count"`
	// Baseline holds the without-Eshu metrics.
	Baseline StrategyMetrics `json:"baseline"`
	// Eshu holds the with-Eshu metrics.
	Eshu StrategyMetrics `json:"eshu"`
	// Delta holds the with-minus-without comparison.
	Delta Delta `json:"delta"`
	// Questions is the per-question audit trail, sorted by question ID.
	Questions []QuestionScore `json:"questions"`
}

// Score computes an honest Report from a question set and the predictions of
// both strategies. It returns an error when a question lacks exactly one
// prediction per strategy, or when any answer or label is not a known
// reachability value, so that missing or malformed input fails loudly rather
// than silently inflating either strategy's score.
func Score(corpus string, questions []Question, predictions []Prediction) (Report, error) {
	if err := validateQuestions(questions); err != nil {
		return Report{}, err
	}
	index, err := indexPredictions(questions, predictions)
	if err != nil {
		return Report{}, err
	}

	scores := make([]QuestionScore, 0, len(questions))
	baseline := StrategyMetrics{Strategy: StrategyBaseline, Total: len(questions)}
	eshu := StrategyMetrics{Strategy: StrategyEshu, Total: len(questions)}

	for _, q := range questions {
		baseAns := index[predictionKey{q.ID, StrategyBaseline}]
		eshuAns := index[predictionKey{q.ID, StrategyEshu}]
		baseCorrect := baseAns == q.Label
		eshuCorrect := eshuAns == q.Label

		if baseCorrect {
			baseline.Correct++
		}
		if eshuCorrect {
			eshu.Correct++
		}
		accumulateDead(&baseline, q.Label, baseAns)
		accumulateDead(&eshu, q.Label, eshuAns)

		scores = append(scores, QuestionScore{
			QuestionID:      q.ID,
			Artifact:        q.Artifact,
			Family:          q.Family,
			Label:           q.Label,
			BaselineAnswer:  baseAns,
			EshuAnswer:      eshuAns,
			BaselineCorrect: baseCorrect,
			EshuCorrect:     eshuCorrect,
		})
	}

	finalizeMetrics(&baseline)
	finalizeMetrics(&eshu)
	sort.Slice(scores, func(i, j int) bool { return scores[i].QuestionID < scores[j].QuestionID })

	return Report{
		SchemaVersion: SchemaVersion,
		Corpus:        corpus,
		QuestionCount: len(questions),
		Baseline:      baseline,
		Eshu:          eshu,
		Delta: Delta{
			AccuracyDelta:            eshu.Accuracy - baseline.Accuracy,
			DeadPrecisionDelta:       eshu.DeadPrecision - baseline.DeadPrecision,
			DeadRecallDelta:          eshu.DeadRecall - baseline.DeadRecall,
			DangerousMistakesAvoided: baseline.DeadFalsePositive - eshu.DeadFalsePositive,
		},
		Questions: scores,
	}, nil
}

// accumulateDead updates the dead-artifact confusion counts for one answer.
// "unused" is the cleanup-candidate ("dead") decision under test.
func accumulateDead(m *StrategyMetrics, label, answer string) {
	const dead = "unused"
	switch {
	case label == dead && answer == dead:
		m.DeadTruePositive++
	case label != dead && answer == dead:
		m.DeadFalsePositive++
	case label == dead && answer != dead:
		m.DeadFalseNegative++
	}
}

// finalizeMetrics fills the derived ratio fields from the accumulated counts.
func finalizeMetrics(m *StrategyMetrics) {
	if m.Total > 0 {
		m.Accuracy = float64(m.Correct) / float64(m.Total)
	}
	if denom := m.DeadTruePositive + m.DeadFalsePositive; denom > 0 {
		m.DeadPrecision = float64(m.DeadTruePositive) / float64(denom)
	}
	if denom := m.DeadTruePositive + m.DeadFalseNegative; denom > 0 {
		m.DeadRecall = float64(m.DeadTruePositive) / float64(denom)
	}
}
