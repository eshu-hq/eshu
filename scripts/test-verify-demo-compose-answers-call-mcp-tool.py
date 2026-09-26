"""Regress MCP answer extraction for both success result shapes."""

import json
import os
import re
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


HELPER = Path(__file__).resolve().parent / "lib/verify-demo-compose-answers-call-mcp-tool.py"
WRAPPER = Path(__file__).resolve().parent / "verify-demo-compose-answers.sh"


class MCPAnswerExtractionTest(unittest.TestCase):
    """Check that the demo reads evidence rather than the human summary."""

    def _run_helper(self, document: dict[str, object]) -> subprocess.CompletedProcess[str]:
        """Execute the same CLI entrypoint used by the compose verifier."""
        return subprocess.run(
            [sys.executable, str(HELPER)],
            input=json.dumps(document),
            capture_output=True,
            text=True,
            check=False,
        )

    def test_resource_only_success_preserves_full_answer(self) -> None:
        """A large successful MCP result can omit structuredContent."""
        envelope = {
            "data": {"results": [{"id": 1, "body": "full evidence"}]},
            "truth": {"level": "exact"},
            "error": None,
        }
        document = {
            "result": {
                "content": [
                    {"type": "text", "text": "Returned 1 result(s)."},
                    {
                        "type": "resource",
                        "resource": {
                            "uri": "eshu://tool-result/envelope",
                            "mimeType": "application/eshu.envelope+json",
                            "text": json.dumps(envelope),
                        },
                    },
                ]
            }
        }

        completed = self._run_helper(document)

        self.assertEqual(completed.returncode, 0, completed.stderr)
        self.assertEqual(json.loads(completed.stdout), envelope["data"])

    def test_structured_content_precedes_resource(self) -> None:
        """The usual two-copy result still uses structuredContent."""
        document = {
            "result": {
                "structuredContent": {"data": {"count": 2}, "truth": {}, "error": None},
                "content": [
                    {
                        "type": "resource",
                        "resource": {"text": '{"data":{"count":1},"truth":{},"error":null}'},
                    }
                ],
            }
        }

        completed = self._run_helper(document)

        self.assertEqual(completed.returncode, 0, completed.stderr)
        self.assertEqual(json.loads(completed.stdout), {"count": 2})

    def test_resource_only_error_is_not_reported_as_success(self) -> None:
        """An error envelope remains an error without structuredContent."""
        document = {
            "result": {
                "isError": True,
                "content": [
                    {"type": "text", "text": "Request failed."},
                    {
                        "type": "resource",
                        "resource": {
                            "text": '{"data":null,"truth":null,"error":{"code":"bad_input"}}'
                        },
                    },
                ],
            }
        }

        completed = self._run_helper(document)

        self.assertNotEqual(completed.returncode, 0)
        self.assertIn("tool reported error", completed.stderr)

    def test_empty_malformed_and_nonobject_input_fail(self) -> None:
        """The stdin parser rejects invalid responses without printing a body."""
        for raw in ("", "{invalid", "[]"):
            with self.subTest(raw=raw):
                completed = subprocess.run(
                    [sys.executable, str(HELPER)],
                    input=raw,
                    capture_output=True,
                    text=True,
                    check=False,
                )
                self.assertNotEqual(completed.returncode, 0)
                self.assertEqual(completed.stdout, "")
                self.assertIn("tools/call:", completed.stderr)

    def test_rpc_error_fails(self) -> None:
        """A JSON-RPC failure propagates through the stdin CLI."""
        completed = self._run_helper({"error": {"code": -32603}})

        self.assertNotEqual(completed.returncode, 0)
        self.assertIn("rpc error", completed.stderr)

    def test_wrapper_propagates_parse_error_without_logging_input(self) -> None:
        """A malformed MCP body fails the shell call without echoing its input."""
        sentinel = "private_fixture_value"
        wrapper_source = WRAPPER.read_text(encoding="utf-8")
        function = re.search(r"(?ms)^call_mcp_tool\(\) \{\n.*?^\}", wrapper_source)
        self.assertIsNotNone(function)

        with tempfile.TemporaryDirectory() as directory:
            fixture = Path(directory) / "response.json"
            fixture.write_text("{invalid:" + sentinel, encoding="utf-8")
            environment = os.environ.copy()
            environment["ESHU_MCP_FIXTURE"] = str(fixture)
            environment["ESHU_REPO_ROOT"] = str(WRAPPER.parent.parent)
            completed = subprocess.run(
                [
                    "bash",
                    "-c",
                    (
                        'set -euo pipefail; repo_root="$ESHU_REPO_ROOT"; '
                        'mcp_base=http://localhost; '
                        'curl() { cat "$ESHU_MCP_FIXTURE"; }; '
                        + function.group()
                        + "\ncall_mcp_tool sample '{}'"
                    ),
                ],
                capture_output=True,
                text=True,
                env=environment,
                check=False,
            )

        self.assertNotEqual(completed.returncode, 0)
        self.assertEqual(completed.stdout, "")
        self.assertIn("tools/call:", completed.stderr)
        self.assertNotIn(sentinel, completed.stderr)

    def test_wrapper_passes_large_resource_without_argument_limit(self) -> None:
        """The wrapper can extract and assert a full resource over 128 KiB."""
        answer = {
            "count": 25,
            "findings": [{"rank": rank, "body": "x" * 5_600} for rank in range(25)],
        }
        envelope = {"data": answer, "truth": {}, "error": None}
        document = {
            "result": {
                "content": [
                    {"type": "text", "text": "Summary."},
                    {"type": "resource", "resource": {"text": json.dumps(envelope)}},
                ]
            }
        }
        wrapper_source = WRAPPER.read_text(encoding="utf-8")
        functions = []
        for name in ("call_mcp_tool", "assert_fields_present"):
            function = re.search(
                rf"(?ms)^{name}\(\) \{{\n.*?^\}}", wrapper_source
            )
            self.assertIsNotNone(function)
            functions.append(function.group())
        counter = re.search(r"(?m)^json_count\(\) \{.*$", wrapper_source)
        self.assertIsNotNone(counter)
        functions.append(counter.group())

        with tempfile.TemporaryDirectory() as directory:
            fixture = Path(directory) / "response.json"
            fixture.write_text(json.dumps(document), encoding="utf-8")
            environment = os.environ.copy()
            environment["ESHU_MCP_FIXTURE"] = str(fixture)
            environment["ESHU_REPO_ROOT"] = str(WRAPPER.parent.parent)
            completed = subprocess.run(
                [
                    "bash",
                    "-c",
                    (
                        'set -euo pipefail; repo_root="$ESHU_REPO_ROOT"; mcp_base=http://localhost; '
                        'curl() { cat "$ESHU_MCP_FIXTURE"; }; '
                        + "\n".join(functions)
                        + "\nbody=\"$(call_mcp_tool sample '{}')\"\n"
                        + 'assert_fields_present "large resource" "$body" count findings\n'
                        + 'count="$(json_count "$body")"\n'
                        + 'printf "COUNT=%s\\nANSWER=%s\\n" "$count" "$body"\n'
                    ),
                ],
                capture_output=True,
                text=True,
                env=environment,
                check=False,
            )

        self.assertEqual(completed.returncode, 0, completed.stderr)
        self.assertIn("COUNT=25\n", completed.stdout)
        self.assertEqual(json.loads(completed.stdout.split("ANSWER=", 1)[1]), answer)


if __name__ == "__main__":
    unittest.main()
