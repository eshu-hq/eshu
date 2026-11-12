// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"go.opentelemetry.io/otel/attribute"
)

// Fan-out read sites (#10564 H): the closed set naming which read of the
// supply-chain impact holder fan-out failed. The holder lookup is one
// statement per site entry here (the H3 pair-union), so the three lookup
// arms share the holders site.
const (
	// FanOutReadSiteHolders is the CVE/advisory/member holder lookup.
	FanOutReadSiteHolders = "holders"
	// FanOutReadSiteInFlight is the claimed-or-running queue-state read.
	FanOutReadSiteInFlight = "in_flight"
	// FanOutReadSiteManifestGeneration is the #9874 manifest generation read.
	FanOutReadSiteManifestGeneration = "manifest_generation"
	// FanOutReadSitePackage is the #9874/#10061 package-keyed holder read.
	FanOutReadSitePackage = "package"
	// FanOutReadSiteRepository is the #10059 repository-keyed holder read.
	FanOutReadSiteRepository = "repository"
)

// AttrFanOutReadSite returns a site attribute for the holder fan-out
// counter's read outcomes. v must be one of the FanOutReadSite* constants.
func AttrFanOutReadSite(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionSite, v)
}
