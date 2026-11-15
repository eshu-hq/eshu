// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import goruntime "runtime"

// hostSeedPlatform returns the executing host's GOOS/GOARCH pair, the only
// platform slot satellite seed admission consults (#9877). Production passes
// it to loadRuntimeConfigObserved; tests pass an explicit platform instead so
// the admission rule does not depend on the machine running them (#10739).
func hostSeedPlatform() string {
	return goruntime.GOOS + "/" + goruntime.GOARCH
}
