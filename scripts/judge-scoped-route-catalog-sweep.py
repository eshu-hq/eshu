#!/usr/bin/env python3
"""Independently re-judge a catalog-sweep report against its route policy.

Reads the machine-readable sweep report and the Go-derived policy, prints
exercised/refused/errors counts, and exits nonzero on any undocumented 403,
so a runner-side verdict can never green a bad row silently. Invoked by
scripts/verify-scoped-route-catalog-sweep.sh; kept as a standalone file
because heredoc bodies over 512 bytes trip the heredoc-budget gate (#5074).
"""

import json
import sys


def main() -> int:
    report_path, policy_path = sys.argv[1], sys.argv[2]
    failures = []

    def load(path, what):
        try:
            with open(path) as fh:
                return json.load(fh)
        except (OSError, ValueError) as err:
            failures.append(f"{what} unreadable at {path}: {err}")
            return None

    policy = load(policy_path, "policy")
    report = load(report_path, "report")

    refusal_actuals = {
        "route_denied_403",
        "route_denied_403 (disclosed)",
        "route_denied_403 (undisclosed)",
        "http_403",
    }
    ledger_classes = {"shared_key_only", "pending_row_filtering"}

    def is_refusal(actual):
        return actual in refusal_actuals or actual.startswith("http_403")

    if policy is not None and report is not None:
        policy_rows = {(r["tool"], r["label"]): r for r in policy.get("rows", [])}
        results = report.get("results", [])
        seen = set()
        exercised = len(results)
        routes = set()
        refused = {"shared_key_only": 0, "pending_row_filtering": 0}
        strict = tolerant = errors = 0
        for res in results:
            key = (res.get("tool"), res.get("label"))
            seen.add(key)
            row = policy_rows.get(key)
            if row is None:
                failures.append(f'{res.get("tool")}[{res.get("label")}] has no policy row')
                errors += 1
                continue
            routes.add(res.get("route", ""))
            actual = res.get("actual", "")
            passed = res.get("pass") is True
            cls = row.get("class", "")
            if cls in ledger_classes:
                if passed and "(disclosed)" in actual:
                    refused[cls] += 1
                else:
                    failures.append(
                        f'{row["tool"]}[{row["label"]}] {row["method"]} {row["path"]} '
                        f"({cls}) refused without the documented disclosed 403: "
                        f"actual={actual!r} pass={passed}")
                    errors += 1
            elif is_refusal(actual):
                failures.append(
                    f'{row["tool"]}[{row["label"]}] {row["method"]} {row["path"]} '
                    f"is allowlisted but answered {actual!r}: undocumented 403")
                errors += 1
            elif not passed:
                failures.append(
                    f'{row["tool"]}[{row["label"]}] {row["method"]} {row["path"]}: '
                    f'expected {res.get("expected")!r}, got {actual!r} -- {res.get("detail", "")}')
                errors += 1
            elif row.get("accept", ["ok"]) == ["ok"]:
                strict += 1
            else:
                tolerant += 1
        for key, row in sorted(policy_rows.items()):
            if key not in seen:
                failures.append(
                    f'{row["tool"]}[{row["label"]}] {row["method"]} {row["path"]} '
                    "has a policy row but no sweep result")
                errors += 1
        refused_total = refused["shared_key_only"] + refused["pending_row_filtering"]
        summary = (f"exercised={exercised} routes={len(routes)} "
                   f"refused={refused_total} strict={strict} "
                   f"tolerant={tolerant} errors={errors}")
        if failures:
            print(f"verify-scoped-route-catalog-sweep: FAIL {summary}")
            for line in failures:
                print(f"  {line}")
            print(f"  report: {report_path}")
            print("  policy: Go-derived (TestCatalogSweepPolicy), not a hand list")
            return 1
        print(f"verify-scoped-route-catalog-sweep: PASS {summary}")
        print(f"  refused disclosed: shared_key_only={refused['shared_key_only']} "
              f"pending_row_filtering={refused['pending_row_filtering']}")
        print(f"  report: {report_path}")
        return 0
    print("verify-scoped-route-catalog-sweep: FAIL unreadable artifact")
    for line in failures:
        print(f"  {line}")
    return 1


if __name__ == "__main__":
    sys.exit(main())
