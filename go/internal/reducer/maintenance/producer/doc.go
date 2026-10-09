// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package producer settles the producer-activation obligations
// ProjectorQueue.Ack writes (#7635).
//
// [Runner] workers claim one obligation at a time through [Store] and
// settle it: the dependent consumer items reopen and the obligation
// completes under the claim fence; one worker per process also runs a
// bounded prune and the census gauges each cycle. The production store
// is postgres.ProducerActivationRunnerStore, wired by cmd/reducer when
// ESHU_PRODUCER_ACTIVATION_CONSUMER_ENABLED is true. There is no
// catch-up: a generation whose consumers already replayed is
// indistinguishable from one that never did, so a catch-up would re-owe
// every pruned generation.
package producer
