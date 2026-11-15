#!/usr/bin/env python3
"""Compare every installed OpenCode role shim against the kit's tier model.

The role shims in .opencode/agent/ carry the model the manifest pins for
that role's tier. A stale installed shim would dispatch a role on a model
the kit no longer pins, even with the manifest itself current.

Every role is checked against its own tier, not one spot-checked role:
a single arbiter check left the other four unguarded.

Usage: verify-role-shim-bindings.py <kit-roles.json> <shim-dir>
Exits 0 when every shim matches, 1 with the drift named on stderr.
"""

import json
import os
import sys


def main() -> int:
    if len(sys.argv) != 3:
        print(
            "verify-role-shim-bindings: usage: %s <kit-roles.json> <shim-dir>"
            % os.path.basename(sys.argv[0]),
            file=sys.stderr,
        )
        return 2
    manifest_path, shim_dir = sys.argv[1], sys.argv[2]
    with open(manifest_path, encoding="utf-8") as handle:
        manifest = json.load(handle)
    models = manifest["models"]["opencode"]
    roles = manifest["roles"]

    # A role that inherits a base role uses the base's tier, unless it
    # names its own.
    resolved = {}
    for name in roles:
        spec = dict(roles[name])
        if "base" in spec:
            base = dict(roles[spec["base"]])
            base.update(spec)
            spec = base
        resolved[name] = models[spec["tier"]]["model"]

    drift = False
    for name, expected in sorted(resolved.items()):
        shim = os.path.join(shim_dir, name + ".md")
        if not os.path.isfile(shim):
            continue
        actual = ""
        with open(shim, encoding="utf-8") as handle:
            for line in handle:
                if line.startswith("model: "):
                    actual = line[len("model: "):].strip()
                    break
        if actual != expected:
            print(
                "verify-agent-canon: installed %s shim pins %s; the kit pins %s"
                % (name, actual or "<none>", expected),
                file=sys.stderr,
            )
            drift = True
    if drift:
        print(
            "Fix: re-run the eshu-agent-kit installer: install.sh --agents",
            file=sys.stderr,
        )
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
