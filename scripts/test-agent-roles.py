#!/usr/bin/env python3
"""Check generated binding drift without changing the working checkout."""

import json
import os
import subprocess
import sys
import tempfile
from pathlib import Path


SOURCE_ROOT = Path(__file__).resolve().parent.parent
GENERATOR = SOURCE_ROOT / "scripts" / "agent-roles.py"
MANIFEST = SOURCE_ROOT / ".agents" / "roles.json"


def run(root, *args):
    env = {**os.environ, "ESHU_AGENT_ROLES_REPO_ROOT": str(root)}
    return subprocess.run(
        [sys.executable, str(GENERATOR), *args],
        env=env,
        capture_output=True,
        text=True,
        check=False,
    )


def main():
    with tempfile.TemporaryDirectory(prefix="eshu-agent-roles-") as temporary:
        root = Path(temporary)
        manifest = json.loads(MANIFEST.read_text())
        target = root / ".agents" / "roles.json"
        target.parent.mkdir(parents=True)
        target.write_text(json.dumps(manifest))
        for spec in manifest["roles"].values():
            skill = spec.get("skill")
            if skill:
                path = root / ".agents" / "skills" / skill / "SKILL.md"
                path.parent.mkdir(parents=True, exist_ok=True)
                path.touch()

        assert run(root, "generate").returncode == 0
        assert run(root, "check").returncode == 0
        binding = root / ".codex" / "agents" / "debug-eshu-deep.toml"
        assert "eshu-diagnostic-rigor" in binding.read_text()
        chosen = manifest["models"]["codex"]["deep"]["model"]
        binding.write_text(binding.read_text().replace('model = "' + chosen + '"', 'model = "wrong"'))
        stale = run(root, "check")
        assert stale.returncode != 0 and "debug-eshu-deep.toml" in stale.stderr
        binding.write_text(binding.read_text().replace('model = "wrong"', 'model = "' + chosen + '"'))
        reader = root / ".opencode" / "agent" / "debug-eshu.md"
        assert "  bash: deny\n" in reader.read_text()
        for before, after in (("  edit: deny", "  edit: allow"), ("  bash: deny", "  bash: allow")):
            original = reader.read_text()
            reader.write_text(original.replace(before, after))
            stale = run(root, "check")
            assert stale.returncode != 0 and "debug-eshu.md" in stale.stderr
            reader.write_text(original)
        reviewer = root / ".opencode" / "agent" / "review-eshu.md"
        assert "  bash: deny\n" in reviewer.read_text()
        muse = run(root, "muse-exec", "review-eshu", "Review", "--dry-run")
        assert muse.returncode == 0
        assert ":read-only" in muse.stdout
        print("agent-roles: generation, inheritance, permission drift, and Muse routing pass")


if __name__ == "__main__":
    main()
