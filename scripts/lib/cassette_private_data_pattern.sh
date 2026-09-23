#!/usr/bin/env bash
# shellcheck disable=SC2154  # Sourced helper; `fail` is parent-owned.
# The one definition of what "private data" means to the cassette author gate
# (#6965 Phase 2), the allowlist that says which matches are documentation
# values, and the controls that prove both still do their job.
#
# WHY A SIBLING OF ifa_private_data_pattern.sh AND NOT A SECOND FUNCTION IN IT.
# That file is a registry trigger for four Ifá gates, so any edit to a cassette
# alternative would re-run the live Docker matrices, and the determinism
# mirror pins exact-once literals inside it that a second control loop of the
# same shape would collide with. The Ifá pattern, its 14 samples and its
# consumers are untouched; this file follows its discipline instead:
#
#   - the pattern is handed over ONLY through a function that runs the
#     positive control first, and it ASSIGNS by nameref rather than prints,
#     because a `fail` inside a command substitution exits only the subshell;
#   - one hand-written planted sample per alternative, assembled from split
#     quoted strings so a scanner run over this file cannot flag its own
#     control, and deliberately not derived from the pattern it checks;
#   - hand-counted totals asserted by number, so a deleted sample is loud;
#   - rg's exit code is CAPTURED and compared, never tested through `if`,
#     because an uncompilable pattern exits 2 and `if` reads that as no-match.
#
# WHAT IS DIFFERENT HERE. Cassettes legitimately carry documentation accounts,
# reserved hostnames and public service endpoints, so a bare match is a
# CANDIDATE, not a finding. Each alternative carries an anchored allow pattern
# applied to the matched token as a second, explicit step. The allow side is
# FAIL-CLOSED: a candidate passes only when it matches a reserved suffix or a
# short committed list of forms the corpus is known to need; everything else
# fails, and widening the list is a reviewed edit to this file. Blocklists
# fail open; that is the owner's principle and this file's shape.
#
# The controls therefore cut both ways. Every planted sample must match its
# alternative AND survive the allow filter (so the allowlist cannot be widened
# into the detector); every allowed sample must match the alternative AND be
# accepted (so the allowlist cannot silently stop covering a form the corpus
# uses, which would fail the clean tree, but also so a typo in it is caught
# here rather than at the next cassette refresh).
#
# WHAT THE HOSTNAME ALTERNATIVE CANNOT SEE, stated plainly. It is a lexical
# scan: a dotted token is a hostname candidate only when its last label is in
# the TLD list below, and that list deliberately leaves out every TLD that
# collides with a file extension or a dotted code/field path the corpus
# carries (.in .it .is .at .no .es .cc .pl .rs .tf .sh .md .ps .pm .so .am
# .mk .zip .name .email .run, measured on the committed cassettes). An
# organisation domain under one of those TLDs, or under any other TLD the list
# does not name, is never a candidate here. That blind spot is why Phase 3 of #6965 requires two further independent
# mechanisms (redaction on record, and re-validation) and does not rest on
# this scan alone.
#
# Same caveat as the Ifá file: the counts catch deletion, not substitution.
# The per-alternative coverage check below narrows that -- a sample moved to
# another alternative leaves its own alternative with zero samples -- but a
# sample replaced by a weaker sample of the same alternative is not caught.

# _cassette_identifier_pattern assigns the organisation / product identifier
# alternative into the variable named by $1. Identifiers MUST come from outside
# git: ESHU_PRIVATE_IDENTIFIERS_FILE names a file of literals, one per line,
# `#` comments and blank lines ignored, matched case-insensitively on
# alphanumeric boundaries. The committed canary is always included so the
# alternative is never empty and its positive control always has a sample.
#
# Unset is LOUD, not silent: the scan says on stderr that this alternative is
# running canary-only, and ESHU_PRIVATE_IDENTIFIERS_REQUIRED=1 turns that into
# a failure. A configured file that is missing or empty is a failure outright:
# a pointer that resolves to nothing is the worst shape a guard can take.
# Only the COUNT is ever printed; the values never reach a log.
_cassette_identifier_pattern() {
	local -n _cpd_ident_out="$1"
	local file="${ESHU_PRIVATE_IDENTIFIERS_FILE:-}" line count=0 lineno=0 joined
	local -a terms=('eshu-canar''y-org')
	if [[ -z "${file}" ]]; then
		printf 'cassette private-data scan: WARNING ESHU_PRIVATE_IDENTIFIERS_FILE is not set; the identifier alternative scans for the committed canary only\n' >&2
		[[ "${ESHU_PRIVATE_IDENTIFIERS_REQUIRED:-0}" != "1" ]] \
			|| fail "ESHU_PRIVATE_IDENTIFIERS_REQUIRED=1 but ESHU_PRIVATE_IDENTIFIERS_FILE is not set -- the identifier alternative would run canary-only"
	else
		[[ -s "${file}" ]] \
			|| fail "ESHU_PRIVATE_IDENTIFIERS_FILE names a missing or empty file -- a configured identifier source that resolves to nothing must never read as clean"
		# `#` starts a comment anywhere on a line, so an identifier cannot
		# contain one.
		while IFS= read -r line || [[ -n "${line}" ]]; do
			lineno=$((lineno + 1))
			line="${line%%#*}"
			line="${line#"${line%%[![:space:]]*}"}"
			line="${line%"${line##*[![:space:]]}"}"
			[[ -n "${line}" ]] || continue
			[[ "${line}" != *'\'* ]] \
				|| fail "ESHU_PRIVATE_IDENTIFIERS_FILE line ${lineno} contains a backslash, which \\Q..\\E quoting cannot carry"
			terms+=("\\Q${line}\\E")
			count=$((count + 1))
		done <"${file}"
		[[ "${count}" -gt 0 ]] \
			|| fail "ESHU_PRIVATE_IDENTIFIERS_FILE carries no identifiers after stripping comments and blank lines"
		printf 'cassette private-data scan: %s identifier(s) loaded from ESHU_PRIVATE_IDENTIFIERS_FILE\n' "${count}"
	fi
	joined="$(IFS='|'; printf '%s' "${terms[*]}")"
	_cpd_ident_out="(?i)(?<![a-z0-9])(?:${joined})(?![a-z0-9])"
}

# _cassette_probe_token runs pattern $1 over the single-line value $2 in a
# scratch file and assigns the matched token to the variable named by $3.
# Returns rg's exit code unchanged: 0 match, 1 none, 2 the pattern is broken.
_cassette_probe_token() {
	local pattern="$1" value="$2"
	local -n _cpd_probe_out="$3"
	# The scratch name is prefixed so it cannot shadow the caller's variable
	# that the nameref points at: a plain `token` here would bind the nameref
	# to this frame's local, which vanishes on return.
	local probe_dir rc=0 _cpd_probe_match=''
	probe_dir="$(mktemp -d -t cassette-private-data-probe.XXXXXX)"
	printf '%s\n' "${value}" >"${probe_dir}/sample.txt"
	# NO pipe. `rg ... | head -n 1` hands back head's status unless the caller
	# happens to run with pipefail, so an uncompilable pattern (rg exit 2)
	# read as a match and the positive control was vacuous in any caller
	# without it. rg's whole output is captured with rg's own status, and the
	# first line is taken afterwards.
	_cpd_probe_match="$(rg --pcre2 --only-matching -- "${pattern}" "${probe_dir}/sample.txt")" || rc=$?
	rm -rf "${probe_dir}"
	_cpd_probe_out="${_cpd_probe_match%%$'\n'*}"
	return "${rc}"
}

# cassette_private_data_patterns proves every alternative still detects its
# planted sample and still admits every documented allowed form, then fills
# the associative arrays named by $1 (alternative -> detect pattern) and $2
# (alternative -> anchored allow pattern; empty means nothing is allowed).
# Callers declare both with `declare -A` first.
cassette_private_data_patterns() {
	local -n _cpd_detect="$1" _cpd_allow="$2"
	local doc_account
	# Documentation account forms, shared by three alternatives: the AWS
	# documentation account, zero-prefixed values, and repdigits. One digit is
	# bracketed so this source line is not itself a 12-digit run.
	# The fourth form, 0000 + 8 digits, is the reserved range record-mode
	# pseudonymization (#6965 Phase 3, go/internal/replay/recordpseudo) mints
	# accounts into; the recorder's Verify belt admits it only for accounts
	# that run produced, so here it is a shape check on committed files.
	doc_account='(?:12345678901[2]|0{11}[0-9]|([0-9])\1{11}|0000[0-9]{8})'
	# Detection. One line per alternative, so the test mirror can delete each
	# in turn and show the control go red.
	# A dot next to the quad closes it unless a digit follows the dot, so an
	# address ending a sentence ("at 10.0.0.5.") or a reverse zone
	# ("5.0.0.10.in-addr.arpa") is still a candidate while a five-part version
	# string is not.
	_cpd_detect[ipv4]='(?<![0-9])(?<![0-9]\.)(?:(?:25[0-5]|2[0-4][0-9]|1[0-9]{2}|[1-9]?[0-9])\.){3}(?:25[0-5]|2[0-4][0-9]|1[0-9]{2}|[1-9]?[0-9])(?![0-9])(?!\.[0-9])'
	# EKS-style node names spell the private address with hyphens. The IPv4
	# alternative never sees them, so they get their own.
	_cpd_detect[nodeip]='(?i)(?<![a-z0-9-])ip-(?:[0-9]{1,3}-){3}[0-9]{1,3}(?![0-9])'
	# Colon-hex runs with three or more colons, or a `::`; that excludes hh:mm:ss
	# and `sha256:<hex>` while keeping every compressed and full IPv6 form.
	_cpd_detect[ipv6]='(?<![0-9A-Za-z:.-])(?:(?:[0-9A-Fa-f]{1,4}:){3,7}[0-9A-Fa-f]{1,4}|(?:[0-9A-Fa-f]{1,4}:){1,7}:(?:[0-9A-Fa-f]{1,4}(?::[0-9A-Fa-f]{1,4}){0,6})?|::(?:[0-9A-Fa-f]{1,4}(?::[0-9A-Fa-f]{1,4}){0,6}))(?![0-9A-Za-z:])'
	# Bounded by NON-HEX, not non-digit: the Ifá form matched twelve digits
	# inside a sha256 digest because hex letters read as boundaries.
	_cpd_detect[account12]='(?<![0-9A-Fa-f])[0-9]{12}(?![0-9A-Fa-f])'
	_cpd_detect[arn]='arn:aws(?:-[a-z]+)*:[a-z0-9-]*:[a-z0-9-]*:[^:"[:space:]]*:'
	# Hostnames end in an explicit TLD list: the reserved names, the common
	# generic TLDs, in-cluster `svc`, and the country TLDs that do not collide
	# with a file extension or a dotted code path (the excluded ones are in the
	# header). Two labels are enough: a bare `<org>.com` is exactly the leak
	# this is for. `_` closes the token on the right: DNS labels cannot carry
	# it, so `aws_s3_bucket.local_backend_demo` is a Terraform address, not a
	# `.local` host. It is not a left boundary, so `_tcp.example.com`-style
	# service names still start a candidate.
	local hostname_tlds
	hostname_tlds='com|net|org|io|dev|app|cloud|co|ai|us|internal|local|svc|example|test|invalid|localhost'
	# Enterprise and infrastructure zones a cluster recording carries: AD and
	# intranet zones, Consul, AWS's own `on.aws` endpoints, and reverse DNS.
	hostname_tlds="${hostname_tlds}|corp|lan|home|intranet|private|edu|gov|mil|int|aws|consul|arpa"
	hostname_tlds="${hostname_tlds}|info|biz|me|xyz|tech|online|site|store|shop|blog|live|news|pro|mobi|tv|ws|work|world|today|space|website|digital|network|systems|solutions|services|software|engineering|tools|team|group|company|global|host|hosting|page|zone|club|link|click|top|vip|fun|life|gg"
	hostname_tlds="${hostname_tlds}|uk|de|ca|jp|au|nl|fr|eu|ch|se|dk|be|fi|ie|pt|br|mx|ar|cn|kr|sg|hk|tw|nz|za|ru|ua|il|ae|sa|tr|gr|hu|ro|bg|sk|si|hr|lt|lv|ee|lu|cz"
	# The left boundary refuses a mid-token suffix but admits a label that
	# follows a bare dot, so `*.acme-internal.com` and `.acme-internal.com`
	# (TLS SANs, ingress hosts, search domains) are candidates.
	_cpd_detect[hostname]="(?i)(?<![a-z0-9-])(?<![a-z0-9]\\.)[a-z0-9](?:[a-z0-9-]*[a-z0-9])?(?:\\.[a-z0-9](?:[a-z0-9-]*[a-z0-9])?)*\\.(?:${hostname_tlds})(?![a-z0-9_-])"
	_cassette_identifier_pattern '_cpd_detect[identifier]'
	# Allow. Anchored on the whole token. RFC 5737 documentation ranges and
	# loopback; RFC 3849 and ::1; documentation accounts; ARNs whose account
	# field is empty, `aws`, or a documentation account; and for hostnames the
	# reserved names (RFC 2606 and RFC 6761: .example, .test, .invalid,
	# .localhost and the example.com/.net/.org zones -- NOT .local, because a
	# recording from a cluster is full of <svc>.<namespace>.svc.cluster.local
	# and each one carries the namespace out), an exact list of public service hosts the
	# committed corpus uses (including the google.cloud and Microsoft.<Provider>
	# vendor namespaces, which end in a TLD without being hosts), a single
	# service label under googleapis.com, ECR under a documentation account,
	# and the corpus's own synthetic zones.
	_cpd_allow[ipv4]='^(?:192\.0\.2\.[0-9]{1,3}|198\.51\.100\.[0-9]{1,3}|203\.0\.113\.[0-9]{1,3}|127\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3})$'
	# The ipv6 detector also sees six-group MAC addresses. A real MAC is a
	# hardware identifier and stays a finding; the RFC 7042 documentation block
	# (00:00:5e:00:53:xx) and the all-zero MAC are what a redacted recording
	# writes, so they pass.
	_cpd_allow[ipv6]='(?i)^(?:2001:db8:[0-9a-f:]*|::1|00:00:5e:00:53:[0-9a-f]{2}|00:00:00:00:00:00)$'
	_cpd_allow[account12]="^${doc_account}\$"
	_cpd_allow[arn]="^arn:aws(?:-[a-z]+)*:[a-z0-9-]*:[a-z0-9-]*:(?:|aws|${doc_account}):\$"
	_cpd_allow[hostname]="(?i)^(?:(?:[a-z0-9-]+\\.)*(?:example|test|invalid|localhost)|(?:[a-z0-9-]+\\.)*example\\.(?:com|net|org)|github\\.com|gitlab\\.com|ghcr\\.io|registry\\.terraform\\.io|registry\\.npmjs\\.org|proxy\\.golang\\.org|console\\.cloud\\.google\\.com|google\\.cloud|microsoft\\.[a-z]+|slsa\\.dev|in-toto\\.io|kubernetes\\.io|app\\.kubernetes\\.io|argoproj\\.io|argocd\\.argoproj\\.io|us-docker\\.pkg\\.dev|[a-z0-9-]+\\.googleapis\\.com|${doc_account}\\.dkr\\.ecr\\.[a-z0-9-]+\\.amazonaws\\.com|(?:[a-z0-9-]+\\.)*supply-chain-demo\\.internal|supply-chain-demo\\.pagerduty\\.internal|supply-chain-demo-project\\.iam\\.gserviceaccount\\.com|supply-chain-demo-project\\.uc\\.r\\.appspot\\.com|supplychaindemoacr\\.azurecr\\.io|supply-chain-demo\\.eastus\\.azurecontainerapps\\.io)\$"
	_cpd_allow[identifier]=''
	_cpd_allow[nodeip]='(?i)^ip-(?:192-0-2-[0-9]{1,3}|198-51-100-[0-9]{1,3}|203-0-113-[0-9]{1,3}|127-[0-9]{1,3}-[0-9]{1,3}-[0-9]{1,3})$'

	# Positive control: `<alternative> <value>`, one per alternative, each a
	# real-shaped private value that must be DETECTED and must NOT be allowed.
	local -a planted=(
		'ipv4 10.0''.0.5'
		'ipv6 fd00:''1234::1'
		'account12 2109''87654321'
		'arn arn:aw''s:iam::210987654321:role/example'
		'hostname vault.acme''-internal.com'
		'hostname payments.team-a.svc.cluster''.local'
		'hostname payments.team-a''.svc'
		'hostname *.orders.acme''-internal.com'
		'hostname vault.service''.consul'
		'ipv4 reachable at 172.16''.9.4.'
		'nodeip ip-10''-0-1-5'
		'identifier eshu-canar''y-org'
	)
	# Negative control: `<alternative> <value>`, one per allowed FORM, each a
	# documentation value that must be detected as a candidate and then allowed.
	local -a allowed=(
		'ipv4 192.0''.2.10'
		'ipv4 198.51''.100.7'
		'ipv4 203.0''.113.9'
		'ipv4 127.0''.0.1'
		'nodeip ip-192''-0-2-10'
		'ipv6 2001:db8::1'
		'ipv6 2001:db8:85a3::8a2e:370:7334'
		'ipv6 ::1'
		'ipv6 00:00:5e:00:53:0a'
		'ipv6 00:00:00:00:00:00'
		'account12 1234''56789012'
		'account12 0000''00000001'
		'account12 5555''55555555'
		'account12 0000''17213864'
		'arn arn:aw''s:s3:::example-bucket'
		'arn arn:aw''s:iam::aws:policy/example'
		'arn arn:aw''s:iam::123456789012:role/example'
		'arn arn:aw''s:iam::000000000000:role/example'
		'hostname registry.example''.invalid'
		'hostname registry.local''host'
		'hostname registry.example''.com'
		'hostname github''.com'
		'hostname compute.googleapis''.com'
		'hostname Microsoft.Ap''p'
		'hostname 123456789012.dkr.ecr.us-east-1.amazonaws''.com'
		'hostname vault.supply-chain-demo''.internal'
		'hostname supply-chain-demo.pagerduty''.internal'
		'hostname supply-chain-demo-project.iam.gserviceaccount''.com'
		'hostname supplychaindemoacr.azurecr''.io'
	)
	# Hand-counted, deliberately not derived from the arrays above or from the
	# patterns: 7 alternatives, 12 planted samples (hostname carries five: a
	# public-TLD host, an in-cluster FQDN, a short in-cluster name, a wildcard
	# host and a Consul name; ipv4 carries two: a bare address and one ending
	# a sentence), 29 allowed samples. Adding an alternative or an allowed form means adding
	# its sample and bumping the number, and that is the point.
	[[ "${#_cpd_detect[@]}" -eq 7 ]] \
		|| fail "cassette private-data pattern carries ${#_cpd_detect[@]} alternative(s), expected 7 -- an alternative was added or removed without re-checking its controls"
	[[ "${#planted[@]}" -eq 12 ]] \
		|| fail "cassette private-data positive control carries ${#planted[@]} sample(s), expected 12 -- a sample was added or removed without re-checking it against the alternatives"
	[[ "${#allowed[@]}" -eq 29 ]] \
		|| fail "cassette private-data negative control carries ${#allowed[@]} sample(s), expected 29 -- an allowed form was added or removed without re-checking it against the allow patterns"

	local entry alt value token rc
	local -A planted_per_alt=()
	for entry in "${planted[@]}"; do
		alt="${entry%% *}"
		value="${entry#* }"
		[[ -n "${_cpd_detect[${alt}]+x}" ]] \
			|| fail "planted sample names alternative ${alt}, which the cassette private-data pattern does not define"
		planted_per_alt[${alt}]=1
		rc=0
		_cassette_probe_token "${_cpd_detect[${alt}]}" "${value}" token || rc=$?
		[[ "${rc}" -eq 0 ]] \
			|| fail "cassette private-data alternative ${alt} no longer detects its planted sample (rg exit ${rc}); it was removed, renamed, or made uncompilable, and the scan would report every cassette clean"
		[[ -z "${_cpd_allow[${alt}]}" ]] && continue
		rc=0
		_cassette_probe_token "${_cpd_allow[${alt}]}" "${token}" token || rc=$?
		[[ "${rc}" -eq 1 ]] \
			|| fail "cassette private-data alternative ${alt} allows its own planted sample (rg exit ${rc}); the allowlist has been widened into the detector"
	done
	for alt in "${!_cpd_detect[@]}"; do
		[[ -n "${planted_per_alt[${alt}]+x}" ]] \
			|| fail "cassette private-data alternative ${alt} has no planted sample; its detection is unproven"
	done
	for entry in "${allowed[@]}"; do
		alt="${entry%% *}"
		value="${entry#* }"
		[[ -n "${_cpd_detect[${alt}]+x}" ]] \
			|| fail "allowed sample names alternative ${alt}, which the cassette private-data pattern does not define"
		rc=0
		_cassette_probe_token "${_cpd_detect[${alt}]}" "${value}" token || rc=$?
		[[ "${rc}" -eq 0 ]] \
			|| fail "cassette private-data allowed sample for ${alt} is no longer a candidate (rg exit ${rc}); the negative control proves nothing about a value the detector never sees"
		rc=0
		_cassette_probe_token "${_cpd_allow[${alt}]}" "${token}" token || rc=$?
		[[ "${rc}" -eq 0 ]] \
			|| fail "cassette private-data alternative ${alt} no longer allows a documented form (rg exit ${rc}); the clean corpus would fail, or the allow pattern is uncompilable"
	done
}

# cassette_private_data_scan scans every file under directory $1 with every
# alternative, applies each alternative's allow pattern to the unique matched
# tokens, and fails naming `file:line:alternative` for every surviving match.
# Tokens are never printed: a finding's value is exactly what must not reach a
# CI log. On success it prints the scanned file count, asserted above zero.
cassette_private_data_scan() {
	local dir="$1"
	local -A detect=() allow=()
	cassette_private_data_patterns detect allow
	local -a files=() violations=()
	local alt rc matches match path rest line token verdict_rc
	while IFS= read -r path; do
		files+=("${path}")
	done < <(rg --files --no-ignore --hidden -- "${dir}" | LC_ALL=C sort)
	[[ "${#files[@]}" -gt 0 ]] \
		|| fail "cassette private-data scan found no files under ${dir}; a scan over nothing proves nothing"
	while IFS= read -r alt; do
		local -A verdict=()
		rc=0
		matches="$(rg --pcre2 --only-matching --line-number --with-filename --no-heading -- "${detect[${alt}]}" "${files[@]}")" || rc=$?
		[[ "${rc}" -eq 0 || "${rc}" -eq 1 ]] \
			|| fail "the cassette private-data scan could not run alternative ${alt} (rg exit ${rc}); a scanner that cannot run must never read as clean"
		[[ "${rc}" -eq 0 ]] || continue
		while IFS= read -r match; do
			path="${match%%:*}"
			rest="${match#*:}"
			line="${rest%%:*}"
			token="${rest#*:}"
			if [[ -z "${verdict[${token}]+x}" ]]; then
				if [[ -z "${allow[${alt}]}" ]]; then
					verdict[${token}]=flag
				else
					verdict_rc=0
					printf '%s\n' "${token}" | rg --pcre2 --quiet -- "${allow[${alt}]}" || verdict_rc=$?
					case "${verdict_rc}" in
					0) verdict[${token}]=allowed ;;
					1) verdict[${token}]=flag ;;
					*) fail "the cassette private-data allow filter could not run for ${alt} (rg exit ${verdict_rc}); a filter that cannot run must never read as clean" ;;
					esac
				fi
			fi
			[[ "${verdict[${token}]}" == allowed ]] || violations+=("${path#"${dir}"/}:${line}:${alt}")
		done <<<"${matches}"
	done < <(printf '%s\n' "${!detect[@]}" | LC_ALL=C sort)
	if [[ "${#violations[@]}" -gt 0 ]]; then
		printf '%s\n' "${violations[@]}" | LC_ALL=C sort -u >&2
		fail "cassette private-data scan: ${#violations[@]} finding(s) above as file:line:alternative -- cassettes carry synthetic or redacted values only"
	fi
	printf 'cassette private-data scan: %s file(s) scanned\n' "${#files[@]}"
}
