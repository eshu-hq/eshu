# Correlation Admission Golden

This fixture suite defines public-safe intent for reducer admission audit cases.
It is not a source corpus yet; the expected JSON is the owned contract that
dogfood and CI collectors compare against after they gather reducer decisions,
graph facts, and API/MCP readbacks.

The cases cover admitted, rejected, ambiguous, stale replay, and missing
evidence outcomes across service/deployment, package/supply-chain, and
cloud/resource correlation paths.
