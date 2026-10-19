#!/usr/bin/env python3
"""Readiness enrollment for ./internal/storage/postgres (generated, data only).

Do not edit by hand; regenerate with
`python3 scripts/generate-live-postgres-readiness.py`.
"""

TABLE = {
    "./internal/storage/postgres": {
        "go/internal/storage/postgres/workflow_control_claim_depth_live_test.go": (
            "TestWorkflowClaimDepthEightClaimersDrainExactlyOnce",
            "TestWorkflowClaimDepthFairRotationServesSmallKeysEarly",
            "TestClaimDepthAnalysisHelpers",
        ),
        "go/internal/storage/postgres/workflow_control_claim_order_plan_live_test.go": (
            "TestWorkflowClaimCandidateUsesFairClaimIndex",
        ),
        "go/internal/storage/postgres/workflow_control_claim_pacing_depth_live_test.go": (
            "TestWorkflowClaimPacingRecordsDepthPendingGauge",
        ),
        "go/internal/storage/postgres/workflow_control_claim_pacing_failure_live_test.go": (
            "TestWorkflowClaimPacingFailsOpenOnLatchQueryFailure",
            "TestWorkflowClaimPacingStuckHoldsMidBandAndReleasesBelowWatermark",
            "TestWorkflowClaimPacingStuckEmitsOperatorSignals",
        ),
        "go/internal/storage/postgres/workflow_control_claim_pacing_live_test.go": (
            "TestWorkflowClaimPacingEngagesOnDomainDepth",
            "TestWorkflowClaimPacingHysteresisRelease",
            "TestWorkflowClaimPacingFailsOpenOnDepthQueryFailure",
            "TestWorkflowClaimPacingStuckSignal",
            "TestWorkflowClaimPacingGlobalCeiling",
            "TestWorkflowClaimPacingIdleDrainHoldsFloor",
            "TestWorkflowClaimPacingEngagedDrainCompletesExactlyOnce",
            "TestWorkflowClaimPacingSwitchPreservesEngagedAt",
            "TestWorkflowClaimPacingDepthCacheReuse",
        ),
        "go/internal/storage/postgres/workflow_control_fair_claim_live_test.go": (
            "TestWorkflowFairClaimLotteryMatchesSimulatedDraw",
            "TestWorkflowFairClaimPriorityPrecedesRotation",
            "TestWorkflowFairnessKeyDepthsReportsTopKeysAndDistinctCount",
            "TestWorkflowClaimDepthStaggeredStartHoldsGates",
        ),
    },
}
