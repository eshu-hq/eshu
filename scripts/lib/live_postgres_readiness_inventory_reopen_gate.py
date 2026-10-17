#!/usr/bin/env python3
"""Deployable-unit reopen gate live PostgreSQL readiness proofs (#8283).

The changed-evidence gate's maintenance-pass, success-write, differential,
and concurrency proofs live here; live_postgres_readiness_records.py merges
them into PACKAGES, as it does the own-gate and config-fence siblings. A
package missing from PACKAGES gets nothing merged, so an emptied PACKAGES
stays empty and still fails closed.
"""

from live_postgres_readiness_inventory import STORAGE_PACKAGE

_PREFIX = "go/internal/storage/postgres/ingestion_reopen_correlation_fingerprint"

REOPEN_GATE_PACKAGES = {
    STORAGE_PACKAGE: {
        f"{_PREFIX}_live_test.go": (
            "TestDeployableUnitReopenFingerprintGateLive",
            "TestDeployableUnitReopenFingerprintKeepsReadSnapshotLive",
        ),
        f"{_PREFIX}_write_live_test.go": (
            "TestDeployableUnitReopenFingerprintWriteIsClaimFencedLive",
            "TestDeployableUnitReopenFingerprintWriteIsMonotonicLive",
            "TestDeployableUnitReopenFingerprintCascadesWithWorkItemLive",
        ),
        f"{_PREFIX}_differential_live_test.go": (
            "TestDeployableUnitReopenFingerprintDifferentialLive",
            "TestDeployableUnitReopenFingerprintZeroCandidateLive",
            "TestDeployableUnitReopenFingerprintGatesTargetedPassLive",
        ),
        f"{_PREFIX}_concurrency_live_test.go": (
            "TestDeployableUnitReopenFingerprintConcurrentPassesLive",
            "TestDeployableUnitReopenFingerprintWriteRacesReclaimLive",
            "TestDeployableUnitReopenFingerprintWriteRacesRetentionLive",
        ),
    },
}
