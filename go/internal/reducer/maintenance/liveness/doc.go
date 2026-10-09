// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package liveness re-drives active generations that wedge past their
// activation deadline.
//
// [Runner] sweeps beside normal reducer intent processing: an active
// generation that makes no forward progress past
// canonical-nodes-committed within the activation deadline is re-driven
// through projector re-enqueue rather than left active indefinitely,
// while generations whose blocking shared-intent domain queues are still
// progressing inside the policy's progress window are skipped as draining
// (#7265), and orphaned older actives are superseded. It logs each
// re-driven [Recovery] and never logs skipped draining generations
// individually; the draining gauge bucket is their signal.
package liveness
