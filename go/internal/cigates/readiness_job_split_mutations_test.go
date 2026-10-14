// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

// Seeded violations for the readiness workflow shape gate. Each entry must
// fail checkReadinessSplit; a passing entry means the gate lost a pin.
var readinessRegistryMutations = []struct {
	name string
	edit func(*Gate)
}{
	{"drop hermetic check", func(g *Gate) { g.CI.CheckNames = g.CI.CheckNames[1:] }},
	{"drop live check", func(g *Gate) { g.CI.CheckNames = g.CI.CheckNames[:1] }},
	{"promote staged gate", func(g *Gate) { g.Blocking = true }},
}

var readinessWorkflowMutations = []struct {
	name, old, replacement string
}{
	{"missing hermetic", "run: bash scripts/test-run-live-postgres-readiness-tests.sh", "run: true"},
	{"missing shard runner", "run: " + readinessShardRunner, "run: true"},
	{"missing shard contract", "run: " + readinessShardContract, "run: true"},
	{"missing queue selector", "    uses: ./.github/workflows/queue-selection.yml", "    uses: actions/checkout@v5"},
	{"wrong hermetic selection", "        if: " + readinessQueueSelection + "\n        run: bash scripts/test-run-live-postgres-readiness-tests.sh", "        if: " + readinessUnemittedSelection + "\n        run: bash scripts/test-run-live-postgres-readiness-tests.sh"},
	{"wrong shard selection", "      - name: Run live readiness proofs\n        if: " + readinessQueueSelection, "      - name: Run live readiness proofs\n        if: " + readinessUnemittedSelection},
	{"skip hermetic", "  hermetic:\n", "  hermetic:\n    if: false\n"},
	{"waive shards", "  " + readinessShardJob + ":\n", "  " + readinessShardJob + ":\n    continue-on-error: true\n"},
	{"waive aggregate", "  " + readinessAggregateJob + ":\n", "  " + readinessAggregateJob + ":\n    continue-on-error: true\n"},
	{"compound shard runner", "run: " + readinessShardRunner, "run: " + readinessShardRunner + " || true"},
	{"unsharded runner", "run: " + readinessShardRunner, "run: bash scripts/run-live-postgres-readiness-tests.sh"},
	{"missing push path", "      - 'go/internal/cigates/readiness_job_split_test.go'\n", ""},
	{"missing shard path", "      - 'scripts/lib/live_postgres_readiness_shards.py'\n", ""},
	{"skip hermetic proof step", "        run: bash scripts/test-run-live-postgres-readiness-tests.sh", "        if: false\n        run: bash scripts/test-run-live-postgres-readiness-tests.sh"},
	{"waive hermetic proof step", "        run: bash scripts/test-run-live-postgres-readiness-tests.sh", "        continue-on-error: true\n        run: bash scripts/test-run-live-postgres-readiness-tests.sh"},
	{"skip shard proof step", "        run: " + readinessShardRunner, "        if: false\n        run: " + readinessShardRunner},
	{"waive shard proof step", "        run: " + readinessShardRunner, "        continue-on-error: true\n        run: " + readinessShardRunner},
	{"added login flag", "    timeout-minutes: 17\n    services:", "    timeout-minutes: 17\n    env:\n      DOCKERHUB_LOGIN_ENABLED: ${{ secrets.DOCKERHUB_USERNAME != '' && secrets.DOCKERHUB_TOKEN != '' }}\n    services:"},
	{"added login step", "        with:\n          persist-credentials: false\n\n      - name: Set up Go", "        with:\n          persist-credentials: false\n      - name: Log in to Docker Hub\n        if: env.DOCKERHUB_LOGIN_ENABLED == 'true'\n        uses: docker/login-action@v3\n        with:\n          username: ${{ secrets.DOCKERHUB_USERNAME }}\n          password: ${{ secrets.DOCKERHUB_TOKEN }}\n      - name: Set up Go"},
	{"short hermetic timeout", "timeout-minutes: 30", "timeout-minutes: 20"},
	{"changed shard timeout", "timeout-minutes: 17", "timeout-minutes: 18"},
	{"changed aggregate timeout", "timeout-minutes: 10", "timeout-minutes: 11"},
	{"missing standby cleanup", "run: bash scripts/ci/live-postgres-standby-fixture.sh stop", "run: true # bash scripts/ci/live-postgres-standby-fixture.sh stop"},
	{"skipped standby cleanup", "        if: " + readinessCleanupSelection, "        if: false"},
	{"fixture without shard suffix", "run_attempt }}-${{ matrix.shard }}'", "run_attempt }}'"},
	{"fifth shard", "shard: [1, 2, 3, 4]", "shard: [1, 2, 3, 4, 5]"},
	{"fail-fast shards", "fail-fast: false", "fail-fast: true"},
	{"aggregate skips shards", "needs: [queue-selection, " + readinessShardJob + "]", "needs: queue-selection"},
	{"missing coverage verify", "python3 scripts/lib/live_postgres_readiness_shards.py verify", "true"},
	{"missing shard download", "actions/download-artifact@v4", "actions/download-artifact@v3"},
	{"missing shard upload", "          path: readiness-shard-report.json", "          path: readiness-shard-report.json.missing"},
}

var readinessServiceMutations = []struct {
	name, old, replacement string
}{
	{"postgres mirror changed", "image: ghcr.io/eshu-hq/ci-postgres-bookworm@", "image: docker.io/library/postgres@"},
	{"neo4j mirror changed", "image: ghcr.io/eshu-hq/ci-neo4j-community@", "image: docker.io/library/neo4j@"},
	{"postgres credentials added", "      postgres:\n        image:", "      postgres:\n        credentials: ${{ secrets.DOCKERHUB_TOKEN }}\n        image:"},
}
