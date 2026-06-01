# shellcheck shell=bash
# API/MCP/CLI readback-proof phase for security_intelligence_release_gate.sh.
#
# Operators run API, MCP, and CLI readback with their local tools, then pass an
# aggregate-only JSON summary here. Raw transcripts stay outside the public
# repository; this phase copies only public-safe status/count evidence.

readback_proof_forbidden_keys='["repository","repositories","repository_name","repository_id","repo","repo_name","repo_id","package","packages","package_name","package_id","provider_url","alert_url","installation","provider_repository","url","host","hostname","ip","path","file","token","payload","description","cve_description","transcript","stdout","stderr","request","response","body"]'

validate_readback_proof_privacy() {
    local proof="$1"
    jq -e . "${proof}" >/dev/null 2>&1 || die "readback-proof must be valid JSON"

    if ! jq -e --argjson forbidden "${readback_proof_forbidden_keys}" '
        [.. | objects | keys[] as $key | select(($forbidden | index($key)) != null)] | length == 0
    ' "${proof}" >/dev/null; then
        die "readback-proof looks like private data; only aggregate API/MCP/CLI status and counts are accepted"
    fi

    if jq -r '.. | strings' "${proof}" | rg --quiet \
        'ghp_|github_pat_|glpat-|AKIA|ASIA|xox[baprs]-|https?://|/security/dependabot|arn:(aws|aws-us-gov|aws-cn):|/(Users|home|private|var|tmp|Volumes|workspace|workspaces|repos|personal-repos)/|(^|[^0-9])[0-9]{12}([^0-9]|$)|([0-9]{1,3}\.){3}[0-9]{1,3}|[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}|(^|[[:space:]"'\''])[-A-Za-z0-9_.]+/[-A-Za-z0-9_.]+($|[[:space:]"'\'',])'; then
        die "readback-proof looks like private data; only aggregate API/MCP/CLI status and counts are accepted"
    fi
}

validate_readback_proof_contract() {
    local proof="$1"
    if ! jq -e '
        def nonneg($value): (($value // 0) | type == "number" and . >= 0);
        def positive($value): (($value // 0) | type == "number" and . > 0);
        def surface_ok($surface):
            ($surface | type == "object") and
            (($surface.status // "") == "pass") and
            positive($surface.checked) and
            (($surface.failed // -1) == 0);
        (.schema_version == 1) and
        (.proof_id | type == "string" and test("^[A-Za-z0-9._-]+$")) and
        ((.transcript_status // "") == "captured") and
        (.surfaces | type == "object") and
        surface_ok(.surfaces.api // {}) and
        surface_ok(.surfaces.mcp // {}) and
        surface_ok(.surfaces.cli // {}) and
        (.queue | type == "object") and
        nonneg(.queue.retrying) and
        nonneg(.queue.failed) and
        nonneg(.queue.dead_letters) and
        ((.queue.retrying // -1) == 0) and
        ((.queue.failed // -1) == 0) and
        ((.queue.dead_letters // -1) == 0)
    ' "${proof}" >/dev/null; then
        die "readback-proof does not satisfy API/MCP/CLI pass, queue-zero, and transcript-captured requirements"
    fi
}

write_readback_proof_summary() {
    local proof="$1"
    jq -c '{
        status: "pass",
        proof_id: .proof_id,
        transcript_status: .transcript_status,
        surfaces: {
            api: {
                status: .surfaces.api.status,
                checked: .surfaces.api.checked,
                failed: .surfaces.api.failed
            },
            mcp: {
                status: .surfaces.mcp.status,
                checked: .surfaces.mcp.checked,
                failed: .surfaces.mcp.failed
            },
            cli: {
                status: .surfaces.cli.status,
                checked: .surfaces.cli.checked,
                failed: .surfaces.cli.failed
            }
        },
        queue: {
            retrying: .queue.retrying,
            failed: .queue.failed,
            dead_letters: .queue.dead_letters
        }
    }' "${proof}"
}

run_phase_readback_proof() {
    [ -n "${readback_proof}" ] || die "readback-proof phase requires --readback-proof"
    [ -f "${readback_proof}" ] || die "readback-proof file not found: ${readback_proof}"

    validate_readback_proof_privacy "${readback_proof}"
    validate_readback_proof_contract "${readback_proof}"

    local summary
    summary="$(write_readback_proof_summary "${readback_proof}")"
    jq --argjson readback_proof_summary "${summary}" \
        '.readback_proof = $readback_proof_summary' \
        "${out_json}.tmp" >"${out_json}.tmp.new"
    mv "${out_json}.tmp.new" "${out_json}.tmp"
}
