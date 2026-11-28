# semanticqueue

`semanticqueue` builds deterministic, metadata-only queue records for semantic
extraction work. It sits after semantic policy and security guard preflight and
before provider execution or observation emission.

The package owns:

- stable chunk fingerprints and semantic job IDs;
- no-provider, policy-denied, budget-denied, unsafe, unchanged, changed, and
  deleted-source planning states;
- retry and dead-letter lifecycle transitions that preserve the same job and
  fingerprint identity.

The package does not call LLM providers, persist rows, retain prompts, retain
raw responses, or promote semantic output into canonical graph truth.
