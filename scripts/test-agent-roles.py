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
        message_rule = "Use native agent messaging, when available"
        for name, spec in manifest["roles"].items():
            access = spec.get("access") or manifest["roles"][spec["base"]]["access"]
            claude = (root / ".claude" / "agents" / (name + ".md")).read_text()
            codex_binding = (root / ".codex" / "agents" / (name + ".toml")).read_text()
            opencode = (root / ".opencode" / "agent" / (name + ".md")).read_text()
            assert all(message_rule in text for text in (claude, codex_binding, opencode))
            if access == "read":
                assert "tools: Read, Glob, Grep, Bash, WebFetch, Skill, SendMessage\n" in claude
                assert "disallowedTools:" not in claude
            else:
                assert "disallowedTools:" not in claude
                assert "tools:" not in claude.split("---", 2)[1]
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
        codex = run(root, "codex-exec", "debug-eshu-deep", "Diagnose", "--dry-run")
        assert codex.returncode == 0
        codex_args = json.loads(codex.stdout)["argv"]
        assert codex_args[codex_args.index("--model") + 1] == chosen
        assert codex_args[codex_args.index("--config") + 1] == 'model_reasoning_effort="high"'
        assert codex_args[codex_args.index("--sandbox") + 1] == "read-only"
        writer = run(root, "codex-exec", "develop-eshu", "Implement", "--dry-run")
        assert writer.returncode == 0
        writer_args = json.loads(writer.stdout)["argv"]
        assert writer_args[writer_args.index("--sandbox") + 1] == "workspace-write"
        # Deep and arbiter roles: each harness binds its own model for the tier.
        for harness, arbiter_model in (("claude", "fable"), ("codex", "gpt-6-astra")):
            assert manifest["models"][harness]["arbiter"]["model"] == arbiter_model
        assert "arbiter" in manifest["models"]["muse"]
        arbiter_claude = (root / ".claude" / "agents" / "arbiter-eshu.md").read_text()
        assert "model: fable\n" in arbiter_claude and "effort: high\n" in arbiter_claude
        assert "tools: Read, Glob, Grep, Bash, WebFetch, Skill, SendMessage\n" in arbiter_claude
        assert "RULING:" in arbiter_claude
        arbiter_codex = (root / ".codex" / "agents" / "arbiter-eshu.toml").read_text()
        assert 'model = "gpt-6-astra"' in arbiter_codex and 'sandbox_mode = "read-only"' in arbiter_codex
        arbiter = run(root, "codex-exec", "arbiter-eshu", "Decide", "--dry-run")
        assert arbiter.returncode == 0
        arbiter_args = json.loads(arbiter.stdout)["argv"]
        assert arbiter_args[arbiter_args.index("--model") + 1] == "gpt-6-astra"
        assert arbiter_args[arbiter_args.index("--sandbox") + 1] == "read-only"
        deep_model = manifest["models"]["claude"]["deep"]["model"]
        deep_review = (root / ".claude" / "agents" / "review-eshu-deep.md").read_text()
        assert "model: " + deep_model + "\n" in deep_review and "skills: eshu-code-review\n" in deep_review
        assert "tools: Read, Glob, Grep, Bash, WebFetch, Skill, SendMessage\n" in deep_review
        deep_develop = (root / ".claude" / "agents" / "develop-eshu-deep.md").read_text()
        assert "model: " + deep_model + "\n" in deep_develop
        assert "tools:" not in deep_develop.split("---", 2)[1]
        deep_writer = run(root, "codex-exec", "develop-eshu-deep", "Implement", "--dry-run")
        deep_writer_args = json.loads(deep_writer.stdout)["argv"]
        assert deep_writer_args[deep_writer_args.index("--sandbox") + 1] == "workspace-write"
        print("agent-roles: generation, inheritance, permission drift, and launcher routing pass")


if __name__ == "__main__":
    main()
