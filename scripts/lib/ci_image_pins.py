#!/usr/bin/env python3
"""Print NAME=ghcr-digest for the five CI image pins the CI compose wrapper reads."""

import json
import sys

_IDS = {"postgres-alpine", "neo4j-community", "xx", "golang", "alpine"}


def main() -> None:
    """Print one NAME=reference line per pinned image of the ledger in argv[1]."""
    ledger = json.load(open(sys.argv[1], encoding="utf-8"))
    for image in ledger["images"]:
        if image.get("env") and image["id"] in _IDS:
            print("{env}={destination}@{digest}".format(**image))


if __name__ == "__main__":
    main()
