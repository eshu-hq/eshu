# shellcheck shell=bash
# Proof-matrix phase for security_intelligence_release_gate.sh.
#
# The phase reads one operator-local aggregate JSON file and copies only the
# public-safe coverage summary into release-gate evidence. It intentionally
# rejects repo names, package names, provider URLs, copied alert payloads, and
# machine-local paths before any data reaches evidence.json.

proof_matrix_required_ecosystems='["npm","gomod","pypi","maven","composer","rubygems","cargo","nuget"]'
proof_matrix_required_families='["terraform_iac","image_sbom","deployment"]'
proof_matrix_allowed_classes='["target_collection","advisory_ingestion","version_matching","unsupported_ecosystem","provider_only_behavior","stale_provider_alert","reducer_bug"]'
proof_matrix_forbidden_keys='["repository","repositories","repository_name","repository_id","repo","repo_name","repo_id","package","packages","package_name","package_id","provider_url","alert_url","installation","provider_repository","url","host","hostname","ip","path","file","token","payload","description","cve_description"]'

validate_proof_matrix_privacy() {
    local matrix="$1"
    jq -e . "${matrix}" >/dev/null 2>&1 || die "proof-matrix must be valid JSON"

    if ! jq -e --argjson forbidden "${proof_matrix_forbidden_keys}" '
        [.. | objects | keys[] as $key | select(($forbidden | index($key)) != null)] | length == 0
    ' "${matrix}" >/dev/null; then
        die "proof-matrix looks like private data; only aggregate totals and public issue refs are accepted"
    fi

    if jq -r '.. | strings' "${matrix}" | rg --quiet \
        'ghp_|github_pat_|glpat-|AKIA|ASIA|xox[baprs]-|https?://|/security/dependabot|arn:(aws|aws-us-gov|aws-cn):|/(Users|home|private|var|tmp|Volumes|workspace|workspaces|repos|personal-repos)/|(^|[^0-9])[0-9]{12}([^0-9]|$)|([0-9]{1,3}\.){3}[0-9]{1,3}|[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}|(^|[[:space:]"'\''])[-A-Za-z0-9_.]+/[-A-Za-z0-9_.]+($|[[:space:]"'\'',])'; then
        die "proof-matrix looks like private data; only aggregate totals and public issue refs are accepted"
    fi
}

validate_proof_matrix_contract() {
    local matrix="$1"
    if ! jq -e \
        --argjson ecosystems "${proof_matrix_required_ecosystems}" \
        --argjson families "${proof_matrix_required_families}" \
        --argjson classes "${proof_matrix_allowed_classes}" '
        def nonneg($value): (($value // 0) | type == "number" and . >= 0);
        def positive($value): (($value // 0) | type == "number" and . > 0);
        def issue_ref($value): ($value | type == "string" and test("^#[0-9]+$"));
        def allowed_class($value): ($value | type == "string" and (($classes | index($value)) != null));
        def classified_gap($obj):
            allowed_class($obj.gap_class // "") and issue_ref($obj.issue_ref // "");
        def ecosystem_ok($obj):
            nonneg($obj.repository_count) and nonneg($obj.affected_rows) and
            nonneg($obj.ready_zero_or_incomplete_rows) and
            (
                (($obj.repository_count // 0) >= 1 and
                 ($obj.affected_rows // 0) >= 1 and
                 ($obj.ready_zero_or_incomplete_rows // 0) >= 1) or
                classified_gap($obj)
            );
        def family_ok($obj):
            nonneg($obj.repository_count) and nonneg($obj.evidence_rows) and
            (
                (($obj.repository_count // 0) >= 1 and ($obj.evidence_rows // 0) >= 1) or
                classified_gap($obj)
            );
        def readback_ok($r):
            nonneg($r.package_fact_count) and
            nonneg($r.dependency_fact_count) and
            nonneg($r.advisory_fact_count) and
            nonneg($r.finding_count) and
            positive($r.wall_time_seconds) and
            (($r.retrying // -1) == 0) and
            (($r.failed // -1) == 0) and
            (($r.dead_letters // -1) == 0) and
            ($r.ready_state_counts | type == "object") and
            (($r.cpu_memory_snapshot // "") == "captured") and
            (($r.pprof_status // "") == "reachable") and
            (($r.logs_status // "") == "captured");
        . as $root |
        ($root.schema_version == 1) and
        ($root.matrix_id | type == "string" and test("^[A-Za-z0-9._-]+$")) and
        ($root.mode == "representative") and
        ($root.repository_count | type == "number" and . >= 20 and . <= 50) and
        (($root.required_repository_count // {}) as $bounds |
            (($bounds.min // 20) | type == "number") and
            (($bounds.max // 50) | type == "number") and
            ($root.repository_count >= ($bounds.min // 20)) and
            ($root.repository_count <= ($bounds.max // 50))) and
        ($root.ecosystems | type == "object") and
        all($ecosystems[]; . as $name | (($root.ecosystems[$name] // null) | type == "object") and ecosystem_ok($root.ecosystems[$name])) and
        ($root.evidence_families | type == "object") and
        all($families[]; . as $name | (($root.evidence_families[$name] // null) | type == "object") and family_ok($root.evidence_families[$name])) and
        readback_ok($root.readback // {}) and
        ($root.mismatch_classes | type == "object") and
        all(($root.mismatch_classes | keys[]); . as $name | (($classes | index($name)) != null)) and
        all($classes[]; . as $name | nonneg($root.mismatch_classes[$name])) and
        (($root.follow_up_issues // []) | type == "array") and
        all(($root.follow_up_issues // [])[]; allowed_class(.class // "") and issue_ref(.issue_ref // "")) and
        all($classes[]; . as $name |
            (($root.mismatch_classes[$name] // 0) == 0) or
            any(($root.follow_up_issues // [])[]; (.class == $name) and issue_ref(.issue_ref // "")))
    ' "${matrix}" >/dev/null; then
        die "proof-matrix does not satisfy required ecosystem coverage, evidence families, queue-zero readback, mismatch classes, and follow-up issue refs"
    fi
}

write_proof_matrix_summary() {
    local matrix="$1"
    jq -c \
        --argjson ecosystems "${proof_matrix_required_ecosystems}" \
        --argjson families "${proof_matrix_required_families}" \
        --argjson classes "${proof_matrix_allowed_classes}" '
        {
            status: "pass",
            matrix_id: .matrix_id,
            mode: .mode,
            repository_count: .repository_count,
            required_repository_count: (.required_repository_count // {min: 20, max: 50}),
            ecosystems: (
                . as $root |
                reduce $ecosystems[] as $name ({}; .[$name] = {
                    repository_count: ($root.ecosystems[$name].repository_count // 0),
                    affected_rows: ($root.ecosystems[$name].affected_rows // 0),
                    ready_zero_or_incomplete_rows: ($root.ecosystems[$name].ready_zero_or_incomplete_rows // 0),
                    gap_class: ($root.ecosystems[$name].gap_class // null),
                    issue_ref: ($root.ecosystems[$name].issue_ref // null)
                })
            ),
            evidence_families: (
                . as $root |
                reduce $families[] as $name ({}; .[$name] = {
                    repository_count: ($root.evidence_families[$name].repository_count // 0),
                    evidence_rows: ($root.evidence_families[$name].evidence_rows // 0),
                    gap_class: ($root.evidence_families[$name].gap_class // null),
                    issue_ref: ($root.evidence_families[$name].issue_ref // null)
                })
            ),
            readback: {
                package_fact_count: (.readback.package_fact_count // 0),
                dependency_fact_count: (.readback.dependency_fact_count // 0),
                advisory_fact_count: (.readback.advisory_fact_count // 0),
                finding_count: (.readback.finding_count // 0),
                ready_state_counts: (.readback.ready_state_counts // {}),
                retrying: (.readback.retrying // 0),
                failed: (.readback.failed // 0),
                dead_letters: (.readback.dead_letters // 0),
                wall_time_seconds: (.readback.wall_time_seconds // 0),
                cpu_memory_snapshot: (.readback.cpu_memory_snapshot // "missing"),
                pprof_status: (.readback.pprof_status // "missing"),
                logs_status: (.readback.logs_status // "missing")
            },
            mismatch_classes: (
                . as $root |
                reduce $classes[] as $name ({}; .[$name] = ($root.mismatch_classes[$name] // 0))
            ),
            follow_up_issues: {
                total: ((.follow_up_issues // []) | length),
                by_class: (
                    . as $root |
                    reduce $classes[] as $name ({}; .[$name] = ([($root.follow_up_issues // [])[] | select(.class == $name)] | length))
                )
            }
        }
    ' "${matrix}"
}

run_phase_proof_matrix() {
    [ -n "${proof_matrix}" ] || die "proof-matrix phase requires --proof-matrix"
    [ -f "${proof_matrix}" ] || die "proof-matrix file not found: ${proof_matrix}"

    validate_proof_matrix_privacy "${proof_matrix}"
    validate_proof_matrix_contract "${proof_matrix}"

    local summary
    summary="$(write_proof_matrix_summary "${proof_matrix}")"
    jq --argjson proof_matrix_summary "${summary}" \
        '.proof_matrix = $proof_matrix_summary' \
        "${out_json}.tmp" >"${out_json}.tmp.new"
    mv "${out_json}.tmp.new" "${out_json}.tmp"
}
