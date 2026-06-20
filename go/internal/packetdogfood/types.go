package packetdogfood

// BenchmarkSchema is the stable schema identifier for a dogfood benchmark
// artifact.
const BenchmarkSchema = "evidence_packet_dogfood.v1"

// Approach names one way of answering an investigation task. The packet approach
// is the subject under test; the others are the baselines it must beat.
type Approach string

const (
	// ApproachRawFiles is reading raw repository files / grep output.
	ApproachRawFiles Approach = "raw_files"
	// ApproachEshuTools is an existing Eshu tool/route drilldown (multiple calls).
	ApproachEshuTools Approach = "eshu_tools"
	// ApproachEvidencePacket is one portable investigation_evidence_packet.v2.
	ApproachEvidencePacket Approach = "evidence_packet"
)

// RunKind distinguishes a reproducible fixture run from a real-repository run.
type RunKind string

const (
	// RunKindFixture is a deterministic fixture scenario, reproducible in CI.
	RunKindFixture RunKind = "fixture"
	// RunKindRealRepo is a captured run against a real indexed Eshu repository.
	RunKindRealRepo RunKind = "real_repo"
)

// ApproachResult is the captured measurement for one approach on one task.
type ApproachResult struct {
	// Approach names the approach measured.
	Approach Approach `json:"approach"`
	// AnswerTimeMS is the wall time to the first useful answer, in milliseconds.
	AnswerTimeMS int `json:"answer_time_ms"`
	// FoundAnswer reports whether the approach produced the correct answer.
	FoundAnswer bool `json:"found_answer"`
	// MissingEvidenceNamed reports whether the approach explicitly named the
	// gaps/missing hops (true for packets; usually false for raw search).
	MissingEvidenceNamed bool `json:"missing_evidence_named"`
	// TokenBudget is the approximate token cost of consuming the output.
	TokenBudget int `json:"token_budget"`
}

// Task is one investigation scored across approaches.
type Task struct {
	// Name is the task identifier.
	Name string `json:"name"`
	// Family is the investigation family the task exercises.
	Family string `json:"family"`
	// Approaches carries the measurement for each approach; it must include the
	// evidence_packet approach and at least one baseline.
	Approaches []ApproachResult `json:"approaches"`
}

// Benchmark is the captured dogfood benchmark artifact.
type Benchmark struct {
	// Schema is always BenchmarkSchema.
	Schema string `json:"schema"`
	// RunKind distinguishes a fixture run from a real-repository run.
	RunKind RunKind `json:"run_kind"`
	// RunID is an opaque, share-safe run identifier.
	RunID string `json:"run_id,omitempty"`
	// Tasks are the scored investigations.
	Tasks []Task `json:"tasks"`
}

// CriterionStatus is the pass/fail/skip state of one dogfood criterion.
type CriterionStatus string

const (
	// CriterionPass marks a satisfied criterion.
	CriterionPass CriterionStatus = "pass"
	// CriterionFail marks a violated criterion.
	CriterionFail CriterionStatus = "fail"
	// CriterionSkip marks a criterion that did not apply.
	CriterionSkip CriterionStatus = "skip"
)

// Criterion is one scored dogfood dimension.
type Criterion struct {
	// Name is the criterion identifier.
	Name string `json:"name"`
	// Status is the criterion outcome.
	Status CriterionStatus `json:"status"`
	// Detail is a human-readable explanation.
	Detail string `json:"detail"`
}

// Verdict is the scored benchmark outcome.
type Verdict struct {
	// Schema echoes the benchmark schema.
	Schema string `json:"schema"`
	// RunKind echoes the benchmark run kind.
	RunKind RunKind `json:"run_kind"`
	// RunID echoes the benchmark run id.
	RunID string `json:"run_id,omitempty"`
	// Pass is true when every applicable criterion passed.
	Pass bool `json:"pass"`
	// TaskCount is the number of scored tasks.
	TaskCount int `json:"task_count"`
	// Families lists the distinct families covered, sorted.
	Families []string `json:"families"`
	// Criteria are the scored dimensions.
	Criteria []Criterion `json:"criteria"`
}
