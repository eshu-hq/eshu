// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import goruntime "runtime"

// hostSeedPlatform returns the executing host's GOOS/GOARCH pair, the only
// platform slot satellite seed admission consults (#9877). Production reaches it
// through seedPlatformSource and passes the result to
// loadRuntimeConfigObserved; tests pass an explicit platform instead so the
// admission rule does not depend on the machine running them (#10739).
func hostSeedPlatform() string {
	return goruntime.GOOS + "/" + goruntime.GOARCH
}

// seedPlatformSource is the platform source the production loaders call. It is
// a variable so serial tests can stub it to a platform no seed pin lists and
// prove the loader asks it, instead of hardcoding a pinned platform. A
// hardcoded linux/amd64 would otherwise stay green on a linux/amd64 host
// (#10739). Production never reassigns it.
var seedPlatformSource = hostSeedPlatform
