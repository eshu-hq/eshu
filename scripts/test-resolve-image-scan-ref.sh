#!/usr/bin/env bash
# Hermetic cases for scripts/ci/resolve-image-scan-ref.sh (#7699, #8059). A
# fake GHCR registry (python3 http.server on 127.0.0.1) answers the Bearer
# challenge and serves manifests only to requests carrying the Bearer token,
# so the cases exercise the real curl/header/realm exchange end to end. The
# challenge realm points at the fake registry itself (never ghcr.io), which
# proves the script follows the realm instead of hardcoding the token URL.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
target="${repo_root}/scripts/ci/resolve-image-scan-ref.sh"
work="$(mktemp -d)"

if [ ! -x "${target}" ]; then
	echo "test-resolve-image-scan-ref: missing executable script at ${target}" >&2
	exit 1
fi

pass=0
fail=0
check() {
	local desc="$1" status="$2"
	if [ "${status}" -eq 0 ]; then
		printf 'PASS: %s\n' "${desc}"
		pass=$((pass + 1))
	else
		printf 'FAIL: %s\n' "${desc}"
		fail=$((fail + 1))
	fi
}

FAKE_DIR="${work}/fake"
mkdir -p "${FAKE_DIR}/tags"
# Distinctive secrets: the resolver output must never contain either value.
FAKE_TOKEN="fake-bearer-k7Q2mT9xR4"
FAKE_GITHUB_TOKEN="fake-ghtoken-4Vz8Lp3qW2"
printf '%s' "${FAKE_TOKEN}" >"${FAKE_DIR}/token.txt"
: >"${FAKE_DIR}/requests.log"

# Fake GHCR registry: /token mints the bearer for basic x-access-token auth;
# /v2/.../manifests/<tag> 401-challenges without it and serves only with it.
# Every request is logged sanitized (presence of credentials only, no values).
cat >"${work}/fake-ghcr-registry.py" <<'PYEOF'
import base64
import http.server
import json
import os
import sys
import urllib.parse

STATE = sys.argv[1]
TOKEN = open(os.path.join(STATE, "token.txt")).read().strip()


class Handler(http.server.BaseHTTPRequestHandler):
    server_version = "FakeGHCR/1"

    def log_message(self, *args):
        pass

    def note(self, line):
        with open(os.path.join(STATE, "requests.log"), "a") as f:
            f.write(line + "\n")

    def send(self, code, body=b"", headers=()):
        self.send_response(code)
        for key, value in headers:
            self.send_header(key, value)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        if body:
            self.wfile.write(body)

    def do_GET(self):
        parsed = urllib.parse.urlparse(self.path)
        if parsed.path == "/healthz":
            self.send(200, b"ok")
            return
        if parsed.path == "/token":
            ok = False
            auth = self.headers.get("Authorization", "")
            if auth.startswith("Basic "):
                try:
                    user, _, password = base64.b64decode(auth[6:]).decode().partition(":")
                    ok = user == "x-access-token" and len(password) > 0
                except Exception:
                    ok = False
            self.note("token basic=%s -> %d" % ("yes" if ok else "no", 200 if ok else 401))
            if not ok:
                self.send(401, b"basic auth required")
                return
            self.send(200, json.dumps({"token": TOKEN}).encode(),
                      (("Content-Type", "application/json"),))
            return
        if parsed.path.startswith("/v2/") and "/manifests/" in parsed.path:
            tag = parsed.path.rsplit("/manifests/", 1)[1]
            if self.headers.get("Authorization", "") != "Bearer " + TOKEN:
                port = self.server.server_address[1]
                challenge = ('Bearer realm="http://127.0.0.1:%d/token",'
                             'service="fake-ghcr",'
                             'scope="repository:eshu-hq/eshu:pull"' % port)
                self.note("manifest tag=%s bearer=no -> 401" % tag)
                self.send(401, b"authentication required",
                          (("WWW-Authenticate", challenge),))
                return
            force_path = os.path.join(STATE, "force_code")
            if os.path.exists(force_path):
                code = int(open(force_path).read().strip())
                self.note("manifest tag=%s bearer=yes -> %d" % (tag, code))
                self.send(code, b"forced failure")
                return
            if os.path.exists(os.path.join(STATE, "tags", tag)):
                body = json.dumps({"schemaVersion": 2, "manifests": []}).encode()
                self.note("manifest tag=%s bearer=yes -> 200" % tag)
                self.send(200, body,
                          (("Content-Type", "application/vnd.oci.image.index.v1+json"),
                           ("Docker-Content-Digest", "sha256:" + "0" * 64),))
                return
            self.note("manifest tag=%s bearer=yes -> 404" % tag)
            self.send(404, b"tag not found")
            return
        self.send(404, b"no such endpoint")


server = http.server.HTTPServer(("127.0.0.1", 0), Handler)
print(server.server_address[1], flush=True)
server.serve_forever()
PYEOF

python3 "${work}/fake-ghcr-registry.py" "${FAKE_DIR}" >"${FAKE_DIR}/port" 2>"${FAKE_DIR}/server.err" &
SERVER_PID=$!
cleanup() {
	rm -rf "${work}"
	if [ -n "${SERVER_PID:-}" ]; then
		kill "${SERVER_PID}" 2>/dev/null || true
	fi
}
trap cleanup EXIT

PORT=""
ready=0
for _ in $(seq 1 100); do
	if [ -s "${FAKE_DIR}/port" ]; then
		PORT="$(cat "${FAKE_DIR}/port")"
		if curl -s -o /dev/null "http://127.0.0.1:${PORT}/healthz"; then
			ready=1
			break
		fi
	fi
	sleep 0.1
done
if [ "${ready}" != "1" ]; then
	echo "test-resolve-image-scan-ref: fake registry did not start" >&2
	cat "${FAKE_DIR}/server.err" >&2 || true
	exit 1
fi

SHA="0123456789abcdef0123456789abcdef01234567"

fresh_case() {
	rm -rf "${FAKE_DIR}/tags" "${FAKE_DIR}/force_code"
	mkdir -p "${FAKE_DIR}/tags"
	: >"${FAKE_DIR}/requests.log"
	GH_OUT="${FAKE_DIR}/output.txt"
	: >"${GH_OUT}"
}

run_resolver() {
	# run_resolver <branch> <sha>: runs the script, captures rc + output.
	set +e
	RESOLVE_OUTPUT="$(
		TRIGGER_HEAD_BRANCH="$1" TRIGGER_HEAD_SHA="$2" \
			REPOSITORY="eshu-hq/eshu" GITHUB_TOKEN="${FAKE_GITHUB_TOKEN}" \
			GHCR_BASE_URL="http://127.0.0.1:${PORT}" \
			GITHUB_OUTPUT="${GH_OUT}" bash "${target}" 2>&1
	)"
	RESOLVE_RC=$?
	set -e
}

out_has() { grep -q -F -e "$1" "${GH_OUT}"; }
req_has() { grep -q -F -e "$1" "${FAKE_DIR}/requests.log"; }
output_leaks() { printf '%s' "${RESOLVE_OUTPUT}" | grep -q -F -e "$1" || return 1; }
output_file_leaks() { grep -q -F -e "$1" "${GH_OUT}" || return 1; }

# 1. A tag release resolves to the version ref and scans.
fresh_case
printf 'x' >"${FAKE_DIR}/tags/1.2.3"
run_resolver "v1.2.3" "${SHA}"
check "tag release exits 0 (rc=${RESOLVE_RC})" "$([ "${RESOLVE_RC}" == "0" ] && echo 0 || echo 1)"
check "tag release resolves the version ref" "$(out_has 'ref=ghcr.io/eshu-hq/eshu:1.2.3' && echo 0 || echo 1)"
check "tag release does not skip" "$(out_has 'skip=false' && echo 0 || echo 1)"

# 2. A main push resolves to the full-sha ref and scans.
fresh_case
printf 'x' >"${FAKE_DIR}/tags/sha-${SHA}"
run_resolver "main" "${SHA}"
check "main push exits 0 (rc=${RESOLVE_RC})" "$([ "${RESOLVE_RC}" == "0" ] && echo 0 || echo 1)"
check "main push resolves sha-<full sha>" "$(out_has "ref=ghcr.io/eshu-hq/eshu:sha-${SHA}" && echo 0 || echo 1)"
check "main push does not skip" "$(out_has 'skip=false' && echo 0 || echo 1)"

# 3. A chart-only run (no per-commit tag) skips green on a 404 with a
# valid token.
fresh_case
run_resolver "main" "${SHA}"
check "chart-only exits 0 (rc=${RESOLVE_RC})" "$([ "${RESOLVE_RC}" == "0" ] && echo 0 || echo 1)"
check "chart-only skips" "$(out_has 'skip=true' && echo 0 || echo 1)"
check "chart-only still records the ref" "$(out_has "ref=ghcr.io/eshu-hq/eshu:sha-${SHA}" && echo 0 || echo 1)"
check "chart-only says why" "$(printf '%s' "${RESOLVE_OUTPUT}" | grep -q 'pushed no image' && echo 0 || echo 1)"
check "chart-only reached the manifest with a token (404, not 401)" "$(req_has "manifest tag=sha-${SHA} bearer=yes -> 404" && echo 0 || echo 1)"

# 4. A dispatch on another branch resolves to the sha ref, not the branch.
fresh_case
printf 'x' >"${FAKE_DIR}/tags/sha-${SHA}"
run_resolver "feature-thing" "${SHA}"
check "dispatch resolves the sha ref" "$(out_has "ref=ghcr.io/eshu-hq/eshu:sha-${SHA}" && echo 0 || echo 1)"
check "dispatch does not resolve the branch" "$(out_has 'eshu:feature-thing' && echo 1 || echo 0)"

# 5. No branch and no sha skips without touching the registry.
fresh_case
run_resolver "" ""
check "empty trigger exits 0 (rc=${RESOLVE_RC})" "$([ "${RESOLVE_RC}" == "0" ] && echo 0 || echo 1)"
check "empty trigger skips" "$(out_has 'skip=true' && echo 0 || echo 1)"
check "empty trigger makes no registry call" "$([ ! -s "${FAKE_DIR}/requests.log" ] && echo 0 || echo 1)"

# 6. A non-404 registry failure fails loudly instead of skipping.
fresh_case
printf 'x' >"${FAKE_DIR}/tags/sha-${SHA}"
printf '500' >"${FAKE_DIR}/force_code"
run_resolver "main" "${SHA}"
check "registry 500 exits non-zero (rc=${RESOLVE_RC})" "$([ "${RESOLVE_RC}" != "0" ] && echo 0 || echo 1)"

# 7. The Bearer exchange: the first manifest hit challenges (401), the
# script mints a token from the challenge realm, and only the Bearer
# request is served. Fails on the pre-#8059 script, which sends basic
# auth and exits 1 on the 401.
fresh_case
printf 'x' >"${FAKE_DIR}/tags/sha-${SHA}"
run_resolver "main" "${SHA}"
check "exchange flow exits 0 (rc=${RESOLVE_RC})" "$([ "${RESOLVE_RC}" == "0" ] && echo 0 || echo 1)"
check "first manifest hit was challenged (no bearer -> 401)" "$(req_has "manifest tag=sha-${SHA} bearer=no -> 401" && echo 0 || echo 1)"
check "script minted a token from the realm" "$(req_has 'token basic=yes -> 200' && echo 0 || echo 1)"
check "manifest served only to the bearer request" "$(req_has "manifest tag=sha-${SHA} bearer=yes -> 200" && echo 0 || echo 1)"

# 8. Neither the token nor the bearer value ever reaches the step log.
fresh_case
printf 'x' >"${FAKE_DIR}/tags/sha-${SHA}"
run_resolver "main" "${SHA}"
check "resolver output hides the github token" "$(output_leaks "${FAKE_GITHUB_TOKEN}" && echo 1 || echo 0)"
check "resolver output hides the bearer token" "$(output_leaks "${FAKE_TOKEN}" && echo 1 || echo 0)"
check "output file hides the github token" "$(output_file_leaks "${FAKE_GITHUB_TOKEN}" && echo 1 || echo 0)"
check "output file hides the bearer token" "$(output_file_leaks "${FAKE_TOKEN}" && echo 1 || echo 0)"

printf 'test-resolve-image-scan-ref: %d passed, %d failed\n' "${pass}" "${fail}"
[ "${fail}" == "0" ]
