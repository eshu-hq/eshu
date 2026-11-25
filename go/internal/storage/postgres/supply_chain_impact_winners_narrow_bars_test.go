package postgres_test

import "testing"

// TestNarrowWriteBarSeededRedGreen is the committed RED/GREEN pair for the
// two Box 1 bars: seeded inputs that must trip each comparator (RED side)
// alongside inputs that must pass (GREEN side). A comparator that cannot
// fail — or one broken to pass everything — fails this test.
func TestNarrowWriteBarSeededRedGreen(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name            string
		shipped, shape  int64
		deadlocks       int
		wantBar, wantDL bool
	}{
		{"over-budget shape trips the 4x bar", 100, 26, 0, false, true},
		{"shape at exactly one quarter passes", 100, 25, 0, true, true},
		{"shape well under passes", 1647, 29, 0, true, true},
		{"zero shipped touches trips (blind measurement)", 0, 29, 0, false, true},
		{"zero shape touches passes the ratio", 100, 0, 0, true, true},
		{"nonzero deadlocks trip the zero bar", 1647, 29, 3, true, false},
		{"zero deadlocks pass", 1647, 29, 0, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := narrowWriteBarPasses(tc.shipped, tc.shape); got != tc.wantBar {
				t.Errorf("narrowWriteBarPasses(%d, %d) = %v, want %v", tc.shipped, tc.shape, got, tc.wantBar)
			}
			if got := narrowDeadlocksPass(tc.deadlocks); got != tc.wantDL {
				t.Errorf("narrowDeadlocksPass(%d) = %v, want %v", tc.deadlocks, got, tc.wantDL)
			}
		})
	}
}
