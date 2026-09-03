// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "github.com/eshu-hq/eshu/go/internal/reducer/iamcan"

// This file is the reducer root's compatibility surface for the IAM
// CAN_ASSUME / CAN_PERFORM edge family, which moved to [iamcan] (issue #6061).
// The four aliases below are the family's exported wiring contract: the
// reducer command constructs the writers and the cypher package implements
// them, so their root spelling stays put. Root-internal call sites (the
// additive-domain registries and the INVOKES_CLOUD_ACTION intent builder) name
// [iamcan] directly instead of going through a forwarder.

// IAMCanAssumeEdgeWriter is the root spelling of
// [iamcan.IAMCanAssumeEdgeWriter].
type IAMCanAssumeEdgeWriter = iamcan.IAMCanAssumeEdgeWriter

// IAMCanAssumeMaterializationHandler is the root spelling of
// [iamcan.IAMCanAssumeMaterializationHandler].
type IAMCanAssumeMaterializationHandler = iamcan.IAMCanAssumeMaterializationHandler

// IAMCanPerformEdgeWriter is the root spelling of
// [iamcan.IAMCanPerformEdgeWriter].
type IAMCanPerformEdgeWriter = iamcan.IAMCanPerformEdgeWriter

// IAMCanPerformMaterializationHandler is the root spelling of
// [iamcan.IAMCanPerformMaterializationHandler].
type IAMCanPerformMaterializationHandler = iamcan.IAMCanPerformMaterializationHandler

// IAMCanAssumeNodesNotReadyFailureClass is the root spelling of
// [iamcan.IAMCanAssumeNodesNotReadyFailureClass]. internal/storage/postgres
// names it when it classifies a queue row's readiness-gate miss.
const IAMCanAssumeNodesNotReadyFailureClass = iamcan.IAMCanAssumeNodesNotReadyFailureClass

// IAMCanPerformNodesNotReadyFailureClass is the root spelling of
// [iamcan.IAMCanPerformNodesNotReadyFailureClass].
const IAMCanPerformNodesNotReadyFailureClass = iamcan.IAMCanPerformNodesNotReadyFailureClass
