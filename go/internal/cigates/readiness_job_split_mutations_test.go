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
	{"missing seeded self-test", "run: " + readinessSelfTestSeeds, "run: true"},
	{"skip seeded self-test step", "        run: " + readinessSelfTestSeeds, "        if: false\n        run: " + readinessSelfTestSeeds},
	{"waive seeded self-test step", "        run: " + readinessSelfTestSeeds, "        continue-on-error: true\n        run: " + readinessSelfTestSeeds},
	{"compound seeded self-test", "run: " + readinessSelfTestSeeds, "run: " + readinessSelfTestSeeds + " || true"},
	{"duplicate seeded self-test", "        run: " + readinessSelfTestSeeds + "\n", "        run: " + readinessSelfTestSeeds + "\n\n      - name: Check self-test seeded violations again\n        run: " + readinessSelfTestSeeds + "\n"},
	{"queue selector reintroduced", "  readiness-plan:\n", "  queue-selection:\n    uses: ./.github/workflows/queue-selection.yml\n  readiness-plan:\n"},
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
	{"added login flag", "    env: {GOCACHE: /tmp/eshu-ci/go-build}\n    services:", "    env: {GOCACHE: /tmp/eshu-ci/go-build, DOCKERHUB_LOGIN_ENABLED: ${{ secrets.DOCKERHUB_USERNAME != '' && secrets.DOCKERHUB_TOKEN != '' }}}\n    services:"},
	{"added login step", "      - name: Set up Go\n        uses: actions/setup-go@v6", "      - name: Log in to Docker Hub\n        if: env.DOCKERHUB_LOGIN_ENABLED == 'true'\n        uses: docker/login-action@v3\n        with:\n          username: ${{ secrets.DOCKERHUB_USERNAME }}\n          password: ${{ secrets.DOCKERHUB_TOKEN }}\n      - name: Set up Go\n        uses: actions/setup-go@v6"},
	{"short hermetic timeout", "timeout-minutes: 30", "timeout-minutes: 20"},
	{"summary cap drifts from timeout", "          CAP_MINUTES: '30'", "          CAP_MINUTES: '45'"},
	{"summary skipped on cancel", "      - name: Summarize hermetic job outcome\n        if: ${{ always() }}", "      - name: Summarize hermetic job outcome\n        if: ${{ success() }}"},
	{"summary ignores status", "          JOB_STATUS: ${{ job.status }}", "          JOB_STATUS: success"},
	{"start step missing", "        run: " + readinessStartRecord, "        run: true"},
	{"start script before checkout", "        run: " + readinessStartRecord, "        run: bash " + readinessSummaryScript + " start"},
	{"start record skipped", "        run: " + readinessStartRecord, "        if: false\n        run: " + readinessStartRecord},
	{"summary fails when script absent", "          if [ ! -f \"$script\" ]; then\n", "          if false; then\n"},
	{"summary exits nonzero when script absent", "            exit 0\n", "            exit 1\n"},
	{"summary classifier failure fails job", "bash \"$script\" finish || echo", "bash \"$script\" finish && echo"},
	{"summary waived by continue-on-error", "        env:\n          JOB_STATUS:", "        continue-on-error: true\n        env:\n          JOB_STATUS:"},
	{"summary classifier test missing", "        run: bash scripts/ci/test-live-postgres-readiness-job-summary.sh", "        run: true"},
	{"hermetic runner CPU missing", "      - name: Record hermetic runner CPU\n        run: " + readinessRunnerCPU, "      - name: Record hermetic runner CPU\n        run: true"},
	{"shard runner CPU missing", "      - name: Record runner CPU\n        run: " + readinessRunnerCPU, "      - name: Record runner CPU\n        run: true"},
	{"shard start step missing", "      - name: Record shard job start\n        run: " + readinessStartRecord, "      - name: Record shard job start\n        run: true"},
	{"shard summary skipped on cancel", "      - name: Summarize shard job outcome\n        if: ${{ always() }}", "      - name: Summarize shard job outcome\n        if: ${{ success() }}"},
	{"shard summary cap drifts from timeout", "          READINESS_JOB_LABEL: " + readinessShardLabel + "\n          CAP_MINUTES: '30'", "          READINESS_JOB_LABEL: " + readinessShardLabel + "\n          CAP_MINUTES: '45'"},
	{"shard summary unnamed", "          READINESS_JOB_LABEL: " + readinessShardLabel, "          READINESS_JOB_LABEL: hermetic"},
	{"shard summary not last", "\n\n  live-postgres-readiness:\n", "\n      - name: Trailing step\n        run: true\n\n  live-postgres-readiness:\n"},
	{"changed shard timeout", "    timeout-minutes: 30\n    # Leg wall is compile-dominated", "    timeout-minutes: 31\n    # Leg wall is compile-dominated"},
	{"changed aggregate timeout", "timeout-minutes: 10\n    steps:\n      - name: Require every shard to succeed", "timeout-minutes: 11\n    steps:\n      - name: Require every shard to succeed"},
	{"missing standby cleanup", "run: bash scripts/ci/live-postgres-standby-fixture.sh stop", "run: true # bash scripts/ci/live-postgres-standby-fixture.sh stop"},
	{"skipped standby cleanup", "      - name: Stop disposable physical standby\n        if: " + readinessCleanupSelection, "      - name: Stop disposable physical standby\n        if: false"},
	{"fixture without shard suffix", "run_attempt }}-${{ matrix.shard }}'", "run_attempt }}'"},
	{"unplanned shard matrix", "matrix: ${{ fromJSON(needs.readiness-plan.outputs.matrix) }}", "matrix: ${{ fromJSON('{}') }}"},
	{"fail-fast shards", "fail-fast: false", "fail-fast: true"},
	{"aggregate skips shards", "needs: [readiness-plan, " + readinessShardJob + "]", "needs: readiness-plan"},
	{"missing coverage verify", "python3 scripts/lib/live_postgres_readiness_shards.py verify", "true"},
	{"missing shard download", "actions/download-artifact@v4", "actions/download-artifact@v3"},
	{"missing shard upload", "          path: readiness-shard-report.json", "          path: readiness-shard-report.json.missing"},
	{"debug upload without failure gate", "      - name: Upload failed-package event streams\n        if: ${{ failure() }}", "      - name: Upload failed-package event streams\n        if: ${{ always() }}"},
	{"debug upload wrong path", "path: .readiness-debug/", "path: .other-debug/"},
	{"debug upload wrong name", "name: readiness-debug-${{ github.run_id }}", "name: readiness-shard-${{ github.run_id }}"},
}

var readinessServiceMutations = []struct {
	name, old, replacement string
}{
	{"postgres mirror changed", "image: ghcr.io/eshu-hq/ci-postgres-bookworm@", "image: docker.io/library/postgres@"},
	{"neo4j mirror changed", "image: ghcr.io/eshu-hq/ci-neo4j-community@", "image: docker.io/library/neo4j@"},
	{"postgres credentials added", "      postgres:\n        image:", "      postgres:\n        credentials: ${{ secrets.DOCKERHUB_TOKEN }}\n        image:"},
}
