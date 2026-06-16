package reducer

// SelectionPhaseDurations records bounded selector subphase timings for
// shared-projection runners that need to diagnose candidate-page outliers.
type SelectionPhaseDurations struct {
	CandidateLoadSeconds      float64
	AcceptancePrefetchSeconds float64
	ReadinessPrefetchSeconds  float64
	RefreshFenceCheckSeconds  float64
}
