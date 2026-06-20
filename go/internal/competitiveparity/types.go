package competitiveparity

// SchemaVersion is the stable JSON schema identifier for parity gate reports.
const SchemaVersion = "competitive_parity_gate.v1"

// CheckKind names one validation dimension for a surface.
type CheckKind string

const (
	// CheckCLICommand validates a command path such as "first-run report".
	CheckCLICommand CheckKind = "cli_command"
	// CheckAPIRoute validates an HTTP API route in the surface inventory.
	CheckAPIRoute CheckKind = "api_route"
	// CheckMCPTool validates an MCP tool in the surface inventory.
	CheckMCPTool CheckKind = "mcp_tool"
	// CheckConsolePage validates a console page component in the surface inventory.
	CheckConsolePage CheckKind = "console_page"
	// CheckDoc validates that a public contract doc is present.
	CheckDoc CheckKind = "doc"
	// CheckTruthLabel validates required truth or missing-evidence terms in docs.
	CheckTruthLabel CheckKind = "truth_label"
	// CheckExercise validates a local artifact or contract exercise.
	CheckExercise CheckKind = "exercise"
)

// CheckStatus is the pass/fail state of one check.
type CheckStatus string

const (
	// CheckPass means the checked target was present.
	CheckPass CheckStatus = "pass"
	// CheckFail means the checked target was missing or stale.
	CheckFail CheckStatus = "fail"
)

// Inventory is the offline input to the parity gate.
type Inventory struct {
	Commands     []string
	APIRoutes    []string
	MCPTools     []string
	ConsolePages []string
	Docs         map[string]string
	Exercises    []ExerciseResult
}

// IssueRef links a known residual gap to the existing GitHub issue that owns it.
type IssueRef struct {
	Number int    `json:"number"`
	Reason string `json:"reason"`
}

// ExerciseResult reports one local artifact or contract exercise.
type ExerciseResult struct {
	ID     string
	OK     bool
	Detail string
}

// DocExpectation is a public documentation contract the gate must find.
type DocExpectation struct {
	Path       string
	Terms      []string
	TruthTerms []string
}

// Expectation declares one shipped surface family the gate validates.
type Expectation struct {
	ID             string
	DisplayName    string
	PeerBaseline   string
	Commands       []string
	APIRoutes      []string
	MCPTools       []string
	ConsolePages   []string
	Docs           []DocExpectation
	Exercises      []string
	RelatedIssues  []IssueRef
	ResidualIssues []IssueRef
}

// CheckResult is one validation result.
type CheckResult struct {
	Kind   CheckKind   `json:"kind"`
	Target string      `json:"target"`
	Status CheckStatus `json:"status"`
	Detail string      `json:"detail"`
}

// SurfaceResult is the validation outcome for one expected surface family.
type SurfaceResult struct {
	ID             string        `json:"id"`
	DisplayName    string        `json:"display_name"`
	PeerBaseline   string        `json:"peer_baseline"`
	Pass           bool          `json:"pass"`
	Checks         []CheckResult `json:"checks"`
	RelatedIssues  []IssueRef    `json:"related_issues,omitempty"`
	ResidualIssues []IssueRef    `json:"residual_issues,omitempty"`
}

// Summary reports aggregate pass/fail counts.
type Summary struct {
	SurfaceCount int `json:"surface_count"`
	Passed       int `json:"passed"`
	Failed       int `json:"failed"`
	CheckCount   int `json:"check_count"`
}

// Report is the deterministic parity gate artifact.
type Report struct {
	SchemaVersion string          `json:"schema_version"`
	Pass          bool            `json:"pass"`
	Summary       Summary         `json:"summary"`
	Surfaces      []SurfaceResult `json:"surfaces"`
}
