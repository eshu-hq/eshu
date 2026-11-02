#!/usr/bin/env bash
# Split-doc ("parts") regression cases for test-verify-telemetry-coverage.sh
# (#9789).
#
# Sourced, never executed: it runs in the caller's shell and reuses the
# caller's init_repo(), expect_pass(), expect_fail(), run_verifier(),
# record_pass(), record_fail(), and expect_fail_with() from the multi-file
# cases. Extracted to keep scripts/test-verify-telemetry-coverage.sh under the
# repo's 500-line cap.
#
# #9789 split docs/public/observability/telemetry-coverage.md into an index
# plus sibling part files (telemetry-coverage-<area>.md). The verifier must
# read the index AND every part. A verifier that kept reading only the index
# would pass a bogus metric or a stale row planted in a part, and would report
# every registered metric whose row moved to a part as undocumented. Each
# negative case below is a seeded violation that lives ONLY in a part.

# make_parts_repo <name> moves the fixture's stage table out of the index and
# into telemetry-coverage-reducer.md, and links that part from the index.
# Writes the repo path to stdout.
make_parts_repo() {
  local dir obs
  dir="$(init_repo "$1")"
  obs="${dir}/docs/public/observability"
  {
    printf '# Telemetry Coverage Reducer Stages\n\n'
    printf 'Part of the [Telemetry Coverage Contract](telemetry-coverage.md).\n\n'
    rg -N '^\|' "${obs}/telemetry-coverage.md"
  } >"${obs}/telemetry-coverage-reducer.md"
  {
    rg -N -v '^\|' "${obs}/telemetry-coverage.md"
    printf '\n- [Reducer Stages](telemetry-coverage-reducer.md)\n'
  } >"${obs}/telemetry-coverage.md.new"
  mv "${obs}/telemetry-coverage.md.new" "${obs}/telemetry-coverage.md"
  git -C "${dir}" add .
  git -C "${dir}" commit -q -m "split coverage doc into index and part"
  printf '%s\n' "${dir}"
}

parts_part() {
  printf '%s/docs/public/observability/telemetry-coverage-reducer.md' "$1"
}

# Case P1: index plus one part, every registered metric documented only in
# the part. Must pass: the part's rows count as documented.
case_parts_clean="$(make_parts_repo case-parts-clean)"
if rg -q 'eshu_dp_' "${case_parts_clean}/docs/public/observability/telemetry-coverage.md"; then
  record_fail "parts fixture must keep every metric row out of the index"
else
  expect_pass "passes when every stage row lives in a linked part file" "${case_parts_clean}"
fi

# Case P2 (seeded bogus metric): a part row names an unregistered metric.
case_parts_bogus="$(make_parts_repo case-parts-bogus)"
printf '| ghost stage | go/internal/reducer/service.go | `eshu_dp_ghost_part_metric_total` | reducer runtime |\n' \
  >>"$(parts_part "${case_parts_bogus}")"
git -C "${case_parts_bogus}" add .
git -C "${case_parts_bogus}" commit -q -m "seed bogus metric in part"
expect_fail_with "fails when a part names an unregistered metric" \
  "${case_parts_bogus}" 'eshu_dp_ghost_part_metric_total'

# Case P3 (seeded renamed registered metric): instruments.go renames a metric
# whose row lives in a part, and the part keeps the old name. Both directions
# must fire: the old name is no longer registered, the new name is undocumented.
case_parts_renamed="$(make_parts_repo case-parts-renamed)"
renamed_inst="${case_parts_renamed}/go/internal/telemetry/instruments.go"
sed -i.bak 's/eshu_dp_reducer_run_duration_seconds/eshu_dp_reducer_run_seconds/' "${renamed_inst}"
rm -f "${renamed_inst}.bak"
git -C "${case_parts_renamed}" add .
git -C "${case_parts_renamed}" commit -q -m "rename registered metric without doc update"
expect_fail_with "fails when a registered metric documented in a part is renamed (stale name)" \
  "${case_parts_renamed}" 'doc references metric `eshu_dp_reducer_run_duration_seconds`'
expect_fail_with "fails when a registered metric documented in a part is renamed (new name)" \
  "${case_parts_renamed}" 'registers `eshu_dp_reducer_run_seconds`'

# Case P4: a part row names a file that does not exist. (3b) must read parts.
case_parts_stale="$(make_parts_repo case-parts-stale)"
printf '| stale stage | go/internal/reducer/does_not_exist.go | `eshu_dp_queue_claim_duration_seconds` | reducer runtime |\n' \
  >>"$(parts_part "${case_parts_stale}")"
git -C "${case_parts_stale}" add .
git -C "${case_parts_stale}" commit -q -m "seed stale row in part"
expect_fail_with "fails when a part row names a missing file" \
  "${case_parts_stale}" 'does_not_exist.go'

# Case P5: a new stage file covered only by a part row. (3) must read parts.
case_parts_new_stage="$(make_parts_repo case-parts-new-stage)"
printf 'package reducer\n' >"${case_parts_new_stage}/go/internal/reducer/part_stage.go"
printf '| part stage | go/internal/reducer/part_stage.go | `eshu_dp_queue_claim_duration_seconds` | reducer runtime |\n' \
  >>"$(parts_part "${case_parts_new_stage}")"
git -C "${case_parts_new_stage}" add .
git -C "${case_parts_new_stage}" commit -q -m "add stage covered by part row"
expect_pass "passes when a new stage file is covered by a part row" "${case_parts_new_stage}"

# Case P6: a part file the index does not link. An unlinked part is invisible
# to readers who start at the index, so the verifier must refuse it.
case_parts_orphan="$(make_parts_repo case-parts-orphan)"
orphan_index="${case_parts_orphan}/docs/public/observability/telemetry-coverage.md"
sed -i.bak '/telemetry-coverage-reducer\.md/d' "${orphan_index}"
rm -f "${orphan_index}.bak"
git -C "${case_parts_orphan}" add .
git -C "${case_parts_orphan}" commit -q -m "unlink part from index"
expect_fail_with "fails when a part file is not linked from the index" \
  "${case_parts_orphan}" 'telemetry-coverage-reducer.md is not linked from'

# Case P7: histogram bucket section in a part. Agreement must pass and a
# documented set the code lacks must fail.
case_parts_buckets="$(make_parts_repo case-parts-buckets)"
cat >>"${case_parts_buckets}/go/internal/telemetry/instruments.go" <<'GO'
	if _, err := meter.Float64Histogram(
		"eshu_dp_test_histogram_seconds",
		metric.WithExplicitBucketBoundaries(1, 10, 100),
	); err != nil {
		return nil, err
	}
GO
{
  printf '| test histogram | go/internal/telemetry/instruments.go | `eshu_dp_test_histogram_seconds` | test |\n\n'
  printf '<!-- eshu:metric:section=histogram-buckets -->\n## Histogram Bucket Boundaries\n\n'
  printf '| set_name | boundary_values |\n| --- | --- |\n| test-histogram-seconds | 1, 10, 100 |\n'
} >>"$(parts_part "${case_parts_buckets}")"
git -C "${case_parts_buckets}" add .
git -C "${case_parts_buckets}" commit -q -m "add bucket section to part"
expect_pass "passes when a part's bucket section agrees with code" "${case_parts_buckets}"
printf '| ghost-set-seconds | 2, 20, 200 |\n' >>"$(parts_part "${case_parts_buckets}")"
git -C "${case_parts_buckets}" add .
git -C "${case_parts_buckets}" commit -q -m "seed bucket set the code lacks"
expect_fail_with "fails when a part documents a bucket set the code lacks" \
  "${case_parts_buckets}" '2,20,200'

# Case P8: telemetry-coverage-notes.md is prose, not a part. It needs no index
# link and its contents are not parsed as stage rows.
case_parts_notes="$(make_parts_repo case-parts-notes)"
printf '# Notes\n\n| not | a | stage | row |\n' \
  >"${case_parts_notes}/docs/public/observability/telemetry-coverage-notes.md"
git -C "${case_parts_notes}" add .
git -C "${case_parts_notes}" commit -q -m "add unlinked notes page"
expect_pass "passes when the notes page is unlinked and holds a non-stage table" "${case_parts_notes}"
