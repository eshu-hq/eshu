"""Behavioral tests for the CI-only Compose image overlay."""

import json
import os
import pathlib
import shutil
import subprocess
import tempfile
import unittest


SCRIPT = pathlib.Path(__file__).with_name("ci_image_compose.py")
SUPPORTED_DOCKERFILE = SCRIPT.parents[2] / "Dockerfile"
DIGEST = "a" * 64
NORNICDB_IMAGE = (
    "ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-500-e022384c@sha256:"
    "74a8ed7b36f37bdd1a7e32d8bc6aa3fa88908b7207bfa6568567ab94e4a4b3b1"
)


class CiImageComposeTest(unittest.TestCase):
    """Verify the rendered stack is mapped completely or rejected."""

    def setUp(self) -> None:
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = pathlib.Path(self.temp.name)
        shutil.copyfile(SUPPORTED_DOCKERFILE, self.root / "Dockerfile")
        self.ledger = self.root / "images.json"
        self.images = [
            {"id": image_id, "env": env, "source": source,
             "destination": f"ghcr.io/eshu-hq/ci-{image_id}", "digest": f"sha256:{DIGEST}",
             "runtime_sources": runtime_sources, "source_records": ["https://hub.docker.com/"] ,
             **({"build_arg": env} if build_arg else {})}
            for image_id, env, source, runtime_sources, build_arg in (
                ("postgres-alpine", "POSTGRES_IMAGE", "docker.io/library/postgres:18-alpine",
                 ["postgres:18-alpine"], False),
                ("neo4j-community", "NEO4J_IMAGE", "docker.io/library/neo4j:2026-community",
                 ["neo4j:2026-community@sha256:" + "b" * 64], False),
                ("xx", "ESHU_XX_IMAGE", "docker.io/tonistiigi/xx:1.5.0", [], True),
                ("golang", "ESHU_GO_IMAGE", "docker.io/library/golang:1.26.9-alpine", [], True),
                ("alpine", "ESHU_ALPINE_IMAGE", "docker.io/library/alpine:3.21", [], True),
            )
        ]
        self.config = {
            "services": {
                "postgres": {"image": "postgres:18-alpine"},
                "neo4j": {"image": "neo4j:2026-community@sha256:" + "b" * 64},
                "api": {"build": {"context": str(self.root), "dockerfile": "Dockerfile",
                                  "target": "runtime", "args": {"SOURCE_DATE_EPOCH": "123"}}},
            }
        }

    def render(self, config: dict | None = None, images: list | None = None,
               environment: dict[str, str] | None = None,
               runtime_allowlist: list[str] | None = None) -> subprocess.CompletedProcess[str]:
        """Run the generator with synthetic effective Compose JSON."""
        self.ledger.write_text(
            json.dumps({"version": 1, "images": images if images is not None else self.images,
                        "runtime_allowlist": ([NORNICDB_IMAGE] if runtime_allowlist is None
                                              else runtime_allowlist)}),
            encoding="utf-8",
        )
        return subprocess.run(
            ["python3", str(SCRIPT), "--root", str(self.root), "--ledger", str(self.ledger)],
            input=json.dumps(config if config is not None else self.config), text=True,
            capture_output=True, check=False, env=environment,
        )

    def test_all_profiles_emit_only_images_and_build_args(self) -> None:
        result = self.render()
        self.assertEqual(result.returncode, 0, result.stderr)
        overlay = json.loads(result.stdout)
        self.assertEqual(set(overlay["services"]), {"postgres", "neo4j", "api"})
        self.assertEqual(overlay["services"]["api"]["build"]["args"]["ESHU_GO_IMAGE"],
                         f"ghcr.io/eshu-hq/ci-golang@sha256:{DIGEST}")
        self.assertNotIn("SOURCE_DATE_EPOCH", result.stdout)
        self.assertNotIn("target", result.stdout)

    def test_partial_pins_fail_closed(self) -> None:
        self.assertNotEqual(self.render(images=self.images[:1]).returncode, 0)
        missing_go = [row for row in self.images if row["id"] != "golang"]
        self.assertNotEqual(self.render(images=missing_go).returncode, 0)
        missing_source = json.loads(json.dumps(self.images))
        missing_source[0]["runtime_sources"] = []
        build_only = {"services": {"api": self.config["services"]["api"]}}
        self.assertNotEqual(self.render(config=build_only, images=missing_source).returncode, 0)

    def test_ci_mode_requires_all_matching_environment_pins(self) -> None:
        pins = {row["env"]: f"{row['destination']}@{row['digest']}" for row in self.images}
        pins["ESHU_CI_IMAGES"] = "1"
        missing_pins = {**os.environ, "ESHU_CI_IMAGES": "1"}
        self.assertNotEqual(self.render(environment=missing_pins).returncode, 0)
        self.assertEqual(self.render(environment={**os.environ, **pins}).returncode, 0)
        pins["ESHU_GO_IMAGE"] = "ghcr.io/eshu-hq/wrong@sha256:" + DIGEST
        self.assertNotEqual(self.render(environment={**os.environ, **pins}).returncode, 0)

    def test_custom_image_is_not_clobbered(self) -> None:
        config = json.loads(json.dumps(self.config))
        config["services"]["postgres"]["image"] = "example.com/custom:latest"
        self.assertNotEqual(self.render(config=config).returncode, 0)

    def test_only_committed_nornicdb_pin_is_accepted(self) -> None:
        """An arbitrary digest or repository must not bypass CI image review."""
        config = json.loads(json.dumps(self.config))
        config["services"]["nornicdb"] = {"image": NORNICDB_IMAGE}
        self.assertEqual(self.render(config=config).returncode, 0)
        self.assertNotEqual(self.render(config=config, runtime_allowlist=[]).returncode, 0)
        for image in (
            NORNICDB_IMAGE.replace("74a8ed7b", "00000000"),
            NORNICDB_IMAGE.replace("nornicdb-amd64-cpu", "nornicdb-other"),
            NORNICDB_IMAGE.replace("fix-500-e022384c", "unreviewed"),
        ):
            with self.subTest(image=image):
                config["services"]["nornicdb"]["image"] = image
                self.assertNotEqual(self.render(config=config).returncode, 0)

    def test_wrong_dockerfile_fails_closed(self) -> None:
        config = json.loads(json.dumps(self.config))
        config["services"]["api"]["build"]["dockerfile"] = "Otherfile"
        self.assertNotEqual(self.render(config=config).returncode, 0)

    def test_external_context_old_or_hardcoded_dockerfile_fails_closed(self) -> None:
        """Build args cannot redirect FROM in an unsupported external Dockerfile."""
        dockerfile = self.root / "Dockerfile"
        dockerfile.write_text("FROM golang:1.26.9-alpine\n", encoding="utf-8")
        self.assertNotEqual(self.render().returncode, 0)
        supported = SUPPORTED_DOCKERFILE.read_text(encoding="utf-8")
        dockerfile.write_text(
            supported.replace("FROM --platform=$BUILDPLATFORM ${ESHU_GO_IMAGE} AS builder",
                              "FROM --platform=$BUILDPLATFORM golang:1.26.9-alpine AS builder"),
            encoding="utf-8",
        )
        self.assertNotEqual(self.render().returncode, 0)

    def test_malformed_render_fails_closed(self) -> None:
        result = subprocess.run(
            ["python3", str(SCRIPT), "--root", str(self.root), "--ledger", str(self.ledger)],
            input="{", text=True, capture_output=True, check=False,
        )
        self.assertNotEqual(result.returncode, 0)


class RealComposeParityTest(unittest.TestCase):
    """Prove the final overlay changes only approved image/build arguments."""

    @unittest.skipUnless(shutil.which("docker"), "Docker Compose unavailable")
    def test_real_nornicdb_override_fails_preflight(self) -> None:
        """Compose interpolation must not turn an override into approval."""
        root = SCRIPT.parents[2]
        wrong_image = NORNICDB_IMAGE.replace("74a8ed7b", "00000000")
        environment = {**os.environ, "NORNICDB_IMAGE": wrong_image}
        rendered = subprocess.run(
            ["docker", "compose", "-f", "docker-compose.yaml", "--profile", "*",
             "config", "--format", "json"],
            cwd=root, env=environment, capture_output=True, text=True, check=False,
        )
        self.assertEqual(rendered.returncode, 0, "Compose render failed")
        result = subprocess.run(
            ["python3", str(SCRIPT), "--root", str(root), "--ledger",
             str(root / "specs/ci-images.v1.json")],
            cwd=root, env=environment, input=rendered.stdout,
            capture_output=True, text=True, check=False,
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(result.stdout, "")

    @unittest.skipUnless(shutil.which("docker"), "Docker Compose unavailable")
    def test_effective_stack_parity(self) -> None:
        root = SCRIPT.parents[2]
        ledger = root / "specs/ci-images.v1.json"
        stacks = (
            ("docker-compose.yaml",),
            ("docker-compose.neo4j.yml",),
            ("docker-compose.e2e.yaml",),
            ("docker-compose.live-backend-neo4j.yml",),
            ("docker-compose.yaml", "docker-compose.read-api-latency-gate.yaml"),
        )
        environment = {key: value for key, value in os.environ.items() if key != "ESHU_CI_IMAGES"}
        for stack in stacks:
            with self.subTest(stack=stack):
                flags = [part for name in stack for part in ("-f", name)]
                base = subprocess.run(
                    ["docker", "compose", *flags, "--profile", "*", "config", "--format", "json"],
                    cwd=root, env=environment, capture_output=True, text=True, check=False,
                )
                self.assertEqual(base.returncode, 0, "base Compose render failed")
                generated = subprocess.run(
                    ["python3", str(SCRIPT), "--root", str(root), "--ledger", str(ledger)],
                    cwd=root, env=environment, input=base.stdout,
                    capture_output=True, text=True, check=False,
                )
                self.assertEqual(generated.returncode, 0, "CI overlay generation failed")
                with tempfile.TemporaryDirectory() as directory:
                    overlay_path = pathlib.Path(directory) / "overlay.json"
                    overlay_path.write_text(generated.stdout, encoding="utf-8")
                    overlay_path.chmod(0o600)
                    after = subprocess.run(
                        ["docker", "compose", *flags, "-f", str(overlay_path),
                         "--profile", "*", "config", "--format", "json"],
                        cwd=root, env=environment, capture_output=True, text=True, check=False,
                    )
                self.assertEqual(after.returncode, 0, "overlaid Compose render failed")
                before_config = json.loads(base.stdout)
                after_config = json.loads(after.stdout)
                for config in (before_config, after_config):
                    for service in config["services"].values():
                        service.pop("image", None)
                        build = service.get("build")
                        if isinstance(build, dict) and isinstance(build.get("args"), dict):
                            for name in ("ESHU_XX_IMAGE", "ESHU_GO_IMAGE", "ESHU_ALPINE_IMAGE"):
                                build["args"].pop(name, None)
                            if not build["args"]:
                                build.pop("args")
                self.assertTrue(before_config == after_config, "unexpected Compose config change")


class CiImageEntrypointTest(unittest.TestCase):
    """Exercise the production startup call and minimal token policy."""

    def test_corpus_sources_its_ci_overlay_helper(self) -> None:
        """The real startup statement resolves a committed, executable helper."""
        root = SCRIPT.parents[2]
        gate = root / "scripts/verify-golden-corpus-gate.sh"
        calls = [line for line in gate.read_text(encoding="utf-8").splitlines()
                 if line.startswith('. "${repo_root}/scripts/lib/')
                 and "golden_corpus_preflight_ci_overlay" in line]
        self.assertEqual(len(calls), 1)
        result = subprocess.run(
            ["bash", "-euo", "pipefail", "-c", calls[0]],
            env={**os.environ, "repo_root": str(root), "use_compose": "0"},
            text=True, capture_output=True, check=False,
        )
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_e2e_token_is_read_only(self) -> None:
        """The image-consuming E2E workflow grants no implicit write scope."""
        path = SCRIPT.parents[2] / ".github/workflows/e2e-tests.yml"
        workflow = json.loads(subprocess.check_output(
            ["yq", "-o=json", ".", str(path)], text=True,
        ))
        self.assertEqual(workflow.get("permissions"), {"contents": "read"})


if __name__ == "__main__":
    unittest.main()
