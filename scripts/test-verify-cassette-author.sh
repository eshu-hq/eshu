#!/usr/bin/env bash
# Test mirror for scripts/verify-cassette-author.sh and its private-data scan
# (#6965 Phase 2). Seeded-violation RED/GREEN: one planted value per
# alternative in a scratch cassette directory must fail the scan naming
# `file:line:alternative`, the clean committed tree must pass through the real
# gate script, and deleting any single alternative from a scratch copy of the
# pattern library must turn its own control red. The identifier source's
# fail-open/fail-closed behaviour is exercised per option.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="${repo_root}/scripts/verify-cassette-author.sh"
lib="${repo_root}/scripts/lib/cassette_private_data_pattern.sh"
cassettes="${repo_root}/testdata/cassettes"

fail() { printf 'test-verify-cassette-author: %s\n' "$*" >&2; exit 1; }

[[ "${BASH_VERSINFO[0]}" -gt 4 || ("${BASH_VERSINFO[0]}" -eq 4 && "${BASH_VERSINFO[1]}" -ge 3) ]] \
	|| fail "requires bash >= 4.3 (found ${BASH_VERSION}); on macOS run it under Homebrew bash"

scratch="$(mktemp -d -t test-verify-cassette-author.XXXXXX)"
cleanup() { rm -rf "${scratch}"; }
trap cleanup EXIT

[[ -f "${script}" ]] || fail "missing ${script}"
[[ -x "${script}" ]] || fail "verify-cassette-author.sh must be executable"
[[ -f "${lib}" ]] || fail "missing ${lib}"
bash -n "${script}" || fail "verify-cassette-author.sh has a syntax error"
bash -n "${lib}" || fail "cassette_private_data_pattern.sh has a syntax error"

# require_code pins a fixed string to at least one NON-COMMENT line of a file,
# so a pin cannot be satisfied by prose describing the code it used to bind.
require_code() {
	local label="$1" needle="$2" file="$3" count
	count="$(rg --fixed-strings -- "${needle}" "${file}" | rg --invert-match '^[[:space:]]*#' | wc -l | tr -d ' ')" || true
	[[ "${count}" -ge 1 ]] \
		|| fail "missing ${label}, or it survives only inside a comment: ${needle}"
}

# The gate script must source the library and scan the committed tree, not a
# copy of the pattern: the control that proves the pattern lives in the
# library, and a scan over any other directory proves nothing about cassettes.
require_code "strict mode" "set -euo pipefail" "${script}"
require_code "library is sourced" 'source "${private_data_lib}"' "${script}"
require_code "committed cassettes are scanned" 'cassette_private_data_scan "${repo_root}/testdata/cassettes"' "${script}"
require_code "format contract go test" "TestCommittedCassettesValid|TestValidateCassetteBytes" "${script}"
require_code "rg exit code is captured, not tested through if" '|| rc=$?' "${lib}"
require_code "per-alternative planted coverage" 'has no planted sample' "${lib}"

# run_scan runs the library scan over $1 in a subshell with the given extra
# environment assignments, capturing combined output into $2 and the exit
# code into the variable named by $3. The subshell's own `fail` is what the
# gate would call, so a control failure and a finding both surface as exit 1.
run_scan() {
	local dir="$1" out_file="$2"
	local -n _run_scan_rc="$3"
	shift 3
	_run_scan_rc=0
	(
		set -euo pipefail
		fail() { printf 'gate: %s\n' "$*" >&2; exit 1; }
		# shellcheck source=scripts/lib/cassette_private_data_pattern.sh
		source "${lib}"
		cassette_private_data_scan "${dir}"
	) >"${out_file}" 2>&1 || _run_scan_rc=$?
}

# One clean cassette as the scratch baseline: the planted file must be the
# ONLY finding, so a RED here is the plant and not the baseline.
baseline="${scratch}/baseline"
mkdir -p "${baseline}/awscloud"
cp "${cassettes}/awscloud/supply-chain-demo.json" "${baseline}/awscloud/"
rc=0
run_scan "${baseline}" "${scratch}/baseline.out" rc
[[ "${rc}" -eq 0 ]] \
	|| fail "the scratch baseline cassette does not pass the scan; output: $(cat "${scratch}/baseline.out")"
rg --quiet --fixed-strings 'cassette private-data scan: 1 file(s) scanned' "${scratch}/baseline.out" \
	|| fail "the scan did not report its file count on the baseline"
rg --quiet --fixed-strings 'WARNING ESHU_PRIVATE_IDENTIFIERS_FILE is not set' "${scratch}/baseline.out" \
	|| fail "an unset identifier source must be announced on stderr, not passed in silence"

# RED: one hand-written planted value per alternative, deliberately different
# from the library's own control samples, so this proves the scan is wired
# to the pattern and not only that the library agrees with itself.
plant_red() {
	local alt="$1" value="$2" tag="${3:-$1}" dir out
	dir="${scratch}/red-${tag}"
	mkdir -p "${dir}/awscloud"
	cp "${cassettes}/awscloud/supply-chain-demo.json" "${dir}/awscloud/"
	printf '{"value": "%s"}\n' "${value}" >"${dir}/planted.json"
	out="${scratch}/red-${tag}.out"
	rc=0
	run_scan "${dir}" "${out}" rc
	[[ "${rc}" -ne 0 ]] \
		|| fail "RED ${alt}: the scan passed a scratch tree with a planted ${alt} value"
	rg --quiet --fixed-strings "planted.json:1:${alt}" "${out}" \
		|| fail "RED ${alt}: the finding is not named as planted.json:1:${alt}; output: $(cat "${out}")"
	# The value itself is the secret; a finding that echoes it has leaked it
	# into the CI log.
	rg --quiet --fixed-strings -- "${value}" "${out}" \
		&& fail "RED ${alt}: the finding printed the planted value"
	printf 'RED %s: exit %s, named planted.json:1:%s\n' "${tag}" "${rc}" "${alt}"
}
plant_red ipv4 '172.16.4.20'
plant_red ipv6 '2001:4860:4860::8888'
plant_red account12 '987654321098'
plant_red arn 'arn:aws:lambda:us-east-1:987654321098:function:example'
plant_red hostname 'metrics.acme-internal.net'
# An in-cluster FQDN carries the namespace out; a wildcard `.local` allow
# would pass it, and did (review of the first cut). Short in-cluster names
# and an org domain under a widened TLD are candidates too.
plant_red hostname 'orders.team-b.svc.cluster.local' hostname-cluster-local
plant_red hostname 'orders.team-b.svc' hostname-svc
plant_red hostname 'intranet.acme-internal.co.uk' hostname-cctld
# Review of the second cut found four more shapes a cluster recording
# carries: a wildcard host (TLS SANs, ingress), an internal zone outside the
# original TLD list, an address ending a sentence, and an EKS node name.
plant_red hostname '*.orders.acme-internal.com' hostname-wildcard
plant_red hostname 'vault.acme-internal.corp' hostname-corp
plant_red hostname 'vault.service.consul' hostname-consul
plant_red ipv4 'reachable at 10.20.30.40.' ipv4-sentence
plant_red nodeip 'ip-10-20-30-40'
# A real MAC is a hardware identifier; only the RFC 7042 documentation block
# and the all-zero MAC are allowed.
plant_red ipv6 '3c:22:fb:12:34:56' ipv6-mac
plant_red identifier 'eshu-canary-org'
# Record-mode pseudonymization (#6965 Phase 3) mints accounts into the
# reserved 0000 + 8 digit range, which doc_account admits. The extension is
# anchored on exactly four leading zeros: an account with any other prefix,
# including 0001, stays a finding, and so does an ARN or ECR host built on it.
plant_red account12 '000112345678' account12-reserved-neighbour
plant_red arn 'arn:aws:iam::000112345678:role/example' arn-reserved-neighbour
plant_red hostname '000112345678.dkr.ecr.us-east-1.amazonaws.com' hostname-reserved-neighbour

# GREEN for the reserved form itself: an account, an ARN and an ECR host in
# the 0000xxxxxxxx range are documentation values to this scan (the
# recorder's Verify belt, not this gate, checks that a run minted them).
reserveddir="${scratch}/reserved-account"
mkdir -p "${reserveddir}"
printf '{"account_id":"000017213864","arn":"arn:aws:ecs:us-east-1:000017213864:task/example/0123456789abcdef0123456789abcdef","uri":"000017213864.dkr.ecr.us-east-1.amazonaws.com/example"}\n' >"${reserveddir}/pseudonymized.json"
rc=0
run_scan "${reserveddir}" "${scratch}/reserved-account.out" rc
[[ "${rc}" -eq 0 ]] \
	|| fail "the reserved 0000 account form was flagged; record-mode output would fail the gate: $(cat "${scratch}/reserved-account.out")"
printf 'GREEN reserved account form: exit %s\n' "${rc}"

# 0.0.0.0 is the unspecified address; 0.0.0.0/0 is a security-group rule's
# "any address" CIDR, which record mode keeps. It is not private data, so it
# passes; the allow is exact, so a neighbouring address stays a finding.
plant_red ipv4 '0.0.0.1/32' ipv4-unspecified-neighbour
anydir="${scratch}/unspecified-ipv4"
mkdir -p "${anydir}"
printf '{"source_value":"0.0.0.0/0","bind":"0.0.0.0"}\n' >"${anydir}/any.json"
rc=0
run_scan "${anydir}" "${scratch}/unspecified-ipv4.out" rc
[[ "${rc}" -eq 0 ]] \
	|| fail "the unspecified address 0.0.0.0 was flagged; a recorded security-group rule would fail the gate: $(cat "${scratch}/unspecified-ipv4.out")"
printf 'GREEN unspecified ipv4: exit %s\n' "${rc}"

# A single label directly under amazonaws.com is an AWS service principal
# (a customer cannot register one) and passes; two raw labels there are a
# customer name and stay a finding.
plant_red hostname 'billing-api.team-c.amazonaws.com' hostname-raw-under-amazonaws
principaldir="${scratch}/service-principal"
mkdir -p "${principaldir}"
printf '{"principal_service":"states.amazonaws.com","assume_principals":["ecs-tasks.amazonaws.com","monitoring.amazonaws.com"]}\n' >"${principaldir}/principal.json"
rc=0
run_scan "${principaldir}" "${scratch}/service-principal.out" rc
[[ "${rc}" -eq 0 ]] \
	|| fail "an AWS service principal was flagged; a recorded trust policy would fail the gate: $(cat "${scratch}/service-principal.out")"
printf 'GREEN service principal: exit %s\n' "${rc}"

# A customer endpoint under an AWS suffix passes only in the form record mode
# writes: h-pseudonym customer labels, then AWS-owned labels. A raw customer
# label fails, including one wedged between an h-label and the AWS words.
plant_red hostname 'myapp-123.us-east-1.elb.amazonaws.com' hostname-raw-elb
plant_red hostname 'h0a1b2c3d4e.team-b.us-east-1.rds.amazonaws.com' hostname-raw-mid-label
endpointdir="${scratch}/aws-endpoint"
mkdir -p "${endpointdir}"
printf '{"a":"h1f2e3d4c5b.us-east-1.elb.amazonaws.com","b":"h1f2e3d4c5b.elb.us-east-1.amazonaws.com","c":"h1f2e3d4c5b.h0a1b2c3d4e.us-east-1.rds.amazonaws.com","d":"h1f2e3d4c5b.apigateway.amazonaws.com","e":"h1f2e3d4c5b.h0a1b2c3d4e.h9e8d7c6b5a.cloudformation.amazonaws.com","f":"h1f2e3d4c5b.execute-api.us-east-1.amazonaws.com","g":"000017213864.dkr.ecr.us-east-1.amazonaws.com"}\n' >"${endpointdir}/endpoint.json"
rc=0
run_scan "${endpointdir}" "${scratch}/aws-endpoint.out" rc
[[ "${rc}" -eq 0 ]] \
	|| fail "a pseudonymized AWS endpoint was flagged; a record-mode recording would fail the gate: $(cat "${scratch}/aws-endpoint.out")"
printf 'GREEN pseudonymized AWS endpoint: exit %s\n' "${rc}"

# An ARN account field that is exactly `*` (an IAM policy resource) or
# exactly `cloudfront` (the legacy origin access identity principal) is
# AWS's own and passes; a raw account and any other word stay findings.
plant_red arn 'arn:aws:iam::987654321098:role/*' arn-raw-account-wildcard-resource
plant_red arn 'arn:aws:iam::acmecorp:user/example' arn-word-account
wildarndir="${scratch}/arn-special-account"
mkdir -p "${wildarndir}"
printf '{"resources":["arn:aws:iam::*:role/*","arn:aws:logs:us-east-1:*:log-group:*"],"principal_arns":["arn:aws:iam::cloudfront:user/CloudFront Origin Access Identity E2EXAMPLE1ABC"]}\n' >"${wildarndir}/policy.json"
rc=0
run_scan "${wildarndir}" "${scratch}/arn-special-account.out" rc
[[ "${rc}" -eq 0 ]] \
	|| fail "an ARN with a wildcard or cloudfront account field was flagged: $(cat "${scratch}/arn-special-account.out")"
printf 'GREEN wildcard and cloudfront ARN accounts: exit %s\n' "${rc}"

# Terraform addresses glue a dotted token to `_`; they are not hosts and the
# corpus asserts them, so they must not be candidates.
tfdir="${scratch}/tf-address"
mkdir -p "${tfdir}"
printf '{"address": "aws_s3_bucket.local_backend_demo_state_only"}\n' >"${tfdir}/address.json"
rc=0
run_scan "${tfdir}" "${scratch}/tf-address.out" rc
[[ "${rc}" -eq 0 ]] \
	|| fail "a Terraform resource address was flagged as a .local host; output: $(cat "${scratch}/tf-address.out")"

# The hex false positive the Ifá form carries must NOT be a finding here: a
# 12-digit run inside a sha256 digest is not an account id.
hexdir="${scratch}/hex"
mkdir -p "${hexdir}"
printf '{"digest": "sha256:0e0f26e6dce79a7c164729766618cb750eca10c8b92f9c22"}\n' >"${hexdir}/digest.json"
rc=0
run_scan "${hexdir}" "${scratch}/hex.out" rc
[[ "${rc}" -eq 0 ]] \
	|| fail "twelve digits inside a hex digest were flagged; output: $(cat "${scratch}/hex.out")"

# Identifier source options. (a) unset + REQUIRED=1 is a failure, not a
# warning. (b) a configured file that is missing is a failure. (c) a
# configured file adds its literals and the count, never the values, is
# printed. (d) a cassette carrying a configured literal is a finding.
rc=0
ESHU_PRIVATE_IDENTIFIERS_REQUIRED=1 run_scan "${baseline}" "${scratch}/required.out" rc
[[ "${rc}" -ne 0 ]] || fail "ESHU_PRIVATE_IDENTIFIERS_REQUIRED=1 with no source must fail"
rc=0
ESHU_PRIVATE_IDENTIFIERS_FILE="${scratch}/does-not-exist" run_scan "${baseline}" "${scratch}/missing.out" rc
[[ "${rc}" -ne 0 ]] || fail "a missing ESHU_PRIVATE_IDENTIFIERS_FILE must fail"
identifiers="${scratch}/identifiers.txt"
printf '# synthetic identifiers for this test only\n\nacme-internal\nEshu-Canary-Product  \n' >"${identifiers}"
rc=0
ESHU_PRIVATE_IDENTIFIERS_FILE="${identifiers}" run_scan "${baseline}" "${scratch}/loaded.out" rc
[[ "${rc}" -eq 0 ]] || fail "a configured identifier file failed the clean baseline; output: $(cat "${scratch}/loaded.out")"
rg --quiet --fixed-strings '2 identifier(s) loaded from ESHU_PRIVATE_IDENTIFIERS_FILE' "${scratch}/loaded.out" \
	|| fail "the identifier count was not reported; output: $(cat "${scratch}/loaded.out")"
rg --quiet --ignore-case --fixed-strings 'acme-internal' "${scratch}/loaded.out" \
	&& fail "a configured identifier value was printed"
iddir="${scratch}/red-identifier-file"
mkdir -p "${iddir}"
printf '{"owner": "team ESHU-CANARY-PRODUCT platform"}\n' >"${iddir}/planted.json"
rc=0
ESHU_PRIVATE_IDENTIFIERS_FILE="${identifiers}" run_scan "${iddir}" "${scratch}/red-identifier-file.out" rc
[[ "${rc}" -ne 0 ]] || fail "a configured identifier in a cassette was not a finding"
rg --quiet --fixed-strings 'planted.json:1:identifier' "${scratch}/red-identifier-file.out" \
	|| fail "the configured-identifier finding is not named; output: $(cat "${scratch}/red-identifier-file.out")"
printf 'identifier source: REQUIRED=1 unset fails, missing file fails, 2 loaded, configured literal named\n'

# Mutation: delete each alternative's one defining line from a scratch copy of
# the library and expect the control to go red. The line count is checked so
# a sed that matched nothing cannot read as a passing mutation.
mutate_expect_red() {
	local label="$1" sed_expr="$2" expect="${3:-}" mutated="${scratch}/mutated.sh" before after
	sed "${sed_expr}" "${lib}" >"${mutated}"
	before="$(wc -l <"${lib}" | tr -d ' ')"
	after="$(wc -l <"${mutated}" | tr -d ' ')"
	[[ "${mutated}" != "${lib}" ]] || fail "mutation wrote over the library"
	if cmp -s "${lib}" "${mutated}"; then
		fail "mutation ${label} changed nothing; the sed expression matched no line"
	fi
	rc=0
	(
		set -euo pipefail
		fail() { printf 'control: %s\n' "$*" >&2; exit 1; }
		# shellcheck disable=SC1090
		source "${mutated}"
		declare -A detect=() allow=()
		cassette_private_data_patterns detect allow
	) >"${scratch}/mutation.out" 2>&1 || rc=$?
	[[ "${rc}" -ne 0 ]] \
		|| fail "mutation ${label} left the control green (library ${before} -> ${after} lines)"
	# Red for the right reason: a control verdict, not a crash in the mutated
	# copy, which would also exit non-zero and prove nothing about the control.
	rg --quiet '^control: ' "${scratch}/mutation.out" \
		|| fail "mutation ${label} exited ${rc} without a control verdict: $(cat "${scratch}/mutation.out")"
	if [[ -n "${expect}" ]]; then
		rg --quiet --fixed-strings -- "${expect}" "${scratch}/mutation.out" \
			|| fail "mutation ${label} went red for another reason than '${expect}': $(cat "${scratch}/mutation.out")"
	fi
	printf 'mutation %s: control exit %s: %s\n' "${label}" "${rc}" "$(rg --only-matching '^control: [^;(]*' "${scratch}/mutation.out" | head -n 1)"
}
# Two mutations per alternative. Deleting its line trips the alternative
# count; that proves the count, not the detection. Replacing its pattern with
# one that can never match keeps the count and must trip the planted-sample
# probe, which is the assertion the whole file exists for.
for alt in ipv4 nodeip ipv6 account12 arn hostname identifier; do
	mutate_expect_red "delete alternative ${alt}" "/_cpd_detect\\[${alt}\\]/{/^[[:space:]]*#/!d;}" \
		"carries 6 alternative(s), expected 7"
done
for alt in ipv4 nodeip ipv6 account12 arn hostname; do
	mutate_expect_red "never-match alternative ${alt}" "s/^\\([[:space:]]*_cpd_detect\\[${alt}\\]=\\).*/\\1'(?!)'/" \
		"alternative ${alt} no longer detects its planted sample"
done
mutate_expect_red "never-match alternative identifier" \
	"s/^[[:space:]]*_cassette_identifier_pattern '_cpd_detect\\[identifier\\]'\$/	_cpd_detect[identifier]='(?!)'/" \
	"alternative identifier no longer detects its planted sample"
mutate_expect_red "widen hostname allow to everything" "s/^\\([[:space:]]*_cpd_allow\\[hostname\\]=\\).*/\\1'.*'/" \
	"alternative hostname allows its own planted sample"
mutate_expect_red "delete the ipv4 planted sample" "/^[[:space:]]*'ipv4 10\\.0''\\.0\\.5'$/d" \
	"positive control carries 15 sample(s), expected 16"
# The reserved-account allowed sample pins the doc_account extension: delete
# it and the hand count of 34 goes red, so the form cannot be dropped from
# the allowlist without touching the number.
mutate_expect_red "delete the reserved-account allowed sample" "/^[[:space:]]*'account12 0000''17213864'$/d" \
	"negative control carries 33 sample(s), expected 34"

# The library must not depend on its caller's pipefail. A probe written as
# `rg | head` returned head's status in a caller without pipefail, so an
# uncompilable detect pattern (rg exit 2) read as a match and the positive
# control proved nothing. Both directions, in a subshell with pipefail OFF:
# the clean library's controls pass, and a scratch copy whose ipv4 pattern
# cannot compile makes the control fail.
run_controls_without_pipefail() {
	local lib_path="$1" out_file="$2"
	local -n _rcwp_rc="$3"
	_rcwp_rc=0
	(
		set -eu
		set +o pipefail
		fail() { printf 'control: %s\n' "$*" >&2; exit 1; }
		# shellcheck disable=SC1090
		source "${lib_path}"
		declare -A detect=() allow=()
		cassette_private_data_patterns detect allow
	) >"${out_file}" 2>&1 || _rcwp_rc=$?
}
rc=0
run_controls_without_pipefail "${lib}" "${scratch}/nopipefail-clean.out" rc
[[ "${rc}" -eq 0 ]] \
	|| fail "the controls fail in a caller without pipefail: $(cat "${scratch}/nopipefail-clean.out")"
broken="${scratch}/broken-ipv4.sh"
sed "s/^\\([[:space:]]*_cpd_detect\\[ipv4\\]=\\).*/\\1'(?<![0-9'/" "${lib}" >"${broken}"
cmp -s "${lib}" "${broken}" && fail "the uncompilable-ipv4 mutation changed nothing"
rc=0
run_controls_without_pipefail "${broken}" "${scratch}/nopipefail-broken.out" rc
[[ "${rc}" -ne 0 ]] \
	|| fail "an uncompilable ipv4 pattern passed the control in a caller without pipefail; rg's exit 2 is being lost"
rg --quiet --fixed-strings 'alternative ipv4 no longer detects its planted sample (rg exit 2)' "${scratch}/nopipefail-broken.out" \
	|| fail "the uncompilable-ipv4 control went red for another reason than rg exit 2: $(cat "${scratch}/nopipefail-broken.out")"
# ipv4 has an allow side, whose own probe went red by accident under the old
# pipe and masked the detect-side hole. identifier has NO allow side, so a
# lost rg exit 2 there passed the control in silence; that is the case that
# must stay red.
broken_ident="${scratch}/broken-identifier.sh"
sed "s/^[[:space:]]*_cassette_identifier_pattern '_cpd_detect\\[identifier\\]'\$/	_cpd_detect[identifier]='(?<![0-9'/" "${lib}" >"${broken_ident}"
cmp -s "${lib}" "${broken_ident}" && fail "the uncompilable-identifier mutation changed nothing"
rc=0
run_controls_without_pipefail "${broken_ident}" "${scratch}/nopipefail-broken-identifier.out" rc
[[ "${rc}" -ne 0 ]] \
	|| fail "an uncompilable identifier pattern passed the control in a caller without pipefail; with no allow side, rg's exit 2 is being lost in silence"
rg --quiet --fixed-strings 'alternative identifier no longer detects its planted sample (rg exit 2)' "${scratch}/nopipefail-broken-identifier.out" \
	|| fail "the uncompilable-identifier control went red for another reason than rg exit 2: $(cat "${scratch}/nopipefail-broken-identifier.out")"
printf 'no-pipefail caller: clean controls pass; uncompilable ipv4 and identifier patterns fail with rg exit 2\n'

# GREEN: the real gate over the committed tree, including the format go test.
green_out="${scratch}/green.out"
rc=0
# The running interpreter, not `bash`: on macOS a bare `bash` is 3.2, which
# the gate refuses before it scans anything.
"${BASH}" "${script}" >"${green_out}" 2>&1 || rc=$?
[[ "${rc}" -eq 0 ]] || fail "verify-cassette-author.sh failed on the clean tree: $(cat "${green_out}")"
rg --quiet 'cassette private-data scan: [1-9][0-9]* file\(s\) scanned' "${green_out}" \
	|| fail "the gate did not report a positive scanned-file count: $(cat "${green_out}")"
printf 'GREEN: %s\n' "$(rg --only-matching 'cassette private-data scan: [0-9]+ file\(s\) scanned' "${green_out}")"

printf 'PASS: test-verify-cassette-author\n'
