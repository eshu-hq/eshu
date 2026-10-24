"""Verify the owner and digest of legacy scheduled live-test exemptions."""

import hashlib
import re


def verify_legacy_policy(text: str, paths: list[str]) -> None:
    """Reject an unreviewed change to the sorted exemption path set."""
    policy = re.search(
        r"^legacy_scheduled_exemptions:\n"
        r"  baseline_main: ([0-9a-f]{40})\n"
        r"  steward: (\S+)\n"
        r"  tracking_issue: ([1-9][0-9]*)\n"
        r"  count: ([0-9]+)\n"
        r"  sha256: ([0-9a-f]{64})$",
        text,
        re.M,
    )
    if not policy:
        raise ValueError("missing or invalid legacy scheduled exemption owner, issue, count, or digest")
    canonical = sorted(paths)
    digest = hashlib.sha256(
        "".join(f"{path}\n" for path in canonical).encode("utf-8")
    ).hexdigest()
    if int(policy.group(4)) != len(canonical) or digest != policy.group(5):
        raise ValueError(
            "legacy scheduled exemption inventory changed without reviewed disposition: "
            f"count={len(canonical)} sha256={digest}, "
            f"want count={policy.group(4)} sha256={policy.group(5)}"
        )
