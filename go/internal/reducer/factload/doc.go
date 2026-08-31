// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package factload holds the reducer's scoped fact-loading seam: the FactLoader
// port every handler reads facts through, the optional kind- and
// payload-value-filtering extensions a store may implement, and the retry
// classification applied to a load failure.
//
// Loading is pushed down to the store when it can filter. When it cannot, the
// loader falls back to the full FactLoader contract and returns the WHOLE scope
// generation unfiltered — it does not filter in process; the calling domain
// handler does. That fallback is the reason the optional extensions are
// interfaces rather than methods on FactLoader: a store that cannot filter still
// satisfies the port.
//
// ClassifyFactLoadError marks a transport or availability failure retryable and
// leaves everything else terminal, so a reducer intent that cannot read its
// facts retries rather than dead-lettering on a transient outage.
package factload
