#!/usr/bin/env python3
"""Assemble exact scheduled proof streams from one workflow run's attempts."""

import json
from pathlib import Path
import re
import shutil

from postgres_scheduled_contract import PACKAGES


LEG_FILES = {
    "setup": ("manifest.json",),
    "freshness-scale": ("freshness_links_scale.jsonl",),
    "freshness-a": ("freshness_links_a.jsonl",),
    "freshness-b": ("freshness_links_b.jsonl",),
    "ack": ("ack_family.jsonl",),
}
NON_BULK_GROUPS = frozenset({
    "freshness_links_scale", "freshness_links_a", "freshness_links_b",
    "ack_family",
})
ARTIFACT_NAME = re.compile(
    r"scheduled-proof-streams-(setup|freshness-scale|freshness-a|"
    r"freshness-b|ack|bulk)-([1-9][0-9]*)-([1-9][0-9]*)"
)


def assemble_proof_bundle(
    downloads: Path, bundle: Path, run_id: str, run_attempt: int,
    packages: dict[str, str] = PACKAGES,
) -> None:
    """Select each leg's newest eligible artifact and retain its provenance.

    Args:
        downloads: Directory containing individually named downloaded artifacts.
        bundle: New output directory for the exact proof files.
        run_id: Current GitHub workflow run ID.
        run_attempt: Current GitHub workflow attempt.
        packages: Enrolled proof groups, used to require every bulk stream.

    Raises:
        ValueError: The artifact set is missing, malformed, from another run,
            or from a future attempt.
    """
    if not re.fullmatch(r"[1-9][0-9]*", run_id or "") or run_attempt < 1:
        raise ValueError("invalid current workflow run identity")
    if bundle.exists():
        raise ValueError("proof bundle output already exists")
    expected = {
        **LEG_FILES,
        "bulk": tuple(f"{group}.jsonl" for group in packages
                      if group not in NON_BULK_GROUPS),
    }
    selected: dict[str, tuple[int, Path]] = {}
    for artifact in downloads.iterdir():
        match = ARTIFACT_NAME.fullmatch(artifact.name)
        if not match:
            continue
        leg, artifact_run_id, attempt_text = match.groups()
        if artifact_run_id != run_id:
            continue
        attempt = int(attempt_text)
        if attempt > run_attempt:
            raise ValueError(f"future proof artifact attempt: {artifact.name}")
        if not artifact.is_dir():
            raise ValueError(f"proof artifact is not a directory: {artifact.name}")
        if leg in selected and selected[leg][0] == attempt:
            raise ValueError(f"duplicate proof artifact: {artifact.name}")
        if leg not in selected or attempt > selected[leg][0]:
            selected[leg] = (attempt, artifact)
    for leg, files in expected.items():
        if leg not in selected:
            raise ValueError(f"missing hosted proof artifact: {leg}")
        artifact = selected[leg][1]
        if {path.name for path in artifact.iterdir()} != set(files):
            raise ValueError(f"incomplete or unexpected proof artifact: {leg}")
        if any(not (artifact / name).is_file() for name in files):
            raise ValueError(f"invalid proof artifact file: {leg}")
    manifest = json.loads((selected["setup"][1] / "manifest.json").read_text(
        encoding="utf-8"))
    if (not isinstance(manifest, dict)
            or manifest.get("run_id") != run_id
            or manifest.get("run_attempt") != selected["setup"][0]):
        raise ValueError("hosted proof manifest differs from selected artifact")
    bundle.mkdir()
    for leg, files in expected.items():
        for name in files:
            shutil.copyfile(selected[leg][1] / name, bundle / name)
    provenance = {
        "run_id": run_id,
        "run_attempt": run_attempt,
        "legs": {leg: selected[leg][0] for leg in expected},
    }
    (bundle / "artifact_sources.json").write_text(
        json.dumps(provenance, sort_keys=True, indent=2) + "\n", encoding="utf-8")


def verify_artifact_sources(bundle: Path, manifest: dict[str, object],
                            current_attempt: int) -> None:
    """Require complete, same-run provenance for the assembled hosted bundle."""
    source_path = bundle / "artifact_sources.json"
    if not source_path.is_file():
        raise ValueError("missing hosted artifact source provenance")
    sources = json.loads(source_path.read_text(encoding="utf-8"))
    if (not isinstance(sources, dict)
            or set(sources) != {"run_id", "run_attempt", "legs"}
            or sources["run_id"] != manifest["run_id"]
            or sources["run_attempt"] != current_attempt
            or not isinstance(sources["legs"], dict)
            or set(sources["legs"]) != set(LEG_FILES) | {"bulk"}):
        raise ValueError("hosted artifact source provenance differs from run")
    for leg, attempt in sources["legs"].items():
        if (type(attempt) is not int or attempt < 1
                or attempt > current_attempt):
            raise ValueError(f"invalid hosted artifact source attempt: {leg}")
    if sources["legs"]["setup"] != manifest["run_attempt"]:
        raise ValueError("hosted manifest attempt differs from artifact source")
