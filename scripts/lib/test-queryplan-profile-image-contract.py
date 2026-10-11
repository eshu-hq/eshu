#!/usr/bin/env python3
"""Check the executable Neo4j PROFILE image against its independent ledger."""

import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile


def main(ledger_path: Path, verifier_path: Path) -> None:
    """Run the profile verifier with a fake Docker binary and compare its image."""
    ledger = json.loads(ledger_path.read_text(encoding="utf-8"))
    profile = next(
        image for image in ledger["images"] if image["id"] == "neo4j-queryplan"
    )
    with tempfile.TemporaryDirectory(prefix="queryplan-image-contract-") as directory:
        temporary = Path(directory)
        arguments = temporary / "docker-arguments.txt"
        docker = temporary / "docker"
        docker.write_text(
            '#!/bin/sh\n'
            'if [ "$1" = run ]; then\n'
            '  printf "%s\\n" "$@" > "$ESHU_PROFILE_PIN_TEST_ARGS"\n'
            '  exit 91\n'
            'fi\n'
            'exit 0\n',
            encoding="utf-8",
        )
        docker.chmod(0o700)
        environment = dict(os.environ)
        environment.pop("ESHU_QUERYPLAN_PROFILE_IMAGE", None)
        environment["PATH"] = str(temporary) + os.pathsep + environment["PATH"]
        environment["ESHU_PROFILE_PIN_TEST_ARGS"] = str(arguments)
        result = subprocess.run(
            ["bash", str(verifier_path)],
            env=environment,
            capture_output=True,
            text=True,
            timeout=5,
            check=False,
        )
        if result.returncode != 91 or not arguments.is_file():
            sys.exit(
                "test-verify-query-plan-regression: profile did not reach "
                "the isolated Docker invocation"
            )
        if arguments.read_text(encoding="utf-8").splitlines()[-1] != profile[
            "runtime_sources"
        ][0]:
            sys.exit(
                "test-verify-query-plan-regression: executable profile default "
                "differs from the neo4j-queryplan image ledger"
            )


if __name__ == "__main__":
    if len(sys.argv) != 3:
        sys.exit(f"usage: {sys.argv[0]} <image-ledger> <profile-verifier>")
    main(Path(sys.argv[1]), Path(sys.argv[2]))
