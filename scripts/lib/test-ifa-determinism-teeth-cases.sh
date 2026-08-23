#!/usr/bin/env bash
# shellcheck disable=SC2154
# --teeth (#4396 slice 6) case module for scripts/test-verify-ifa-determinism.sh,
# extracted so that mirror stays under the repository's 500-line cap (mirroring
# the other sourced case-module splits in that file). `fail`, `require_code`,
# `require_lib`, `repo_root`, and `script` all come from the parent.
run_ifa_determinism_teeth_cases() {
	# --teeth: the acceptance clause's negative-path proof that the matrix
	# catches a deliberately non-idempotent write, built behind a Go build tag
	# so it never ships in a normal/CI/production binary.
	# Both of these bind the LOAD-BEARING line, not the bare word. Each word also
	# appears in this gate's own log/die message strings, which are code, so the
	# earlier `-ge 1` form on the bare word stayed satisfied by a message with the
	# real thing gone: deleting the `--teeth) teeth=1 ;;` parser left the gate
	# unable to enter teeth mode at all, and misspelling the build tag left it
	# building without the injected non-idempotent write -- both GREEN (#6161).
	# A needle that occurs more than once needs the occurrence that DOES the work.
	require_code "--teeth flag is parsed" "--teeth) teeth=1 ;;"
	require_code "teeth build tag is assigned" 'build_tags="ifadeterminismteeth"'
	require_code "teeth threads tags through every build call" 'ifa_det_build_bin "${bin_dir}" reducer "${build_tags}"'
	# Bind the log CALL, not the phrase: the die() two lines below says "see the
	# TEETH: CAUGHT line above", which is live code, so the marker operators
	# actually grep for could be renamed with this pin still green (#6161).
	require_code "teeth caught framing" 'log "TEETH: CAUGHT'
	require_code "teeth-not-caught is its own failure" "TEETH FAILED"
	require_code "teeth still forbids lowering N" "lower N, retry, or otherwise normalize this away"
	require_lib "build_bin accepts an optional tags argument" 'local bin_dir="$1" cmd="$2" tags="${3:-}"'
	require_lib "tags become -tags args only when non-empty" 'tag_args=(-tags "${tags}")'

	# The build-tag-gated fault itself must exist exactly where the script's own
	# doc says it does, and must not be reachable without the tag.
	local teeth_reducer_on teeth_reducer_off teeth_cypher_on teeth_cypher_off f
	teeth_reducer_on="${repo_root}/go/internal/reducer/gcp_resource_materialization_teeth.go"
	teeth_reducer_off="${repo_root}/go/internal/reducer/gcp_resource_materialization_teeth_off.go"
	teeth_cypher_on="${repo_root}/go/internal/storage/cypher/cloud_resource_node_writer_teeth.go"
	teeth_cypher_off="${repo_root}/go/internal/storage/cypher/cloud_resource_node_writer_teeth_off.go"
	for f in "${teeth_reducer_on}" "${teeth_reducer_off}" "${teeth_cypher_on}" "${teeth_cypher_off}"; do
		[[ -f "${f}" ]] || fail "missing teeth build-tag file: ${f}"
	done
	rg --fixed-strings --quiet -- '//go:build ifadeterminismteeth' "${teeth_reducer_on}" \
		|| fail "${teeth_reducer_on} must carry the ifadeterminismteeth build tag"
	rg --fixed-strings --quiet -- '//go:build !ifadeterminismteeth' "${teeth_reducer_off}" \
		|| fail "${teeth_reducer_off} must carry the !ifadeterminismteeth build tag"
	rg --fixed-strings --quiet -- '//go:build ifadeterminismteeth' "${teeth_cypher_on}" \
		|| fail "${teeth_cypher_on} must carry the ifadeterminismteeth build tag"
	rg --fixed-strings --quiet -- '//go:build !ifadeterminismteeth' "${teeth_cypher_off}" \
		|| fail "${teeth_cypher_off} must carry the !ifadeterminismteeth build tag"
}
