#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$SHIM_LOG"
if [[ "${ESHU_QUERY_METHODOLOGY_REQUIRED:-}" == 1 && ! -s "${ESHU_QUERY_METHODOLOGY_IDENTITY:-}" ]]; then
 printf 'go producer started without frozen required methodology identity\n' >&2
 exit 15
fi
if [[ "$*" == *'-list '* ]]; then
 pattern=""
 while [[ "$#" -gt 0 ]]; do
  if [[ "$1" == '-list' ]]; then pattern="$2"; break; fi
  shift
 done
 for test in TestMethodologyProductionSourceTreesMatchBase TestMethodologyProductionTreeRejectsBaseOnlyMigration TestMethodologyCombinedCheckoutUsesIntegratedBase TestMethodologyRequiredProductionVariants TestHandlerQueryplanManifestBindsProductionBuilders TestLegacyQueryplanManifestBindsProductionQueries TestQueryplanProfileFlagsUnboundedVarLength TestPilotArtifactFiles TestMethodologyEmptyGrantHTTPDoesNotReadGraph TestMethodologyRequestQueryCountBudget TestMethodologyGraphStatementOracle TestMethodologyCycleEdgeOracleKeepsDirectionalCandidates TestMethodologyGraphOracleEmptyPagesOnPopulatedFixture TestMethodologyGraphStatementCapturePreservesExtraColumn TestMethodologyProfileMetricsRequireRawCounters TestMethodologyPlanOmitsUnavailableOptionalMetrics TestMethodologyScratchGitIgnoresAmbientTrace TestMethodologyPostgresWorkUsesInclusiveRootCounters TestMethodologyPostgresWorkRequiresSixRootCounters TestMethodologyPostgresStatementRowsIncludeSelectedColumns; do
  [[ "$test" =~ $pattern ]] || continue
  [[ "${SHIM_OMIT_TREE_GUARD:-}" != "$test" ]] || continue
  printf '%s\n' "$test"
 done
 exit 0
fi
case "$*" in
 *'test ./internal/queryplan -count=1'*)
  if [[ "${SHIM_OMIT:-}" != coverage ]]; then printf '{"families":[]}\n' > "$ESHU_QUERY_METHODOLOGY_COVERAGE"; fi ;;
 *'-run ^TestQueryMethodologyPostgresLive$'*)
  if [[ -n "${SHIM_HANG_MARK:-}" ]]; then printf '%s\n' "$PPID" > "$SHIM_HANG_MARK"; sleep 3; fi
  [[ -n "$ESHU_POSTGRES_TEST_DSN" && -n "$ESHU_QUERY_METHODOLOGY_POSTGRES_IMAGE" ]] || exit 11
  [[ "${SHIM_FAIL:-}" != postgres ]] || exit 12
  if [[ "${SHIM_OMIT:-}" != postgres ]]; then printf '{"engine":"postgresql"}\n' > "$ESHU_QUERY_METHODOLOGY_POSTGRES_ARTIFACT"; fi ;;
 *'-run ^TestImportDependencyMethodologyLive$'*)
  [[ "$ESHU_QUERY_METHODOLOGY_LIVE" == 1 && "$ESHU_QUERYPLAN_PROFILE_ISOLATED" == 1 && "$ESHU_QUERY_METHODOLOGY_CALIBRATED" == 1 ]] || exit 13
  printf '{"engine":"neo4j"}\n' > "$ESHU_QUERY_METHODOLOGY_GRAPH_ARTIFACT"
  if [[ "${SHIM_OMIT:-}" != graph-cases ]]; then printf '{"cases":[]}\n' > "$ESHU_QUERY_METHODOLOGY_REPORT"; fi ;;
 *'-run ^TestImportDependencyMethodologyMCPTerminalCapLive$'*)
  [[ -n "$ESHU_NEO4J_URI" ]] || exit 14 ;;
esac
if [[ "$*" == *'-json '* && "$*" =~ -run\ \^([A-Za-z0-9_]+)\$ ]]; then
 root_name="${BASH_REMATCH[1]}"
 if [[ "${SHIM_EVENT:-}" != zero ]]; then
  printf '{"Action":"run","Test":"%s"}\n' "$root_name"
  printf '{"Action":"%s","Test":"%s"}\n' "${SHIM_EVENT:-pass}" "$root_name"
 fi
fi
