// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crossplane

// SatisfiedBySnapshotLoaderForTest exposes crossplaneSatisfiedByFactLoader to
// the external wiring test, which imports storage/postgres: an internal test
// cannot, because storage/postgres imports this package through the reducer.
type SatisfiedBySnapshotLoaderForTest = crossplaneSatisfiedByFactLoader
