#!/usr/bin/env bash
# verify-npm-audit-exceptions.sh — fail-closed npm audit with one exception.
#
# Fails when `npm audit` reports any high- or critical-severity advisory
# object whose package is not picomatch or whose URL is not exactly the
# allowed one below. The single allowed advisory is picomatch
# GHSA-fc5q-39xc-vjpf (ReDoS, dev-tooling only), which has no patched
# release upstream; see the audit-job comment in
# .github/workflows/frontend.yml and the tracking issue linked there.
# Anything else high/critical fails closed, including an advisory
# object without a URL, a report with no vulnerabilities object, and
# an npm error report. Plain dependency names in a node's via list
# inherit the advisory attached to the named node, which is resolved
# transitively and checked in its own right; dangling references,
# cycles, and empty chains fail closed.
#
# Usage: verify-npm-audit-exceptions.sh [dir] [audit-json-file]
#   dir             directory whose lockfile to audit (default: .)
#   audit-json-file read this `npm audit --json` report instead of running
#                   npm audit (used by the seeded unit test)
set -euo pipefail

allow_url="https://github.com/advisories/GHSA-fc5q-39xc-vjpf"
allow_name="picomatch"
dir="${1:-.}"
fixture="${2:-}"

if [[ -n "${fixture}" ]]; then
	audit_json="$(cat "${fixture}")"
else
	# npm audit exits 1 when it finds anything (the report still lands on
	# stdout); `|| true` keeps errexit from firing on that expected code.
	# A hard failure with no report fails closed in the JSON parse below.
	audit_json="$(cd "${dir}" && npm audit --json)" || true
fi

printf '%s' "${audit_json}" | python3 -c '
import json, sys
allowed_url, allowed_name = sys.argv[1], sys.argv[2]
try:
    doc = json.loads(sys.stdin.read())
except json.JSONDecodeError as e:
    print("npm-audit-exceptions: not valid audit JSON: " + str(e))
    sys.exit(2)
if not isinstance(doc, dict) or doc.get("error"):
    print("npm-audit-exceptions: npm audit reported an error, failing closed")
    sys.exit(2)
vulns = doc.get("vulnerabilities")
if not isinstance(vulns, dict):
    print("npm-audit-exceptions: no vulnerabilities object in audit report")
    sys.exit(2)
if "auditReportVersion" in doc and doc["auditReportVersion"] != 2:
    print("npm-audit-exceptions: unsupported audit report version, failing closed")
    sys.exit(2)
# Transitively resolve each node to the advisory objects that bear on it.
# A name resolves to its own dict vias plus, for every plain string ref,
# the refs of the named node (cycle-safe; dangling refs fail closed).
resolved = {}
visiting = []
def advisory_dicts(node_name):
    if node_name in resolved:
        return resolved[node_name]
    if node_name in visiting:
        raise ValueError("dependency cycle at " + node_name)
    node = vulns.get(node_name)
    if not isinstance(node, dict):
        raise ValueError("dangling reference to " + node_name)
    via = node.get("via", [])
    if via is None:
        via = []
    if not isinstance(via, list):
        raise ValueError("malformed via in " + node_name)
    visiting.append(node_name)
    try:
        found = []
        seen = set()
        for x in via:
            if isinstance(x, dict):
                if id(x) not in seen:
                    seen.add(id(x))
                    found.append(x)
            elif isinstance(x, str):
                for adv in advisory_dicts(x):
                    if id(adv) not in seen:
                        seen.add(id(adv))
                        found.append(adv)
            else:
                raise ValueError("malformed via entry in " + node_name)
    finally:
        visiting.pop()
    resolved[node_name] = found
    return found
rank = {"info": -1, "low": 0, "moderate": 1, "high": 2, "critical": 3}
bad = []
try:
    for name in sorted(vulns):
        node = vulns[name]
        if not isinstance(node, dict):
            bad.append(("?", name, "<malformed-node>"))
            continue
        node_sev = str(node.get("severity", "")).lower()
        if node_sev not in rank:
            bad.append(("?", name, "<unclassifiable-node-severity>"))
            continue
        via = node.get("via")
        via_list = via if isinstance(via, list) else []
        if via is not None and not isinstance(via, list):
            bad.append((node_sev, name, "<malformed-via>"))
            continue
        dicts = advisory_dicts(name)
        for adv in dicts:
            sev = str(adv.get("severity", "")).lower()
            if sev not in rank:
                bad.append((node_sev, name, str(adv.get("name") or "?") + " <unclassifiable-severity>"))
            elif rank[sev] >= 2 and (adv.get("name") != allowed_name or adv.get("url") != allowed_url or sev != "high"):
                bad.append((node_sev, name, str(adv.get("name") or "?") + " " + str(adv.get("url") or "<no-advisory-url>")))
        high_dicts = [d for d in dicts if rank.get(str(d.get("severity", "")).lower(), -1) >= 2]
        if node_sev in ("high", "critical"):
            if not via_list:
                bad.append((node_sev, name, "<no-via-entries>"))
            elif not high_dicts:
                bad.append((node_sev, name, "<no-attributable-high-advisory>"))
            elif max(rank[str(d.get("severity", "")).lower()] for d in high_dicts) < rank[node_sev]:
                bad.append((node_sev, name, "<severity-not-covered-by-resolved-advisories>"))
except ValueError as e:
    print("npm-audit-exceptions: unresolvable advisory chain, failing closed: " + str(e))
    sys.exit(2)
except RecursionError:
    print("npm-audit-exceptions: advisory chain too deep to resolve, failing closed")
    sys.exit(2)
if bad:
    print("npm-audit-exceptions: FAIL - non-allowed high/critical advisories:")
    for sev, name, urls in sorted(set(bad)):
        print("  " + sev + " " + name + " " + urls)
    sys.exit(1)
print("npm-audit-exceptions: ok - no high/critical advisories outside " + allowed_url)
' "${allow_url}" "${allow_name}"
