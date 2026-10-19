#!/usr/bin/env python3
"""Write the sweep-judge self-test fixtures: one clean policy/report pair.

Takes the fixture directory as argv[1] and writes policy.json (2 allowlisted
rows, 1 shared_key_only and 1 pending_row_filtering disclosed refusal) plus
report.json (all four passing). Invoked by
scripts/test-verify-scoped-route-catalog-sweep.sh; kept as a standalone file
because the heredoc body exceeds the 512-byte heredoc budget (#5074).
"""

import json
import sys


def main() -> None:
    fx = sys.argv[1]
    policy = {"disclosurePattern": "\\b403\\b", "disclosureFlags": "i", "rows": [
        {"tool": "list_indexed_repositories", "label": "default",
         "method": "GET", "path": "/api/v0/repositories", "class": "allowlisted",
         "arguments": {}, "accept": ["ok"]},
        {"tool": "get_entity_context", "label": "default",
         "method": "GET", "path": "/api/v0/entities/missing/context",
         "class": "allowlisted", "arguments": {}, "accept": ["ok", "not_found"]},
        {"tool": "execute_cypher_query", "label": "default",
         "method": "POST", "path": "/api/v0/code/cypher", "class": "shared_key_only",
         "arguments": {}, "accept": []},
        {"tool": "get_service_changed_since", "label": "default",
         "method": "GET", "path": "/api/v0/freshness/services/changed-since",
         "class": "pending_row_filtering", "arguments": {}, "accept": []},
    ]}
    report = {"tools": 4, "calls": 4, "results": [
        {"tool": "list_indexed_repositories", "label": "default",
         "route": "GET /api/v0/repositories", "expected": "success (ok)",
         "actual": "ok", "pass": True, "detail": ""},
        {"tool": "get_entity_context", "label": "default",
         "route": "GET /api/v0/entities/missing/context",
         "expected": "success (ok|not_found)", "actual": "not_found",
         "pass": True, "detail": ""},
        {"tool": "execute_cypher_query", "label": "default",
         "route": "POST /api/v0/code/cypher", "expected": "403 disclosed (shared_key_only)",
         "actual": "route_denied_403 (disclosed)", "pass": True, "detail": "refused"},
        {"tool": "get_service_changed_since", "label": "default",
         "route": "GET /api/v0/freshness/services/changed-since",
         "expected": "403 disclosed (pending_row_filtering)",
         "actual": "route_denied_403 (disclosed)", "pass": True, "detail": "refused"},
    ]}
    open(fx + "/policy.json", "w").write(json.dumps(policy))
    open(fx + "/report.json", "w").write(json.dumps(report))


if __name__ == "__main__":
    main()
