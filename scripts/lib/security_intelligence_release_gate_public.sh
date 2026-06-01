# shellcheck shell=bash
# Public-safety helpers shared by security intelligence release-gate phases.

evidence_ref() {
    local path="$1"
    printf '%s' "${path#"${out_dir}"/}"
}

sanitize_public_file() {
    local input="$1"
    local output="$2"
    sed -E \
        -e 's#https?://[^[:space:]"<>]+#[redacted-url]#g' \
        -e 's#arn:(aws|aws-us-gov|aws-cn):[^[:space:]"'\'',}]+#[redacted-arn]#g' \
        -e 's#(^|[^0-9])[0-9]{12}([^0-9]|$)#\1[redacted-account]\2#g' \
        -e 's#([Aa][Uu][Tt][Hh][Oo][Rr][Ii][Zz][Aa][Tt][Ii][Oo][Nn][[:space:]]*:[[:space:]]*)(Bearer|Basic)[[:space:]]+[^[:space:]"'\'',}]+#\1[redacted-token]#g' \
        -e 's#("([Pp][Aa][Ss][Ss][Ww][Oo][Rr][Dd]|[Aa][Ww][Ss]_[Ss][Ee][Cc][Rr][Ee][Tt]_[Aa][Cc][Cc][Ee][Ss][Ss]_[Kk][Ee][Yy]|[Aa][Pp][Ii][Kk][Ee][Yy]|[Cc][Ll][Ii][Ee][Nn][Tt]_[Ss][Ee][Cc][Rr][Ee][Tt]|[Aa][Uu][Tt][Hh][Oo][Rr][Ii][Zz][Aa][Tt][Ii][Oo][Nn])"[[:space:]]*:[[:space:]]*)"[^"]*"#\1"[redacted-secret]"#g' \
        -e 's#(([Pp][Aa][Ss][Ss][Ww][Oo][Rr][Dd]|[Aa][Ww][Ss]_[Ss][Ee][Cc][Rr][Ee][Tt]_[Aa][Cc][Cc][Ee][Ss][Ss]_[Kk][Ee][Yy]|[Aa][Pp][Ii][Kk][Ee][Yy]|[Cc][Ll][Ii][Ee][Nn][Tt]_[Ss][Ee][Cc][Rr][Ee][Tt])[[:space:]]*[:=][[:space:]]*)["'\'']?[^[:space:]"'\'',}]+#\1[redacted-secret]#g' \
        -e 's#(ghp_|github_pat_|glpat-|AKIA|ASIA|xox[baprs]-)[A-Za-z0-9_./+=:-]*#[redacted-token]#g' \
        -e 's#([0-9]{1,3}\.){3}[0-9]{1,3}#[redacted-ip]#g' \
        -e 's#([[:alnum:]_-]+\.)+(internal|local|example\.com|invalid|com|net|org|io|dev|cloud)(:[0-9]+)?#[redacted-host]#g' \
        -e 's#/(Users|home|private|var|tmp|Volumes|workspace|workspaces|repos|personal-repos)/[^[:space:]",}]*(/[^[:space:]",}]*)*#[redacted-path]#g' \
        -e 's#((repository|repo|repo_id|package|package_name|provider_url|url|host|hostname|ip|path|file|token)=)["'\'']?[^[:space:]"'\'',}]+#\1[redacted]#g' \
        -e 's#("(repo|repository|repo_id|package|package_name|provider_url|url|host|hostname|ip|path|file|token)"[[:space:]]*:[[:space:]]*)"[^"]*"#\1"[redacted]"#g' \
        "${input}" >"${output}"
}

normalize_api_base_url() {
    local raw="$1"
    raw="${raw%/}"
    raw="${raw%/api/v0}"
    printf '%s' "${raw}"
}

curl_get_public_safe() {
    local url="$1"
    local accept_json="${2:-0}"
    local cfg=""
    local status
    if [ -n "${api_key}" ] || [ "${accept_json}" = "1" ]; then
        cfg="$(mktemp "${TMPDIR:-/tmp}/eshu-release-gate-curl.XXXXXX")"
        chmod 600 "${cfg}"
        if [ "${accept_json}" = "1" ]; then
            printf 'header = "Accept: application/json"\n' >>"${cfg}"
        fi
        if [ -n "${api_key}" ]; then
            printf 'header = "Authorization: Bearer %s"\n' "${api_key}" >>"${cfg}"
        fi
        curl -fsS -m 15 --config "${cfg}" "${url}"
        status=$?
        rm -f "${cfg}"
        return "${status}"
    fi
    curl -fsS -m 15 "${url}"
}

curl_readback() {
    local path="$1"
    curl_get_public_safe "${api_base_url}${path}" 0
}

k8s_curl_readback() {
    local base="$1"
    local path="$2"
    curl_get_public_safe "${base}${path}" 1
}
