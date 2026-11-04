// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factload

import (
	"sort"
	"testing"
)

// TestWorkloadCandidateValueClassesPartitionSignalKeys fails when a signal
// key has no value rule or two value rules: the probe tests values per class,
// so an unclassified key would silently stop matching and a doubly classified
// key would be tested twice.
func TestWorkloadCandidateValueClassesPartitionSignalKeys(t *testing.T) {
	t.Parallel()

	seen := map[string]int{}
	for _, class := range [][]string{
		WorkloadCandidateArrayBucketKeys,
		WorkloadCandidatePresenceKeys,
		WorkloadCandidateTokenKeys,
	} {
		for _, key := range class {
			seen[key]++
		}
	}
	var classified, all []string
	for key, n := range seen {
		classified = append(classified, key)
		if n != 1 {
			t.Errorf("key %q is in %d value classes, want exactly 1", key, n)
		}
	}
	all = append(all, WorkloadCandidateSignalKeys...)
	sort.Strings(classified)
	sort.Strings(all)
	if len(classified) != len(all) {
		t.Fatalf("value classes hold %v, signal keys are %v", classified, all)
	}
	for i := range all {
		if classified[i] != all[i] {
			t.Fatalf("value classes hold %v, signal keys are %v", classified, all)
		}
	}
}
