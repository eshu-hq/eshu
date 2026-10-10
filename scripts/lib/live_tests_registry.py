#!/usr/bin/env python3
"""Load the live-test ledger from a flat file or ordered local fragments.

The ledger intentionally uses a small YAML subset. Validate that subset before
producing the same flat text consumed by the existing policy checks. This keeps
the hermetic CI mirrors independent of a third-party YAML installation.
"""

import pathlib
import re
import sys

ROOT_KEYS = frozenset({
    "version", "updated_at", "issue", "parent_issue", "owners",
    "legacy_scheduled_exemptions", "purpose", "design", "tests", "fragments",
})
POLICY_KEYS = frozenset({
    "baseline_main", "steward", "tracking_issue", "count", "sha256",
})
ROW_KEYS = ("tag", "class", "reason", "runner", "backends")
FRAGMENT_REF = re.compile(r"live-tests\.d/[A-Za-z0-9._-]+\.yaml\Z")
ROOT_FIELD = re.compile(r"([a-z_]+):(?: (.*))?\Z")
POLICY_FIELD = re.compile(r"  ([a-z0-9_]+): (.+)\Z")
ROW_FIELD = re.compile(r"    ([a-z_]+): (.*)\Z")
ROW_START = re.compile(r"  - file: (\S+)\Z")


def _significant(line: str) -> bool:
    """Return whether a line carries a ledger field rather than spacing."""
    return bool(line.strip()) and not line.lstrip().startswith("#")


def _validate_rows(lines: list[str], source: pathlib.Path) -> list[str]:
    """Validate every row and preserve its original field bytes and order."""
    seen_paths: set[str] = set()
    rows: list[str] = []
    fields: list[str] = []
    current_path = ""

    def finish() -> None:
        if not current_path:
            return
        if fields[:3] != ["tag", "class", "reason"]:
            raise ValueError(f"{source}: incomplete or reordered row {current_path}")
        if fields[3:] not in ([], ["runner"], ["backends"],
                              ["runner", "backends"]):
            raise ValueError(f"{source}: invalid row field order {current_path}")

    for line in lines:
        if not _significant(line):
            rows.append(line)
            continue
        start = ROW_START.fullmatch(line)
        if start:
            finish()
            current_path = start.group(1)
            if current_path in seen_paths:
                raise ValueError(f"{source}: duplicate ledger row: {current_path}")
            seen_paths.add(current_path)
            fields = []
            rows.append(line)
            continue
        field = ROW_FIELD.fullmatch(line)
        if not current_path or not field or field.group(1) not in ROW_KEYS:
            raise ValueError(f"{source}: malformed or unknown row field: {line}")
        if field.group(1) in fields:
            raise ValueError(f"{source}: duplicate row field {field.group(1)}")
        fields.append(field.group(1))
        rows.append(line)
    finish()
    if not seen_paths:
        raise ValueError(f"{source}: no ledger rows parsed")
    return rows


def _parse_root(lines: list[str], source: pathlib.Path) -> tuple[str, list[str]]:
    """Validate root metadata and return the enrollment mode and data lines."""
    keys: set[str] = set()
    policy: set[str] = set()
    mode = ""
    section = ""
    data: list[str] = []
    for line in lines:
        if not _significant(line):
            if mode:
                data.append(line)
            continue
        top = ROOT_FIELD.fullmatch(line)
        if top:
            key, value = top.groups()
            if key not in ROOT_KEYS or key in keys:
                raise ValueError(f"{source}: unknown or duplicate root key {key}")
            if mode:
                raise ValueError(f"{source}: root field follows {mode}: {key}")
            keys.add(key)
            section = key
            if key in ("tests", "fragments"):
                if value is not None:
                    raise ValueError(f"{source}: {key} must be a block")
                mode = key
            elif key in ("owners", "legacy_scheduled_exemptions"):
                if value is not None:
                    raise ValueError(f"{source}: {key} must be a block")
            elif not value:
                raise ValueError(f"{source}: empty root field {key}")
            continue
        if mode:
            data.append(line)
        elif section == "owners" and re.fullmatch(r"  - [A-Za-z0-9_-]+", line):
            continue
        elif section == "legacy_scheduled_exemptions":
            match = POLICY_FIELD.fullmatch(line)
            if not match or match.group(1) not in POLICY_KEYS or match.group(1) in policy:
                raise ValueError(f"{source}: unknown or duplicate exemption field: {line}")
            policy.add(match.group(1))
        elif section in ("purpose", "design") and line.startswith("  "):
            continue
        else:
            raise ValueError(f"{source}: malformed root field: {line}")
    if not mode or (mode == "fragments" and "tests" in keys):
        raise ValueError(f"{source}: expected exactly one tests or fragments block")
    if keys != ROOT_KEYS - ({"fragments"} if mode == "tests" else {"tests"}):
        raise ValueError(f"{source}: missing root metadata")
    if policy != POLICY_KEYS:
        raise ValueError(f"{source}: incomplete legacy exemption policy")
    if lines[0] != "version: live-tests/v1":
        raise ValueError(f"{source}: unsupported live-test ledger version")
    return mode, data


def _fragment_rows(path: pathlib.Path) -> list[str]:
    """Read a fragment with only one tests block and validated rows."""
    try:
        lines = path.read_text(encoding="utf-8").splitlines()
    except OSError as error:
        raise ValueError(f"{path}: missing or unreadable fragment: {error}") from error
    if not lines or lines[0] != "tests:":
        raise ValueError(f"{path}: fragment must begin with tests:")
    return _validate_rows(lines[1:], path)


def load_ledger_text(manifest: pathlib.Path) -> str:
    """Return validated flat ledger text from a flat or sharded manifest.

    Fragment names are explicit, ordered, local children of live-tests.d.
    Existing flat fixture and custom-registry paths remain supported.
    """
    manifest = pathlib.Path(manifest)
    try:
        original = manifest.read_text(encoding="utf-8")
    except OSError as error:
        raise ValueError(f"{manifest}: missing or unreadable ledger: {error}") from error
    if not original.endswith("\n"):
        raise ValueError(f"{manifest}: ledger must end with a newline")
    lines = original.splitlines()
    mode, data = _parse_root(lines, manifest)
    if mode == "tests":
        _validate_rows(data, manifest)
        return original

    references: list[str] = []
    for line in data:
        if not _significant(line):
            continue
        match = re.fullmatch(r"  - (.+)", line)
        if not match or not FRAGMENT_REF.fullmatch(match.group(1)):
            raise ValueError(f"{manifest}: invalid fragment reference: {line}")
        reference = match.group(1)
        if reference in references:
            raise ValueError(f"{manifest}: duplicate fragment reference: {reference}")
        references.append(reference)
    if not references:
        raise ValueError(f"{manifest}: empty fragments list")
    root = manifest.parent.resolve()
    bodies: list[str] = []
    for reference in references:
        path = manifest.parent / reference
        resolved = path.resolve()
        if not resolved.is_relative_to(root) or path.is_symlink():
            raise ValueError(f"{manifest}: fragment path escapes manifest root: {reference}")
        bodies.extend(_fragment_rows(path))
    _validate_rows(bodies, manifest)
    head = original[:original.index("\nfragments:\n") + 1]
    return head + "tests:\n" + "\n".join(bodies) + "\n"


if __name__ == "__main__":
    if len(sys.argv) != 3 or sys.argv[1] != "flatten":
        sys.exit("usage: live_tests_registry.py flatten <ledger_path>")
    try:
        sys.stdout.write(load_ledger_text(pathlib.Path(sys.argv[2])))
    except ValueError as error:
        sys.exit(str(error))
