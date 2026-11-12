// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import "time"

// defaultComponentClaimLeaseTTL is the extracted worker's lease fallback
// when ESHU_COMPONENT_COLLECTOR_CLAIM_LEASE_TTL is unset. It is longer
// than the shared workflow default (60s) because satellites run paced
// multi-minute walks: the ruled 600s NVD envelope covers the measured
// >=546s walk floor of the 4566-record / 92-page head window
// (eshu-hq/eshu#10446), and the heartbeat fallback (20s) stays coherent
// under it. Live deployments set the env explicitly per activation.
const defaultComponentClaimLeaseTTL = 600 * time.Second
