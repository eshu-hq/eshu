// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package failure

// ReplayGenerationSupersededClass labels projector work a replay left terminal
// or refused because its scope generation is superseded (#7130). The recovery
// store records it as the eshu_dp_superseded_generation_fence_total
// failure_class for a replay or drain that skips such rows, and the admin
// replay handler uses it as the failure_class of its 422 refusal and of the
// same counter. Both surfaces share this one constant so the metric label, the
// refusal body, and the docs cannot drift apart.
const ReplayGenerationSupersededClass = "projector_replay_generation_superseded"
