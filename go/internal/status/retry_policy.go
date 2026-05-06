package status

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"
)

// RetryPolicySummary captures one runtime stage's bounded retry settings.
type RetryPolicySummary struct {
	Stage       string
	MaxAttempts int
	RetryDelay  time.Duration
}

// DefaultRetryPolicies returns the shared runtime retry defaults projected by
// the status surface when a reader does not attach stage-specific metadata.
func DefaultRetryPolicies() []RetryPolicySummary {
	return []RetryPolicySummary{
		{Stage: "projector", MaxAttempts: 3, RetryDelay: 30 * time.Second},
		{Stage: "reducer", MaxAttempts: 3, RetryDelay: 30 * time.Second},
	}
}

// WithRetryPolicies wraps a status reader and attaches static retry metadata
// without persisting the policy into Postgres.
func WithRetryPolicies(reader Reader, policies ...RetryPolicySummary) Reader {
	if reader == nil {
		return nil
	}

	return retryPolicyReader{
		reader:   reader,
		policies: cloneRetryPolicies(policies),
	}
}

type retryPolicyReader struct {
	reader   Reader
	policies []RetryPolicySummary
}

func (r retryPolicyReader) ReadStatusSnapshot(
	ctx context.Context,
	asOf time.Time,
) (RawSnapshot, error) {
	raw, err := r.reader.ReadStatusSnapshot(ctx, asOf)
	if err != nil {
		return RawSnapshot{}, err
	}

	raw.RetryPolicies = cloneRetryPolicies(r.policies)

	return raw, nil
}

func normalizeRetryPolicies(rows []RetryPolicySummary) []RetryPolicySummary {
	normalized := make([]RetryPolicySummary, 0, len(rows))
	for _, row := range rows {
		stage := strings.TrimSpace(row.Stage)
		if stage == "" {
			continue
		}
		normalized = append(normalized, RetryPolicySummary{
			Stage:       stage,
			MaxAttempts: row.MaxAttempts,
			RetryDelay:  row.RetryDelay,
		})
	}
	sort.Slice(normalized, func(i, j int) bool {
		if normalized[i].Stage != normalized[j].Stage {
			return normalized[i].Stage < normalized[j].Stage
		}
		if normalized[i].MaxAttempts != normalized[j].MaxAttempts {
			return normalized[i].MaxAttempts < normalized[j].MaxAttempts
		}
		return normalized[i].RetryDelay < normalized[j].RetryDelay
	})

	return normalized
}

func cloneRetryPolicies(rows []RetryPolicySummary) []RetryPolicySummary {
	if len(rows) == 0 {
		return nil
	}

	cloned := make([]RetryPolicySummary, len(rows))
	copy(cloned, rows)
	return normalizeRetryPolicies(cloned)
}

// MergeRetryPolicies overlays stage-specific retry policy rows onto a base
// policy set and returns a normalized, deduplicated result keyed by stage.
func MergeRetryPolicies(base []RetryPolicySummary, overrides ...RetryPolicySummary) []RetryPolicySummary {
	merged := make(map[string]RetryPolicySummary, len(base)+len(overrides))
	for _, row := range normalizeRetryPolicies(base) {
		merged[row.Stage] = row
	}
	for _, row := range normalizeRetryPolicies(overrides) {
		merged[row.Stage] = row
	}
	if len(merged) == 0 {
		return nil
	}

	rows := make([]RetryPolicySummary, 0, len(merged))
	for _, row := range merged {
		rows = append(rows, row)
	}

	return normalizeRetryPolicies(rows)
}

func retryPoliciesText(rows []RetryPolicySummary) string {
	if len(rows) == 0 {
		return "none"
	}

	parts := make([]string, 0, len(rows))
	for _, row := range rows {
		parts = append(
			parts,
			row.Stage+" max_attempts="+
				strconv.Itoa(row.MaxAttempts)+" retry_delay="+row.RetryDelay.String(),
		)
	}

	return strings.Join(parts, "; ")
}

type retryPolicyJSON struct {
	Stage             string  `json:"stage"`
	MaxAttempts       int     `json:"max_attempts"`
	RetryDelay        string  `json:"retry_delay"`
	RetryDelaySeconds float64 `json:"retry_delay_seconds"`
}

func retryPoliciesJSON(rows []RetryPolicySummary) []retryPolicyJSON {
	if len(rows) == 0 {
		return nil
	}

	projected := make([]retryPolicyJSON, 0, len(rows))
	for _, row := range rows {
		projected = append(projected, retryPolicyJSON{
			Stage:             row.Stage,
			MaxAttempts:       row.MaxAttempts,
			RetryDelay:        row.RetryDelay.String(),
			RetryDelaySeconds: row.RetryDelay.Seconds(),
		})
	}

	return projected
}
