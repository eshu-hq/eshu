#!/usr/bin/env python3
"""Matrix-shape artifact checks for scheduled PostgreSQL live proofs."""

import re

from postgres_scheduled_contract import PACKAGES
from postgres_scheduled_workflow import named_step


LEG_UPLOADS = {
    "Upload hosted proof manifest": (
        "setup", ("          path: ${{ runner.temp }}/manifest.json",), False),
    "Upload freshness scale proof stream": (
        "freshness-scale",
        ("          path: ${{ runner.temp }}/freshness_links_scale.jsonl",), True),
    "Upload freshness chunk A proof stream": (
        "freshness-a",
        ("          path: ${{ runner.temp }}/freshness_links_a.jsonl",), True),
    "Upload freshness chunk B proof stream": (
        "freshness-b",
        ("          path: ${{ runner.temp }}/freshness_links_b.jsonl",), True),
    "Upload ACK proof stream": (
        "ack", ("          path: ${{ runner.temp }}/ack_family.jsonl",), True),
}
UPLOAD_JOBS = {
    "Upload hosted proof manifest": "scheduled-setup",
    "Upload freshness scale proof stream": "scheduled-freshness-scale",
    "Upload freshness chunk A proof stream": "scheduled-freshness-a",
    "Upload freshness chunk B proof stream": "scheduled-freshness-b",
    "Upload ACK proof stream": "scheduled-ack",
}
NON_BULK_GROUPS = frozenset({
    "ack_family", "freshness_links_scale",
    "freshness_links_a", "freshness_links_b",
})


def verify_leg_uploads(blocks: dict[str, str]) -> None:
    """Require every leg's exact stream upload and its always() policy."""
    for name, (leg, files, always) in LEG_UPLOADS.items():
        _, upload_step = named_step(
            re.split(r"(?m)^      - name:", blocks[UPLOAD_JOBS[name]]), name)
        expected_upload = {
            "        uses: actions/upload-artifact@v4",
            f"          name: scheduled-proof-streams-{leg}-"
            "${{ github.run_id }}-${{ github.run_attempt }}",
            "          if-no-files-found: error",
            "          retention-days: 14",
        } | set(files)
        if not expected_upload.issubset(set(upload_step.splitlines())):
            raise ValueError(f"hosted proof leg artifact upload missing: {name}")
        if always != ("        if: always()" in upload_step.splitlines()):
            raise ValueError(f"conditional hosted proof leg upload: {name}")
    _, bulk_upload = named_step(
        re.split(r"(?m)^      - name:", blocks["scheduled-bulk"]),
        "Upload bulk proof streams")
    expected_bulk_uploads = [
        f"            ${{{{ runner.temp }}}}/{group}.jsonl"
        for group in PACKAGES if group not in NON_BULK_GROUPS
    ]
    bulk_paths = re.search(
        r"(?m)^          path: \|\n((?:            [^\n]+\n)+)",
        bulk_upload,
    )
    if (not bulk_paths
            or bulk_paths.group(1).splitlines() != expected_bulk_uploads
            or "        if: always()" not in bulk_upload.splitlines()
            or bulk_upload.count("        uses: actions/upload-artifact@v4") != 1):
        raise ValueError("hosted proof bulk upload differs from exact bundle")


def verify_aggregate_bundle(aggregate_steps: list[str]) -> None:
    """Require the exact stream download, bundle verify, and retention."""
    _, download_step = named_step(aggregate_steps, "Download proof streams")
    expected_download = {
        "        uses: actions/download-artifact@v4",
        "          pattern: scheduled-proof-streams-*-"
        "${{ github.run_id }}-${{ github.run_attempt }}",
        "          path: ${{ runner.temp }}/proof-bundle",
        "          merge-multiple: true",
    }
    if not expected_download.issubset(set(download_step.splitlines())):
        raise ValueError("hosted proof stream download missing")
    if re.search(r"(?m)^\s*if:", download_step):
        raise ValueError("conditional hosted proof stream download")
    _, bundle_step = named_step(aggregate_steps, "Verify hosted proof bundle")
    expected_bundle = (
        '        run: python3 scripts/lib/postgres_scheduled_runner.py '
        '--verify-proof-bundle "${RUNNER_TEMP}/proof-bundle"'
    )
    if expected_bundle not in bundle_step.splitlines():
        raise ValueError("hosted proof bundle verification missing")
    if re.search(r"(?m)^\s*if:", bundle_step):
        raise ValueError("conditional hosted proof bundle step")
    _, artifact_step = named_step(aggregate_steps, "Retain hosted proof bundle")
    artifact_lines = {
        "        if: always()",
        "        uses: actions/upload-artifact@v4",
        "          name: postgres-scheduled-proof-${{ github.run_id }}-"
        "${{ github.run_attempt }}",
        "          if-no-files-found: error",
        "          retention-days: 14",
    } | {
        f"            ${{{{ runner.temp }}}}/proof-bundle/{name}"
        for name in ("manifest.json", *(f"{group}.jsonl" for group in PACKAGES))
    }
    if not artifact_lines.issubset(set(artifact_step.splitlines())):
        raise ValueError("hosted proof artifact retention missing")
    uploaded = re.search(
        r"(?m)^          path: \|\n((?:            [^\n]+\n)+)",
        artifact_step,
    )
    expected_uploads = [
        f"            ${{{{ runner.temp }}}}/proof-bundle/{name}"
        for name in ("manifest.json", *(f"{group}.jsonl" for group in PACKAGES))
    ]
    if not uploaded or uploaded.group(1).splitlines() != expected_uploads:
        raise ValueError("hosted proof artifact paths differ from exact bundle")
