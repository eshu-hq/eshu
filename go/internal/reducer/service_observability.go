package reducer

import "time"

// reducerQueueWaitSeconds returns how long reducer work was visible before a
// worker started executing it. Negative or missing timestamps are clamped so
// clock skew and legacy rows do not pollute latency histograms.
func reducerQueueWaitSeconds(start time.Time, availableAt time.Time) float64 {
	if availableAt.IsZero() {
		return 0
	}
	wait := start.Sub(availableAt)
	if wait < 0 {
		return 0
	}
	return wait.Seconds()
}
