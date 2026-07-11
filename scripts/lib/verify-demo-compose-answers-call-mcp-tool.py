import json
import sys

doc = json.loads(sys.argv[1])
if doc.get("error"):
	sys.stderr.write(f"tools/call rpc error: {doc['error']}\n")
	sys.exit(1)
result = doc.get("result") or {}
structured = result.get("structuredContent")
if structured is None:
	for entry in result.get("content", []):
		if entry.get("type") == "text":
			structured = json.loads(entry["text"])
			break
if structured is None:
	sys.stderr.write("tools/call: no structuredContent or text content\n")
	sys.exit(1)
if result.get("isError") or (isinstance(structured, dict) and structured.get("error")):
	sys.stderr.write(f"tools/call: tool reported error: {json.dumps(structured)[:400]}\n")
	sys.exit(1)
# Unwrap the canonical { data, truth, error } envelope to the answer body.
if isinstance(structured, dict) and "data" in structured and "truth" in structured:
	structured = structured["data"]
print(json.dumps(structured))
