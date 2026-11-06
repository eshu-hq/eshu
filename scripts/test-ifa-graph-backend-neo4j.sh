#!/usr/bin/env bash
# Guard: the Ifá gates (determinism, dead-letter matrix, fault-injection, and the
# scheduled proofs) run on Neo4j, the supported graph backend. Owner order
# 2026-11-05 retired the NornicDB variant from CI; this guard fails if a workflow
# or gate script is wired to NornicDB again.
#
# It checks three things on the real tree (GREEN) and then proves each check can
# fail by planting one violation per check in a scratch copy (RED):
#   1. the determinism workflow pins ESHU_GRAPH_BACKEND: neo4j at the top level;
#   2. no active (non-comment) line of a watched workflow or gate script names
#      NornicDB (service, image, database, backend value, or restart target)
#      or the NornicDB compose file (docker-compose.yaml, which has no neo4j
#      service), and every compose project command in a watched workflow names
#      docker-compose.neo4j.yml (a project command that names the wrong file
#      loses the Neo4j log and teardown on the failure path);
#   3. semantic_entity_containment asserts the canonical-ownership .nornicdb.json
#      expected-edge sibling on Neo4j (and on a manual NornicDB run) and falls
#      back to the base file only when the sibling is absent;
#   4. scripts/lib/ifa_graph_backend.sh defaults to Neo4j, maps it to the neo4j
#      service, database and compose file, still supports a manual nornicdb run,
#      and rejects an unknown backend.
#
# To restore NornicDB in CI, change the workflow env value, list the nornicdb
# image pin in the scheduled runner, and drop the matching checks here.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# Files whose active lines must not name NornicDB. scripts/lib/ifa_graph_backend.sh
# is excluded on purpose: it owns the manual nornicdb branch.
watched=(
	.github/workflows/ifa-determinism-gate.yml
	.github/workflows/ifa-scheduled-proofs.yml
	scripts/verify-ifa-determinism.sh
	scripts/verify-ifa-dead-letter-matrix.sh
	scripts/verify-ifa-fault-injection.sh
	scripts/run-ifa-scheduled-proofs.sh
	scripts/lib/ifa_determinism_common.sh
	scripts/lib/ifa_determinism_lifecycle.sh
	scripts/lib/ifa_fault_injection_driver.sh
	scripts/lib/ifa_fault_injection_common.sh
	scripts/lib/ifa_fault_injection_cells.sh
	scripts/lib/ifa_repo_dependency_live.sh
	scripts/lib/ifa_deployable_unit_live.sh
)

fail() {
	printf 'test-ifa-graph-backend-neo4j: %s\n' "$*" >&2
	exit 1
}

# check_tree ROOT prints one line per violation and returns non-zero when any
# exist. It never exits, so the RED cases can inspect its output.
check_tree() {
	local root="$1" file count=0 hits
	if ! rg --quiet --fixed-strings -- '  ESHU_GRAPH_BACKEND: neo4j' "${root}/.github/workflows/ifa-determinism-gate.yml" 2>/dev/null; then
		echo ".github/workflows/ifa-determinism-gate.yml: top-level env must set 'ESHU_GRAPH_BACKEND: neo4j'"
		count=$((count + 1))
	fi
	for file in "${watched[@]}"; do
		if [[ ! -f "${root}/${file}" ]]; then
			echo "${file}: watched file is missing"
			count=$((count + 1))
			continue
		fi
		hits="$(rg -n -i -- 'nornic' "${root}/${file}" | rg -v -- '^[0-9]+:[[:space:]]*#' || true)"
		if [[ -n "${hits}" ]]; then
			echo "${file}: active line names NornicDB: $(printf '%s' "${hits}" | head -1)"
			count=$((count + 1))
		fi
		# The NornicDB compose file never contains the word nornic in its name,
		# so the check above cannot see it.
		hits="$(rg -n -- 'docker-compose\.yaml' "${root}/${file}" | rg -v -- '^[0-9]+:[[:space:]]*#' || true)"
		if [[ -n "${hits}" ]]; then
			echo "${file}: active line names the NornicDB compose file docker-compose.yaml: $(printf '%s' "${hits}" | head -1)"
			count=$((count + 1))
		fi
		case "${file}" in
		.github/workflows/*)
			# Every compose project command (logs, down, ps, ...) must name the
			# Neo4j compose file. A bare scripts/ci/compose.sh line without -f
			# is also wrong: compose then falls back to the default file.
			hits="$(rg -n -- 'scripts/ci/compose\.sh' "${root}/${file}" | rg -v -- '^[0-9]+:[[:space:]]*#' | rg -v -- 'docker-compose\.neo4j\.yml' || true)"
			if [[ -n "${hits}" ]]; then
				echo "${file}: compose project command does not name docker-compose.neo4j.yml: $(printf '%s' "${hits}" | head -1)"
				count=$((count + 1))
			fi
			;;
		esac
	done
	if ! rg --quiet --fixed-strings -- ': "${ESHU_GRAPH_BACKEND:=neo4j}"' "${root}/scripts/lib/ifa_graph_backend.sh" 2>/dev/null; then
		echo "scripts/lib/ifa_graph_backend.sh: ESHU_GRAPH_BACKEND must default to neo4j"
		count=$((count + 1))
	fi
	[[ "${count}" -eq 0 ]]
}

# check_backend_lib ROOT runs the library and compares what it exports.
check_backend_lib() {
	local root="$1" out
	out="$(env -u ESHU_GRAPH_BACKEND bash -c 'source "$1"; ifa_graph_backend_init; echo "$ESHU_GRAPH_BACKEND $IFA_GRAPH_SERVICE $IFA_GRAPH_DATABASE $IFA_GRAPH_COMPOSE_FILE"; ifa_graph_compose_file /x' _ \
		"${root}/scripts/lib/ifa_graph_backend.sh" 2>&1)" || { echo "backend lib default run failed: ${out}"; return 1; }
	[[ "${out}" == $'neo4j neo4j neo4j docker-compose.neo4j.yml\n/x/compose/docker-compose.neo4j.yml' ]] \
		|| { echo "backend lib default is not the Neo4j profile: ${out}"; return 1; }
	out="$(ESHU_GRAPH_BACKEND=nornicdb bash -c 'source "$1"; ifa_graph_backend_init; echo "$IFA_GRAPH_SERVICE $IFA_GRAPH_DATABASE $IFA_GRAPH_COMPOSE_FILE"' _ \
		"${root}/scripts/lib/ifa_graph_backend.sh" 2>&1)" || { echo "backend lib nornicdb run failed: ${out}"; return 1; }
	[[ "${out}" == 'nornicdb nornic docker-compose.yaml' ]] \
		|| { echo "manual nornicdb profile drifted: ${out}"; return 1; }
	if ESHU_GRAPH_BACKEND=memgraph bash -c 'source "$1"; ifa_graph_backend_init' _ \
		"${root}/scripts/lib/ifa_graph_backend.sh" >/dev/null 2>&1; then
		echo "backend lib accepted the unsupported backend 'memgraph'"
		return 1
	fi
	return 0
}

# check_containment_selection ROOT sources the live-gate library from ROOT with a
# fake eshu-ifa that records the -expected argument, and compares which fixture
# file ifa_semantic_entity_containment_assert hands to assert-edges. Neo4j keeps
# canonical ownership of canonical-entity CONTAINS edges since #10138, so it must
# select the .nornicdb.json sibling, not the all-semantic base file.
check_containment_selection() {
	local root="$1" work backend got want base
	work="$(mktemp -d -t ifa-containment-select.XXXXXX)"
	mkdir -p "${work}/bin" "${work}/fx"
	printf '#!/usr/bin/env bash\nwhile [[ $# -gt 0 ]]; do [[ "$1" == -expected ]] && { printf "%%s\\n" "$2"; exit 0; }; shift; done\nexit 1\n' >"${work}/bin/eshu-ifa"
	chmod +x "${work}/bin/eshu-ifa"
	base="${work}/fx/family-expected-edges.json"
	: >"${base}"
	for backend in neo4j nornicdb; do
		: >"${base%.json}.nornicdb.json"
		want="${base%.json}.nornicdb.json"
		got="$(ESHU_GRAPH_BACKEND="${backend}" bash -c 'source "$1"; ifa_semantic_entity_containment_assert x "$2" "$3"' _ \
			"${root}/scripts/lib/ifa_direct_family_live.sh" "${work}/bin" "${base}" 2>&1 | tail -1)"
		[[ "${got}" == "${want}" ]] || { echo "containment fixture on ${backend}: asserted '${got}', want the .nornicdb.json sibling '${want}'"; rm -rf "${work}"; return 1; }
	done
	rm -f "${base%.json}.nornicdb.json"
	got="$(ESHU_GRAPH_BACKEND=neo4j bash -c 'source "$1"; ifa_semantic_entity_containment_assert x "$2" "$3"' _ \
		"${root}/scripts/lib/ifa_direct_family_live.sh" "${work}/bin" "${base}" 2>&1 | tail -1)"
	[[ "${got}" == "${base}" ]] || { echo "containment fixture without a sibling: asserted '${got}', want the base file '${base}'"; rm -rf "${work}"; return 1; }
	rm -rf "${work}"
	return 0
}

# --- GREEN: the real tree passes ---
output="$(check_tree "${repo_root}")" || fail "the tree is wired to NornicDB:
${output}"
output="$(check_backend_lib "${repo_root}")" || fail "${output}"
output="$(check_containment_selection "${repo_root}")" || fail "${output}"

# --- RED: each check fails on a planted violation ---
scratch="$(mktemp -d -t ifa-graph-backend-guard.XXXXXX)"
trap 'rm -rf "${scratch}"' EXIT
for file in "${watched[@]}" scripts/lib/ifa_graph_backend.sh scripts/lib/ifa_direct_family_live.sh; do
	mkdir -p "${scratch}/base/$(dirname "${file}")"
	cp "${repo_root}/${file}" "${scratch}/base/${file}"
done

# plant NAME FILE SED_EXPRESSION EXPECTED_SUBSTRING: copy the base tree, apply one
# mutation, and require check_tree to fail with the expected text.
plant() {
	local name="$1" file="$2" expression="$3" expected="$4" case_root output
	case_root="${scratch}/${name}"
	cp -R "${scratch}/base" "${case_root}"
	sed -i.bak -e "${expression}" "${case_root}/${file}"
	rm -f "${case_root}/${file}.bak"
	if cmp -s "${scratch}/base/${file}" "${case_root}/${file}"; then
		fail "RED case ${name}: the mutation changed nothing (stale guard)"
	fi
	if output="$(check_tree "${case_root}")"; then
		fail "RED case ${name}: check_tree passed a planted violation"
	fi
	[[ "${output}" == *"${expected}"* ]] \
		|| fail "RED case ${name}: wrong failure, wanted '${expected}', got: ${output}"
}

plant workflow-backend-flipped .github/workflows/ifa-determinism-gate.yml \
	's/^  ESHU_GRAPH_BACKEND: neo4j$/  ESHU_GRAPH_BACKEND: nornicdb/' "top-level env must set"
plant workflow-backend-removed .github/workflows/ifa-determinism-gate.yml \
	'/^  ESHU_GRAPH_BACKEND: neo4j$/d' "top-level env must set"
plant script-service-hardcoded scripts/verify-ifa-determinism.sh \
	's/-- up -d "${IFA_GRAPH_SERVICE}" postgres/-- up -d nornicdb postgres/' "verify-ifa-determinism.sh: active line names NornicDB"
plant script-restart-hardcoded scripts/lib/ifa_fault_injection_common.sh \
	's/-- restart "${IFA_GRAPH_SERVICE}"/-- restart nornicdb/' "ifa_fault_injection_common.sh: active line names NornicDB"
plant scheduled-runner-image scripts/run-ifa-scheduled-proofs.sh \
	's#^image="ghcr.io/eshu-hq/ci-neo4j-community@#image="ghcr.io/eshu-hq/nornicdb-amd64-cpu@#' "run-ifa-scheduled-proofs.sh: active line names NornicDB"
plant scheduled-workflow-name .github/workflows/ifa-scheduled-proofs.yml \
	's/vs Neo4j and PostgreSQL/vs NornicDB and PostgreSQL/' "ifa-scheduled-proofs.yml: active line names NornicDB"
plant workflow-logs-wrong-compose-file .github/workflows/ifa-determinism-gate.yml \
	'120s#docker-compose\.neo4j\.yml#docker-compose.yaml#' "names the NornicDB compose file docker-compose.yaml"
plant workflow-down-wrong-compose-file .github/workflows/ifa-determinism-gate.yml \
	'267s#docker-compose\.neo4j\.yml#docker-compose.yaml#' "names the NornicDB compose file docker-compose.yaml"
plant workflow-compose-without-file .github/workflows/ifa-determinism-gate.yml \
	'174s# -f eshu-deploy/compose/docker-compose\.neo4j\.yml##' "does not name docker-compose.neo4j.yml"
plant script-compose-file-nornic scripts/lib/ifa_fault_injection_common.sh \
	's#^\(.*\)bash scripts/ci/compose.sh#\1docker-compose.yaml bash scripts/ci/compose.sh#' "names the NornicDB compose file docker-compose.yaml"
plant lib-default-flipped scripts/lib/ifa_graph_backend.sh \
	's/ESHU_GRAPH_BACKEND:=neo4j/ESHU_GRAPH_BACKEND:=nornicdb/' "must default to neo4j"

# The commented explanation of the retired variant must stay allowed, so a
# comment that names NornicDB is not a violation.
mkdir -p "${scratch}/comment"
cp -R "${scratch}/base/." "${scratch}/comment/"
printf '# NornicDB was retired from this gate by owner order 2026-11-05.\n' >>"${scratch}/comment/scripts/verify-ifa-determinism.sh"
output="$(check_tree "${scratch}/comment")" || fail "a comment naming NornicDB was flagged: ${output}"

# check_containment_selection RED: the pre-fix selection (sibling only on a
# manual nornicdb run) sends Neo4j to the all-semantic base file.
mkdir -p "${scratch}/selection-nornic-only"
cp -R "${scratch}/base/." "${scratch}/selection-nornic-only/"
sed -i.bak -e 's/^\tif \[\[ -f "${expected_edges%.json}.nornicdb.json" \]\]; then$/\tif [[ "${ESHU_GRAPH_BACKEND:-}" == "nornicdb" \&\& -f "${expected_edges%.json}.nornicdb.json" ]]; then/' \
	"${scratch}/selection-nornic-only/scripts/lib/ifa_direct_family_live.sh"
if cmp -s "${scratch}/base/scripts/lib/ifa_direct_family_live.sh" "${scratch}/selection-nornic-only/scripts/lib/ifa_direct_family_live.sh"; then
	fail "RED case selection-nornic-only: the mutation changed nothing (stale guard)"
fi
if output="$(check_containment_selection "${scratch}/selection-nornic-only")"; then
	fail "RED case selection-nornic-only: check_containment_selection passed a Neo4j run on the base file"
fi
[[ "${output}" == *"containment fixture on neo4j"* ]] || fail "RED case selection-nornic-only: wrong failure: ${output}"

# check_backend_lib RED: a default flipped to nornicdb and a lib that accepts anything.
mkdir -p "${scratch}/lib-default"
cp -R "${scratch}/base/." "${scratch}/lib-default/"
sed -i.bak -e 's/ESHU_GRAPH_BACKEND:=neo4j/ESHU_GRAPH_BACKEND:=nornicdb/' "${scratch}/lib-default/scripts/lib/ifa_graph_backend.sh"
if check_backend_lib "${scratch}/lib-default" >/dev/null; then
	fail "RED case lib-default: check_backend_lib passed a nornicdb default"
fi
mkdir -p "${scratch}/lib-lenient"
cp -R "${scratch}/base/." "${scratch}/lib-lenient/"
sed -i.bak -e 's/^\t\treturn 1$/\t\treturn 0/' "${scratch}/lib-lenient/scripts/lib/ifa_graph_backend.sh"
if cmp -s "${scratch}/base/scripts/lib/ifa_graph_backend.sh" "${scratch}/lib-lenient/scripts/lib/ifa_graph_backend.sh"; then
	fail "RED case lib-lenient: the mutation changed nothing (stale guard)"
fi
if check_backend_lib "${scratch}/lib-lenient" >/dev/null; then
	fail "RED case lib-lenient: check_backend_lib passed a lib that accepts an unknown backend"
fi

printf 'test-ifa-graph-backend-neo4j: pass (%s watched files, 11 tree mutations, 1 comment case, 2 library mutations, 1 fixture-selection mutation)\n' "${#watched[@]}"
