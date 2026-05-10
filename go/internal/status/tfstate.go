package status

import (
	"sort"
	"strings"
	"time"
)

// TerraformStateLocatorSerial reports the most recent observed state serial for
// one Terraform-state scope, keyed by the scope-level safe locator hash so the
// report never carries raw bucket names, S3 keys, or local file paths.
type TerraformStateLocatorSerial struct {
	SafeLocatorHash string
	BackendKind     string
	Lineage         string
	Serial          int64
	GenerationID    string
	ObservedAt      time.Time
}

// TerraformStateLocatorWarning reports recent warning_fact observations for one
// Terraform-state scope, grouped by warning_kind so operators can spot patterns
// without scanning the full fact stream.
type TerraformStateLocatorWarning struct {
	SafeLocatorHash string
	BackendKind     string
	WarningKind     string
	Reason          string
	Source          string
	GenerationID    string
	ObservedAt      time.Time
}

// MaxTerraformStateRecentWarnings caps the number of recent warning rows the
// admin status surface will return per safe_locator_hash. Postgres still owns
// the canonical history; this bound prevents the JSON projection from growing
// without limits across restarts.
const MaxTerraformStateRecentWarnings = 50

// CloneTerraformStateSerials returns a defensive copy of a serial slice so the
// report cannot be mutated by callers after rendering.
func CloneTerraformStateSerials(rows []TerraformStateLocatorSerial) []TerraformStateLocatorSerial {
	if len(rows) == 0 {
		return nil
	}
	cloned := make([]TerraformStateLocatorSerial, len(rows))
	copy(cloned, rows)
	return cloned
}

// CloneTerraformStateWarnings returns a defensive copy of a warning slice.
func CloneTerraformStateWarnings(rows []TerraformStateLocatorWarning) []TerraformStateLocatorWarning {
	if len(rows) == 0 {
		return nil
	}
	cloned := make([]TerraformStateLocatorWarning, len(rows))
	copy(cloned, rows)
	return cloned
}

// SortTerraformStateSerials orders serial rows deterministically by safe
// locator hash so JSON output is stable across reads.
func SortTerraformStateSerials(rows []TerraformStateLocatorSerial) []TerraformStateLocatorSerial {
	cloned := CloneTerraformStateSerials(rows)
	sort.SliceStable(cloned, func(i, j int) bool {
		left := strings.TrimSpace(cloned[i].SafeLocatorHash)
		right := strings.TrimSpace(cloned[j].SafeLocatorHash)
		return left < right
	})
	return cloned
}

// SortTerraformStateWarnings orders warnings deterministically by safe locator
// hash, then warning_kind, then ObservedAt descending. The Postgres query is
// expected to bound the input; this enforces ordering for stable JSON output.
func SortTerraformStateWarnings(rows []TerraformStateLocatorWarning) []TerraformStateLocatorWarning {
	cloned := CloneTerraformStateWarnings(rows)
	sort.SliceStable(cloned, func(i, j int) bool {
		left := cloned[i]
		right := cloned[j]
		if left.SafeLocatorHash != right.SafeLocatorHash {
			return left.SafeLocatorHash < right.SafeLocatorHash
		}
		if left.WarningKind != right.WarningKind {
			return left.WarningKind < right.WarningKind
		}
		return left.ObservedAt.After(right.ObservedAt)
	})
	return cloned
}

// GroupTerraformStateWarningsByKind buckets warnings per safe locator hash and
// warning_kind, returning a map keyed first by SafeLocatorHash then WarningKind.
// The Postgres query already caps results per locator; the grouping here only
// projects the bounded input into operator-friendly shape.
func GroupTerraformStateWarningsByKind(
	rows []TerraformStateLocatorWarning,
) map[string]map[string][]TerraformStateLocatorWarning {
	if len(rows) == 0 {
		return map[string]map[string][]TerraformStateLocatorWarning{}
	}
	grouped := map[string]map[string][]TerraformStateLocatorWarning{}
	for _, row := range rows {
		hash := strings.TrimSpace(row.SafeLocatorHash)
		kind := strings.TrimSpace(row.WarningKind)
		if hash == "" || kind == "" {
			continue
		}
		if _, ok := grouped[hash]; !ok {
			grouped[hash] = map[string][]TerraformStateLocatorWarning{}
		}
		grouped[hash][kind] = append(grouped[hash][kind], row)
	}
	return grouped
}
