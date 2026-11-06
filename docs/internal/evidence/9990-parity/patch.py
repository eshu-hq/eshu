"""Adds the #9990 parity capture calls to one scratch worktree."""

import sys


def main(root: str) -> None:
    """Patches the fleet and authorized builders under root to record each call.

    Args:
        root: Path to the scratch worktree to patch.
    """
    path = root + "/go/internal/reducer/supplychain/core/impact.go"
    with open(path, encoding="utf-8") as source:
        text = source.read()
    old = """\tfindings, _, _ := buildSupplyChainImpactFindingsWithQuarantine(envelopes)
\treturn findings
"""
    assert text.count(old) == 1
    text = text.replace(
        old,
        """\tfindings, _, err := buildSupplyChainImpactFindingsWithQuarantine(envelopes)
\tparityCapture("fleet", "", envelopes, findings, err)
\treturn findings
""",
    )
    with open(path, "w", encoding="utf-8") as target:
        target.write(text)

    path = root + "/go/internal/reducer/supplychain/core/derived_partition.go"
    with open(path, encoding="utf-8") as source:
        text = source.read()
    start = text.index(
        "\tfindings, _, err := buildAuthorizedScanImpactFindingsWithQuarantine(legacyFindings, targets"
    )
    end = text.index("\treturn findings, err\n", start)
    capture = '\tparityCapture("authorized", "", []any{legacyFindings, targets}, findings, err)\n'
    with open(path, "w", encoding="utf-8") as target:
        target.write(text[:end] + capture + text[end:])


if __name__ == "__main__":
    main(sys.argv[1])
