package packetdogfood

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// requiredFamilies are the investigation families the dogfood benchmark must
// exercise per issue #3143. The drift task satisfies the deployable-drift slot.
var requiredFamilies = []string{"supply_chain_impact", "drift", "service_context"}

// requiredApproaches is the closed set of approaches every task must measure. The
// packet is the subject under test; raw_files and eshu_tools are the two
// baselines it must beat. Requiring both baselines prevents a run from claiming
// the packet beat existing Eshu tools without ever measuring that approach.
var requiredApproaches = []Approach{ApproachRawFiles, ApproachEshuTools, ApproachEvidencePacket}

// ParseBenchmark decodes and structurally validates a captured benchmark
// artifact. It rejects an unknown schema, an empty task set, and any task that
// does not measure exactly the closed approach set (raw_files, eshu_tools,
// evidence_packet), so Score always operates on a well-formed benchmark with both
// baselines present.
//
// It also rejects any approach with a non-positive answer_time_ms or token_budget.
// The benchmark gate only proves the packet beats the BEST (fastest, smallest)
// baseline, so a captured artifact must carry honest, independently measured
// baselines — a placeholder baseline with a zero or negative cost would make the
// gate meaningless, and is rejected here rather than silently passing.
func ParseBenchmark(raw []byte) (Benchmark, error) {
	var benchmark Benchmark
	if err := json.Unmarshal(raw, &benchmark); err != nil {
		return Benchmark{}, fmt.Errorf("decode dogfood benchmark: %w", err)
	}
	if benchmark.Schema != BenchmarkSchema {
		return Benchmark{}, fmt.Errorf("benchmark schema = %q, want %q", benchmark.Schema, BenchmarkSchema)
	}
	if len(benchmark.Tasks) == 0 {
		return Benchmark{}, fmt.Errorf("benchmark has no tasks")
	}
	for i, task := range benchmark.Tasks {
		if strings.TrimSpace(task.Family) == "" {
			return Benchmark{}, fmt.Errorf("task %d (%q) has no family", i, task.Name)
		}
		if err := requireApproaches(task); err != nil {
			return Benchmark{}, err
		}
		for _, approach := range task.Approaches {
			if approach.AnswerTimeMS <= 0 || approach.TokenBudget <= 0 {
				return Benchmark{}, fmt.Errorf("task %q approach %q has a non-positive answer_time_ms or token_budget; baselines must be honest measurements", task.Name, approach.Approach)
			}
		}
	}
	return benchmark, nil
}

// requireApproaches enforces the closed approach vocabulary: every task must
// measure raw_files, eshu_tools, and evidence_packet exactly once, and may
// contain no other or duplicate approach. This guarantees both baselines are
// present before scoring, so the gate can never claim the packet beat a baseline
// that was never measured.
func requireApproaches(task Task) error {
	counts := map[Approach]int{}
	for _, result := range task.Approaches {
		counts[result.Approach]++
	}
	for _, approach := range requiredApproaches {
		switch counts[approach] {
		case 1:
			// present exactly once
		case 0:
			return fmt.Errorf("task %q is missing the %q approach", task.Name, approach)
		default:
			return fmt.Errorf("task %q has the %q approach %d times; expected exactly one", task.Name, approach, counts[approach])
		}
	}
	for approach := range counts {
		if !isRequiredApproach(approach) {
			return fmt.Errorf("task %q has unsupported approach %q", task.Name, approach)
		}
	}
	return nil
}

func isRequiredApproach(approach Approach) bool {
	for _, required := range requiredApproaches {
		if required == approach {
			return true
		}
	}
	return false
}

// Score evaluates a benchmark across the dogfood dimensions and returns a
// pass/fail Verdict. The benchmark passes only when the evidence-packet approach
// covers the required families and, on every task, finds the answer at least as
// fast as the best baseline, within the best baseline's token budget, and names
// missing evidence — plus names a gap on at least one task where no baseline did.
func Score(benchmark Benchmark) Verdict {
	verdict := Verdict{
		Schema:    benchmark.Schema,
		RunKind:   benchmark.RunKind,
		RunID:     benchmark.RunID,
		TaskCount: len(benchmark.Tasks),
		Families:  distinctFamilies(benchmark),
	}
	verdict.Criteria = []Criterion{
		scoreFamilyCoverage(benchmark),
		scoreCorrectness(benchmark),
		scoreAnswerTime(benchmark),
		scoreTokenEfficiency(benchmark),
		scoreMissingEvidenceClarity(benchmark),
	}
	verdict.Pass = true
	for _, criterion := range verdict.Criteria {
		if criterion.Status == CriterionFail {
			verdict.Pass = false
		}
	}
	return verdict
}

func scoreFamilyCoverage(benchmark Benchmark) Criterion {
	present := map[string]struct{}{}
	for _, task := range benchmark.Tasks {
		present[strings.TrimSpace(task.Family)] = struct{}{}
	}
	var missing []string
	for _, family := range requiredFamilies {
		if _, ok := present[family]; !ok {
			missing = append(missing, family)
		}
	}
	if len(missing) > 0 {
		return Criterion{Name: "family_coverage", Status: CriterionFail,
			Detail: "missing required families: " + strings.Join(missing, ", ")}
	}
	return Criterion{Name: "family_coverage", Status: CriterionPass,
		Detail: "covers supply-chain impact, deployable drift, and service context"}
}

func scoreCorrectness(benchmark Benchmark) Criterion {
	for _, task := range benchmark.Tasks {
		packet, _ := packetResult(task)
		if !packet.FoundAnswer {
			return Criterion{Name: "answer_correctness", Status: CriterionFail,
				Detail: fmt.Sprintf("packet did not find the answer for task %q", task.Name)}
		}
	}
	return Criterion{Name: "answer_correctness", Status: CriterionPass,
		Detail: "packet found the correct answer on every task"}
}

func scoreAnswerTime(benchmark Benchmark) Criterion {
	for _, task := range benchmark.Tasks {
		packet, _ := packetResult(task)
		best := bestBaselineAnswerTime(task)
		if packet.AnswerTimeMS > best {
			return Criterion{Name: "answer_time", Status: CriterionFail,
				Detail: fmt.Sprintf("task %q: packet %dms slower than best baseline %dms", task.Name, packet.AnswerTimeMS, best)}
		}
	}
	return Criterion{Name: "answer_time", Status: CriterionPass,
		Detail: "packet reached the first answer at least as fast as the best baseline on every task"}
}

func scoreTokenEfficiency(benchmark Benchmark) Criterion {
	for _, task := range benchmark.Tasks {
		packet, _ := packetResult(task)
		best := bestBaselineTokenBudget(task)
		if packet.TokenBudget > best {
			return Criterion{Name: "token_efficiency", Status: CriterionFail,
				Detail: fmt.Sprintf("task %q: packet %d tokens over best baseline %d", task.Name, packet.TokenBudget, best)}
		}
	}
	return Criterion{Name: "token_efficiency", Status: CriterionPass,
		Detail: "packet stayed within the best baseline token budget on every task"}
}

// scoreMissingEvidenceClarity requires the packet to name missing evidence on
// every task and to do so on at least one task where no baseline did — the
// trustworthiness differentiator the benchmark exists to prove.
func scoreMissingEvidenceClarity(benchmark Benchmark) Criterion {
	differentiated := false
	for _, task := range benchmark.Tasks {
		packet, _ := packetResult(task)
		if !packet.MissingEvidenceNamed {
			return Criterion{Name: "missing_evidence_clarity", Status: CriterionFail,
				Detail: fmt.Sprintf("packet did not name missing evidence for task %q", task.Name)}
		}
		if !anyBaselineNamedMissing(task) {
			differentiated = true
		}
	}
	if !differentiated {
		return Criterion{Name: "missing_evidence_clarity", Status: CriterionFail,
			Detail: "no task where the packet named a gap that every baseline missed"}
	}
	return Criterion{Name: "missing_evidence_clarity", Status: CriterionPass,
		Detail: "packet named missing evidence on every task, including gaps the baselines missed"}
}

func packetResult(task Task) (ApproachResult, bool) {
	for _, result := range task.Approaches {
		if result.Approach == ApproachEvidencePacket {
			return result, true
		}
	}
	return ApproachResult{}, false
}

func baselineResults(task Task) []ApproachResult {
	var out []ApproachResult
	for _, result := range task.Approaches {
		if result.Approach != ApproachEvidencePacket {
			out = append(out, result)
		}
	}
	return out
}

// bestBaselineAnswerTime returns the fastest (minimum) baseline answer time, so
// the packet must beat the hardest baseline to win on time, not the easiest.
func bestBaselineAnswerTime(task Task) int {
	best := 0
	for i, result := range baselineResults(task) {
		if i == 0 || result.AnswerTimeMS < best {
			best = result.AnswerTimeMS
		}
	}
	return best
}

// bestBaselineTokenBudget returns the smallest (minimum) baseline token budget,
// so the packet must beat the most efficient baseline to win on tokens.
func bestBaselineTokenBudget(task Task) int {
	best := 0
	for i, result := range baselineResults(task) {
		if i == 0 || result.TokenBudget < best {
			best = result.TokenBudget
		}
	}
	return best
}

func anyBaselineNamedMissing(task Task) bool {
	for _, result := range baselineResults(task) {
		if result.MissingEvidenceNamed {
			return true
		}
	}
	return false
}

func distinctFamilies(benchmark Benchmark) []string {
	seen := map[string]struct{}{}
	var families []string
	for _, task := range benchmark.Tasks {
		family := strings.TrimSpace(task.Family)
		if family == "" {
			continue
		}
		if _, ok := seen[family]; ok {
			continue
		}
		seen[family] = struct{}{}
		families = append(families, family)
	}
	sort.Strings(families)
	return families
}
