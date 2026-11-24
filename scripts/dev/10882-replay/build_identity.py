#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
"""Writes the machine-readable build identity of a replay build.

For every product binary (eshu-*) in the binary directory: its sha256 and the
vcs.revision and vcs.modified build settings, next to the build commit. check_arm.py
reads it. Called by lib/build.sh (a file rather than a heredoc: the heredoc budget gate).

Usage: build_identity.py <out.json> <commit> <binary-dir>
"""
import hashlib, json, os, subprocess, sys
out, commit, bindir = sys.argv[1:4]
binaries = {}
for name in sorted(os.listdir(bindir)):
  path = os.path.join(bindir, name)
  if not name.startswith("eshu-"):
    continue
  info = subprocess.run(["go", "version", "-m", path], capture_output=True, text=True, check=True).stdout
  settings = {}
  for line in info.splitlines():
    parts = line.split()
    if len(parts) >= 2 and parts[0] == "build" and "=" in parts[1]:
      key, _, value = parts[1].partition("=")
      settings[key] = value
  with open(path, "rb") as handle:
    digest = hashlib.sha256(handle.read()).hexdigest()
  binaries[name] = {"sha256": digest, "vcs_revision": settings.get("vcs.revision", ""),
                    "vcs_modified": settings.get("vcs.modified", "")}
json.dump({"commit": commit, "binaries": binaries}, open(out, "w"), indent=2, sort_keys=True)
