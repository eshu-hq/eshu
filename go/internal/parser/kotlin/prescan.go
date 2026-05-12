package kotlin

import "github.com/eshu-hq/eshu/go/internal/parser/shared"

// PreScan returns Kotlin names used by the collector import-map pre-scan.
func PreScan(repoRoot string, path string) ([]string, error) {
	payload, err := Parse(repoRoot, path, false, shared.Options{})
	if err != nil {
		return nil, err
	}
	return shared.DedupeNonEmptyStrings(shared.CollectBucketNames(payload, "functions", "classes", "interfaces")), nil
}
