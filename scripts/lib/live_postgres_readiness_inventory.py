#!/usr/bin/env python3
"""Authoritative package and test inventory for live PostgreSQL readiness."""

import re

IMPACT_PACKAGE = "./internal/query/supply/chain/impact"
STORAGE_PACKAGE = "./internal/storage/postgres"
ACTIVATION_PACKAGE = "./internal/storage/postgres/activation"
MAINTENANCE_PACKAGE = "./internal/storage/postgres/maintenance"
MEMBERSHIP_PACKAGE = "./internal/storage/postgres/membership"
REDUCER_PACKAGE = "./cmd/reducer"
QUERY_PACKAGE = "./internal/query"
SUMMARY_PACKAGE = "./internal/storage/postgres/status/summary"
WRITER_PACKAGE = "./internal/reducer/status/summary"
ADMIN_PACKAGE = "./internal/query/admin"
REACHABILITY_PACKAGE = "./internal/storage/postgres/code/reachability"

# Expected files and tests per Go package (relative to the go/ module root).
# The runner invokes one go test per package, each with its own events file
# and package terminal event.
PACKAGES = {
    IMPACT_PACKAGE: {
        "go/internal/query/supply/chain/impact/readiness_package_manifest_repo_scope_live_test.go": (
            "TestSupplyChainImpactReadinessPackageManifestRepoScopeQueryPlanLive",
            "TestSupplyChainImpactReadinessRepoArmScopeLive",
        ),
        "go/internal/query/supply/chain/impact/readiness_container_identity_live_test.go": (
            "TestSupplyChainImpactReadinessMutableRefIncludesEveryCurrentDigestLive",
        ),
        "go/internal/query/supply/chain/impact/readiness_package_consumption_scope_live_test.go": (
            "TestSupplyChainImpactReadinessPackageConsumptionScopeLive",
        ),
        "go/internal/query/supply/chain/impact/runtime_environment_store_live_test.go": (
            "TestRuntimeEnvironmentEvidenceHotDigestUsesArtifactIndexLive",
            "TestRuntimeEnvironmentEvidenceCurrentAuthorizedTruthMatrixLive",
            "TestRuntimeEnvironmentEvidenceManyPairsStayFactFirstLive",
        ),
        "go/internal/query/supply/chain/impact/readiness_scan_tier_explain_live_test.go": (
            "TestSupplyChainImpactReadinessScanTierQueryPlanLive",
            "TestSupplyChainImpactReadinessScanTierOSPackageCountDoesNotFanOutLive",
        ),
    },
    STORAGE_PACKAGE: {
        "go/internal/storage/postgres/projector_marked_write_age_live_test.go": (
            "TestQueueObserverStoreProjectorMarkedWriteOldestAgeLive",
            "TestQueueObserverStoreProjectorMarkedWriteOldestAgeEmptyLive",
        ),
        "go/internal/storage/postgres/projector_queue_claim_marked_guard_contention_live_test.go": (
            "TestProjectorClaimMarkedGuardContention",
        ),
        "go/internal/storage/postgres/projector_queue_claim_marked_guard_epq_live_test.go": (
            "TestProjectorClaimSweepSkipsGenerationLockedByMarkerTxn",
            "TestProjectorClaimDropsHolderClaimedAfterSnapshot",
            "TestProjectorClaimGuardAgreesWithFenceAcrossClocks",
            "TestProjectorClaimMarkerSweepRace",
        ),
        "go/internal/storage/postgres/projector_queue_claim_marked_guard_interleave_live_test.go": (
            "TestProjectorClaimRetryAgainstNewerInterleave",
        ),
        "go/internal/storage/postgres/projector_queue_claim_marked_guard_live_test.go": (
            "TestProjectorClaimHoldsNewerBehindInvisibleMarkedRetry",
            "TestProjectorClaimRunsVisibleMarkedRetryBeforeNewer",
            "TestProjectorClaimStillSupersedesUnmarkedStaleGeneration",
            "TestProjectorClaimClaimsNewerBehindTerminalMarkedGeneration",
            "TestProjectorAckKeepsMarkedObsoleteGeneration",
            "TestProjectorAckStillSupersedesUnmarkedObsoleteGeneration",
        ),
        "go/internal/storage/postgres/projector_queue_claim_marked_guard_plan_live_test.go": (
            "TestProjectorClaimMarkedGuardPlanShape",
            "TestProjectorClaimMarkedGuardPlanShapeAtScale",
            "TestProjectorClaimMarkedGuardLockSet",
        ),
        "go/internal/storage/postgres/projector_queue_claim_full_guard_contention_live_test.go": (
            "TestProjectorClaimFullGuardContention",
        ),
        "go/internal/storage/postgres/projector_queue_claim_full_guard_epq_live_test.go": (
            "TestProjectorClaimDropsFullHolderClaimedAfterSnapshot",
            "TestProjectorClaimFullGuardAgreesWithFenceAcrossClocks",
        ),
        "go/internal/storage/postgres/projector_queue_claim_full_guard_live_test.go": (
            "TestProjectorClaimFullSurvivesNewerDelta",
            "TestProjectorClaimFailedFullStillHoldsDelta",
            "TestProjectorClaimHoldsDeltaBehindBackoffFull",
            "TestProjectorClaimRunsDriftedFullBeforeNewerDelta",
            "TestProjectorClaimStillSupersedesFullBehindNewerFull",
            "TestProjectorClaimStillSupersedesDeltaBehindNewerDelta",
            "TestProjectorClaimClaimsDeltaBehindTerminalFull",
            "TestProjectorAckStillSupersedesFullBehindAckedDelta",
        ),
        "go/internal/storage/postgres/projector_queue_claim_full_guard_plan_live_test.go": (
            "TestProjectorClaimFullGuardPlanShape",
            "TestProjectorClaimFullGuardPlanShapeAtScale",
            "TestProjectorClaimFullGuardLockSet",
        ),
        "go/internal/storage/postgres/projector_queue_ack_obsolete_marker_live_test.go": (
            "TestProjectorAckSeesLockTimeMarkerTruth",
            "TestProjectorAckMarkerCommitRace",
        ),
        "go/internal/storage/postgres/projector_queue_claim_marker_fence_live_test.go": (
            "TestMarkProjectionWriteStartedFenceSync",
            "TestMarkProjectionWriteStartedMissingFenceRefuses",
        ),
        "go/internal/storage/postgres/drifted_bucket_skip_live_test.go": (
            "TestDriftedPathologicalBucketSkippedLive",
        ),
        "go/internal/storage/postgres/ingestion_targeted_maintenance_terminal_live_test.go": (
            "TestActivationObligationInapplicableCollisionLoserLive",
        ),
        "go/internal/storage/postgres/package_manifest_consumption_backfill_live_test.go": (
            "TestPackageManifestConsumptionBackfillRepairsOldWriterAfterReadyLive",
            "TestPackageManifestConsumptionBackfillPagesHeavyScopeLive",
            "TestPackageManifestConsumptionBackfillWaitsForScopeWriterLive",
            "TestPackageManifestConsumptionBackfillBoundsTwentyFiveScopePassLive",
            "TestPackageManifestConsumptionBackfillConcurrentPassesAreIdempotentLive",
        ),
        "go/internal/storage/postgres/generation_retention_hard_ceiling_live_test.go": (
            "TestGenerationRetentionHardCeilingLive",
        ),
        "go/internal/storage/postgres/trigger_bootstrap_parity_live_test.go": (
            "TestWebhookTriggerStoreRunsOnBootstrapSchemaLive",
        ),
        "go/internal/storage/postgres/generation_retention_large_fixture_live_test.go": (
            "TestGenerationRetentionStoreLargeFixtureLive",
        ),
        "go/internal/storage/postgres/generation_retention_relock_live_test.go": (
            "TestGenerationRetentionPrescreenSkipsOverFactGenerationsLive",
            "TestGenerationRetentionPrescreenProbesFactIndexLive",
            "TestGenerationRetentionTargetedLockPlanShapeLive",
            "TestGenerationRetentionTargetedLockEvalPlanQualDropsRacedMembersLive",
        ),
        "go/internal/storage/postgres/package_manifest_consumption_migration_upgrade_live_test.go": (
            "TestPackageManifestConsumptionMigrationsUpgradeAfterSecretLinesLive",
        ),
        "go/internal/storage/postgres/prefetch_batch_plan_live_test.go": (
            "TestPrefetchBatchQueriesUsePrimaryKeyLive",
        ),
        "go/internal/storage/postgres/ingestion_flux_evidence_identity_live_test.go": (
            "TestIngestionStoreCommitScopeGenerationPersistsFluxEvidenceNatively",
            "TestFluxEvidenceMixedGenerationLegacyAndCurrentCoexist",
        ),
        "go/internal/storage/postgres/ingestion_targeted_maintenance_differential_live_test.go": (
            "TestTargetedMaintenanceMatchesWholePass",
        ),
        "go/internal/storage/postgres/ingestion_targeted_maintenance_interleave_live_test.go": (
            "TestTargetedMaintenanceInterleavingsMatchWholePass",
        ),
        "go/internal/storage/postgres/ingestion_targeted_maintenance_outcomes_live_test.go": (
            "TestTargetedMaintenanceOutcomesMatchWholePass",
        ),
        "go/internal/storage/postgres/ingestion_targeted_maintenance_sql_live_test.go": (
            "TestBoundedRepositoryGenerationReadsMatchShippedRead",
        ),
        "go/internal/storage/postgres/targeted_partition_evidence_theory_live_test.go": (
            "TestTheoryExactPartitionEvidenceClosure",
        ),
        "go/internal/storage/postgres/container_image_identity_epoch_barrier_live_test.go": (
            "TestContainerImageIdentityEpochBarrierDefersPendingLive",
            "TestContainerImageIdentityActivationEpochMissIsSentinelLive",
        ),
        "go/internal/storage/postgres/projector_queue_zombie_heal_graph_live_test.go": (
            "TestProjectorZombieHealRestoresCanonicalNodesLive",
        ),
        "go/internal/storage/postgres/projector_queue_zombie_heal_live_test.go": (
            "TestProjectorRefusalHealsMarkedSupersededGeneration",
            "TestProjectorRepeatedRefusalsOpenOneActiveRow",
            "TestProjectorConcurrentHealsOpenOneActiveRow",
            "TestProjectorRefusalSkipsHealWithoutWriteMarker",
            "TestProjectorSupersededByNewerSkipsHeal",
            "TestProjectorRefusalSkipsHealWithoutActiveGeneration",
            "TestProjectorRefusalSkipsHealWhenActiveRowInFlight",
        ),
        "go/internal/storage/postgres/active_code_call_symbols_ahead_manifest_live_test.go": (
            "TestReducerContentionGateActiveCodeCallSymbolLoaderAheadManifestKeepsProducer",
            "TestReducerContentionGateActiveCodeCallSymbolLoaderSupersedeThenDeltaHole",
            "TestReducerContentionGateActiveCodeCallSymbolLoaderDirtyNonProducerStaysGated",
        ),
        "go/internal/storage/postgres/recovery_refinalize_delta_active_live_test.go": (
            "TestRefinalizeDeltaActiveRequestsFullReindex",
            "TestRefinalizeDeltaActiveConvergesAcrossTwoCalls",
        ),
        "go/internal/storage/postgres/recovery_refinalize_delta_active_fence_live_test.go": (
            "TestRefinalizeDeltaActiveRollsBackReindexWithTheTransaction",
            "TestRefinalizeDeltaActiveReindexLockContention",
        ),
        "go/internal/storage/postgres/content_files_generation_tag_live_test.go": (
            "TestReducerContentionGateContentGenerationTagAheadWriteReadsDirty",
            "TestReducerContentionGateContentGenerationTagActivatedTagReadsClean",
            "TestReducerContentionGateContentGenerationTagManifestLessScopeWithSignalReadsDirty",
            "TestReducerContentionGateContentGenerationTagDangledAndNullTagReadDirty",
            "TestReducerContentionGateContentWriterStampsGenerationTag",
            "TestReducerContentionGateContentFilesBackfillAttributesOnlyCleanScopes",
            "TestReducerContentionGateContentGenerationTagGoModDirtLegs",
            "TestReducerContentionGateContentWriterBlankGenerationStoresNull",
        ),
    },
    ACTIVATION_PACKAGE: {
        "go/internal/storage/postgres/activation/ack_live_test.go": (
            "TestActivationObligationAtomicAckLive",
        ),
        "go/internal/storage/postgres/activation/claim_live_test.go": (
            "TestActivationObligationClaimSkipsLockedRowsLive",
            "TestActivationObligationClaimIgnoresFinishedRowsLive",
            "TestActivationObligationFinalizeLeaseExpiryRollsBackLive",
        ),
        "go/internal/storage/postgres/activation/composed_live_test.go": (
            "TestActivationObligationConsumerAndEpochPassOverlapLive",
            "TestActivationObligationReplicasLive",
        ),
        "go/internal/storage/postgres/activation/consumer_live_test.go": (
            "TestActivationObligationConsumerOrderingLive",
            "TestActivationObligationConsumerLateFailureLive",
            "TestActivationObligationConsumerClaimRecoveryLive",
        ),
        "go/internal/storage/postgres/activation/lease_restart_live_test.go": (
            "TestActivationObligationLeaseExpiresMidMaintenanceLive",
            "TestActivationObligationRestartBeforePhasePublicationLive",
        ),
        "go/internal/storage/postgres/activation/matrix_live_test.go": (
            "TestActivationObligationConsumerQueueIsolationLive",
            "TestActivationObligationConsumerTerminalPreservedLive",
            "TestActivationObligationConsumerRollbackAndIdentityLive",
            "TestActivationObligationConsumerNoPhaseSubstitutionLive",
            "TestActivationObligationConsumerSupersessionLive",
            "TestActivationObligationWakeIsNotStarvedByOtherClassRowsLive",
        ),
        "go/internal/storage/postgres/activation/recovery_live_test.go": (
            "TestActivationObligationConsumerRestartAndDuplicatesLive",
            "TestActivationObligationConsumerWakeBatchCapLive",
            "TestActivationObligationCatchUpLive",
            "TestActivationObligationCatchUpReowesObsoleteOfActiveLive",
        ),
        "go/internal/storage/postgres/activation/redelivery_live_test.go": (
            "TestActivationObligationRedeliveryAndCatchUpRacesLive",
            "TestActivationObligationSupersessionBetweenClaimAndFinalizeLive",
        ),
        "go/internal/storage/postgres/activation/retention_live_test.go": (
            "TestActivationObligationRetentionCascadeLive",
            "TestActivationObligationPruneLive",
            "TestActivationObligationPruneIsNotStarvedByInapplicableRowsLive",
        ),
        "go/internal/storage/postgres/activation/scope_lock_live_test.go": (
            "TestActivationObligationIngestionCommitRacesFinalizeLive",
        ),
        "go/internal/storage/postgres/activation/targeted_live_test.go": (
            "TestActivationObligationRealCatalogChangeIsHeldThenCompletedLive",
            "TestActivationObligationRealCollisionLoserIsInapplicableLive",
            "TestActivationObligationBlockedMaintenanceIsCancelledBeforeTheLeaseLive",
        ),
        "go/internal/storage/postgres/activation/terminal_live_test.go": (
            "TestActivationObligationInapplicableWithoutRepositoryFactLive",
            "TestActivationObligationCatalogChangedIsHeldLive",
            "TestActivationObligationNullActivePointerIsObsoleteLive",
            "TestActivationObligationRetireInapplicableIsFencedLive",
            "TestActivationObligationInapplicableRetireIsLeaseFencedLive",
        ),
        "go/internal/storage/postgres/activation/quiet_generation_live_test.go": (
            "TestQuietGenerationActivatesAfterMaintenanceSnapshotLive",
            "TestQuietGenerationActivatesWithControlArmMaintenanceLive",
        ),
        "go/internal/storage/postgres/activation/producer_obligation_live_test.go": (
            "TestProducerActivationQuietAckLeavesConsumerUnreplayedLive",
        ),
        "go/internal/storage/postgres/activation/producer_obligation_contract_live_test.go": (
            "TestProducerActivationSettleIsExactlyOnceLive",
            "TestProducerActivationLeaseFencesStaleSettleLive",
            "TestProducerActivationUnrelatedScopeIsNotReopenedLive",
            "TestProducerActivationSettleMatchesEpochPassLive",
            "TestProducerActivationPruneAndStatsLive",
            "TestProducerActivationDriftReopenLive",
            "TestProducerActivationRetentionCascadeLive",
            "TestProducerActivationInsertConflictBranchesLive",
        ),
    },
    MAINTENANCE_PACKAGE: {
        "go/internal/storage/postgres/maintenance/requests_live_test.go": (
            "TestStatusRequestStoreRequestReindexWatermarkMonotonicLive",
        ),
        "go/internal/storage/postgres/maintenance/repository_reindex_live_test.go": (
            "TestRepositoryReindexStoreWatermarksLive",
        ),
    },
    MEMBERSHIP_PACKAGE: {
        "go/internal/storage/postgres/membership/known_scopes_live_test.go": (
            "TestKnownScopesHostFilterLive",
        ),
        "go/internal/storage/postgres/membership/live_read_live_test.go": (
            "TestLiveFilterParityLive",
        ),
        "go/internal/storage/postgres/membership/observations_live_test.go": (
            "TestObservationStoreLive",
        ),
        "go/internal/storage/postgres/membership/observations_sweep_live_test.go": (
            "TestObservationStoreSweepLive",
        ),
    },
    REDUCER_PACKAGE: {
        "go/cmd/reducer/package_manifest_backfill_live_test.go": (
            "TestPackageManifestBackfillOnlyOneCandidateOwnsPass",
            "TestPackageManifestBackfillDoesNotStarveSingleConnectionPool",
        ),
    },
    QUERY_PACKAGE: {
        "go/internal/query/code_topic_fleet_endpoint_live_test.go": (
            "TestCodeTopicFleetCapacityEndpointPostgresLive",
        ),
        "go/internal/query/content_reader_search_unscoped_live_test.go": (
            "TestSearchFilesUnscopedMatchesOldStatementLive",
            "TestSearchFilesUnscopedCancelledTailResumesToExactAnswerLive",
            "TestSearchFilesUnscopedEdgeRowsAreExactLive",
            "TestSearchFilesUnscopedTailFilledPageKeepsMoreLive",
            "TestUnscopedSearchPlanShapesLive",
        ),
        "go/internal/query/content_reader_dead_code_incoming_bound_live_test.go": (
            "TestDeadCodeIncomingEntityIDsActiveRunBoundLive",
            "TestCrossRepoDeadCodeConsumerCoverageLive",
        ),
        "go/internal/query/content_reader_dead_code_refresh_probe_live_test.go": (
            "TestCrossRepoDeadCodeConsumerCoverageRefreshIntentLive",
        ),
        "go/internal/query/content_reader_dead_code_root_paths_live_test.go": (
            "TestCrossRepoDeadCodeConsumerRootPathsLive",
        ),
        "go/internal/query/status_terraform_selection_live_test.go": (
            "TestStatusRoutesTerraformSelectionLive",
        ),
    },
    SUMMARY_PACKAGE: {
        "go/internal/storage/postgres/status/summary/store_live_test.go": (
            "TestStatusSummaryMissingTableLive",
            "TestStatusSummaryMigrationLive",
            "TestStatusSummaryGuardLive",
            "TestStatusSummaryConcurrentWritersLive",
            "TestStatusSummaryCrashSafetyLive",
            "TestStatusSummaryWriterLockLive",
        ),
        "go/internal/storage/postgres/status/summary/conflict_live_test.go": (
            "TestStatusSummaryConflictWaitLive",
        ),
        "go/internal/storage/postgres/status/summary/bloat_live_test.go": (
            "TestStatusSummaryBloatLive",
            "TestStatusSummaryBloatIncompressibleLive",
        ),
        "go/internal/storage/postgres/status/summary/bloat_terraform_live_test.go": (
            "TestStatusSummaryBloatTerraformLive",
            "TestStatusSummaryBloatTerraformOpsQaScaleLive",
            "TestStatusSummaryBloatTerraformWorstCaseLive",
        ),
        "go/internal/storage/postgres/status/summary/select_live_test.go": (
            "TestStatusSummarySelectLive",
        ),
    },
    WRITER_PACKAGE: {
        "go/internal/reducer/status/summary/runner_live_test.go": (
            "TestWriterRowEqualsLiveStatementLive",
            "TestWriterKilledMidPassKeepsTheOldRowLive",
            "TestWriterCountsAGuardRejectionLive",
            "TestWriterSkipsAMissingTableLive",
            "TestWriterReplacesARowFromAnotherStatementLive",
            "TestWriterPassRunsReadCommittedLive",
            "TestSecondWriterSkipsWhileTheFirstHoldsTheLockLive",
        ),
        "go/internal/reducer/status/summary/contention_live_test.go": (
            "TestWritersBesideTheProductionClaimLoopLive",
        ),
        "go/internal/reducer/status/summary/reader_live_test.go": (
            "TestReaderServesWhatTheWriterStoredEqualToLiveLive",
            "TestReaderFallsBackAndNeverMixesWhenTheRowIsStaleLive",
        ),
        "go/internal/reducer/status/summary/scrape_live_test.go": (
            "TestScrapeServesTheStoredRowAndNeverTheLiveStatementLive",
            "TestScrapeStatementInventoryOnAnEmptyStoreLive",
            "TestScrapeBytesEqualWithTheOmittedSectionsPopulatedLive",
        ),
        "go/internal/reducer/status/summary/terraform_live_test.go": (
            "TestTerraformModelServedEqualToLiveLive",
            "TestWriterKilledMidCompanionKeepsThePrimaryRowLive",
        ),
    },
    ADMIN_PACKAGE: {
        "go/internal/query/admin/reopen_live_test.go": (
            "TestAdminHandler_ReopenLive",
        ),
        "go/internal/query/admin/scope_selector_live_test.go": (
            "TestAdminHandler_ScopeSelectorCollisionLive",
        ),
        "go/internal/query/admin/reopen_rollover_live_test.go": (
            "TestAdminHandler_ReopenRolloverActsOnCurrentGenerationLive",
        ),
    },
    REACHABILITY_PACKAGE: {
        "go/internal/storage/postgres/code/reachability/loader_edges_scope_live_test.go": (
            "TestLoadCodeReachabilityEdgesReadsConsumerScopeOnly",
        ),
    },
}
EXPECTED = {
    path: tests
    for package_expected in PACKAGES.values()
    for path, tests in package_expected.items()
}
TOTAL_TESTS = sum(len(tests) for tests in EXPECTED.values())


def package_records() -> list[tuple[str, list[str]]]:
    """Return (package, expected tests) per package, failing closed on bad data.

    PACKAGES is the single source of truth for the runner's package list and
    its -run patterns, so an empty or malformed table must stop the run rather
    than let it pass with fewer packages.
    """
    if not isinstance(PACKAGES, dict) or not PACKAGES:
        raise ValueError("PACKAGES is empty")
    records = []
    for package, files in PACKAGES.items():
        if not isinstance(package, str) or not package.startswith("./"):
            raise ValueError(f"malformed package path: {package!r}")
        if not isinstance(files, dict) or not files:
            raise ValueError(f"{package}: no expected files")
        tests: list[str] = []
        for path, names in files.items():
            if not isinstance(names, (tuple, list)) or not names:
                raise ValueError(f"{package}: {path}: no expected tests")
            for name in names:
                if not isinstance(name, str) or not re.fullmatch(r"Test\w+", name):
                    raise ValueError(f"{package}: {path}: malformed test {name!r}")
                if name in tests:
                    raise ValueError(f"{package}: duplicate test {name}")
                tests.append(name)
        records.append((package, tests))
    return records


def neo4j_tests() -> list[str]:
    """Return the enrolled tests that need a Neo4j proof backend.

    These are the tests carried by `*_graph_live_test.go` files: they open
    a Bolt driver and self-skip without one. The runner excuses exactly
    these (and only these) when no Neo4j env is configured.
    """
    tests: set[str] = set()
    for files in PACKAGES.values():
        for path, names in files.items():
            if path.endswith("_graph_live_test.go"):
                tests.update(names)
    return sorted(tests)
