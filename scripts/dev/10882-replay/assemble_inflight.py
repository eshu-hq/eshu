# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
"""Adds the in-flight arm's own readings to result.json (export/inflight.json).

Usage: assemble_inflight.py RUN_DIR
"""

import json
import os
import sys


def main(run_dir: str) -> int:
  result_path = os.path.join(run_dir, "result.json")
  with open(result_path, encoding="utf-8") as handle:
    result = json.load(handle)
  path = os.path.join(run_dir, "export", "inflight.json")
  inflight = {}
  if os.path.exists(path):
    with open(path, encoding="utf-8") as handle:
      text = handle.read().strip()
    inflight = json.loads(text) if text else {}
  result["inflight"] = inflight
  with open(result_path, "w", encoding="utf-8") as handle:
    json.dump(result, handle, indent=2, sort_keys=True)
  return 0


if __name__ == "__main__":
  sys.exit(main(sys.argv[1]))
