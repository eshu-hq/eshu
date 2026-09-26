"""Regress MCP answer extraction for both success result shapes."""

import json
from pathlib import Path
import subprocess
import sys
import unittest


HELPER = Path(__file__).resolve().parent / "lib/verify-demo-compose-answers-call-mcp-tool.py"


class MCPAnswerExtractionTest(unittest.TestCase):
    """Check that the demo reads evidence rather than the human summary."""

    def _run_helper(self, document: dict[str, object]) -> subprocess.CompletedProcess[str]:
        """Execute the same CLI entrypoint used by the compose verifier."""
        return subprocess.run(
            [sys.executable, str(HELPER), json.dumps(document)],
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


if __name__ == "__main__":
    unittest.main()
