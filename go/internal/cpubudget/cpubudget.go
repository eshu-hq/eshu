// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cpubudget

import "runtime"

// UsableCPUs returns the CPU count worker-pool defaults should size against,
// in place of the HOST cpu count runtime.NumCPU() reports.
//
// On Go 1.25+, the runtime itself already sets GOMAXPROCS from the container
// cgroup CPU quota (the containermaxprocs GODEBUG, default-on since Go 1.25):
// inside a cgroup CPU-quota container (Kubernetes resources.limits.cpu,
// Docker --cpus), runtime.GOMAXPROCS(0) already reflects the quota-derived
// core count, not the host's. runtime.NumCPU() does not — it always reports
// the host count — so a worker pool sized off runtime.NumCPU() over-spawns
// relative to what the container can actually schedule. UsableCPUs is a thin
// wrapper over runtime.GOMAXPROCS(0), floored at 1; it is not a
// reimplementation of cgroup reading. See
// TestGoDirectiveSupportsAutomaticGOMAXPROCS for the go.mod version guard
// this relies on.
func UsableCPUs() int {
	n := runtime.GOMAXPROCS(0)
	if n < 1 {
		return 1
	}
	return n
}
