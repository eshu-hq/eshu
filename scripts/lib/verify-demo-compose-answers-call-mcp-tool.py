"""Extract a demo answer from an MCP tools/call JSON-RPC response."""

import json
import sys
from typing import Any


def _answer_payload(result: dict[str, Any]) -> Any:
    """Read structured evidence, preferring it over the embedded resource."""
    structured = result.get("structuredContent")
    if structured is not None:
        return structured

    content = result.get("content") or []
    for entry in content:
        if not isinstance(entry, dict) or entry.get("type") != "resource":
            continue
        resource = entry.get("resource")
        if isinstance(resource, dict) and isinstance(resource.get("text"), str):
            return json.loads(resource["text"])

    # Older MCP tools sometimes supplied JSON as their only text block.
    for entry in content:
        if isinstance(entry, dict) and entry.get("type") == "text":
            return json.loads(entry["text"])
    raise ValueError("no structuredContent or resource content")


def main() -> int:
    """Print the answer body, returning nonzero for an MCP or parse error."""
    try:
        document = json.load(sys.stdin)
        if not isinstance(document, dict):
            raise ValueError("response is not a JSON object")
        if document.get("error"):
            raise ValueError(f"rpc error: {document['error']}")
        result = document.get("result") or {}
        if not isinstance(result, dict):
            raise ValueError("result is not a JSON object")
        structured = _answer_payload(result)
        if result.get("isError") or (
            isinstance(structured, dict) and structured.get("error")
        ):
            raise ValueError(f"tool reported error: {json.dumps(structured)[:400]}")
        if isinstance(structured, dict) and "data" in structured and "truth" in structured:
            structured = structured["data"]
        print(json.dumps(structured))
        return 0
    except (KeyError, TypeError, ValueError) as error:
        print(f"tools/call: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
