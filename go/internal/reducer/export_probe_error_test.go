// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

// ClassifyWorkloadCandidateProbeErrorForTest exposes the probe error
// classifier to the external reducer_test package so a real driver error can
// be classified without a reducer-to-postgres import.
var ClassifyWorkloadCandidateProbeErrorForTest = classifyWorkloadCandidateProbeError
