---
name: eshu-humanizer
description: Polish the prose of an Eshu PR body/title, review reply, issue update, or doc edit before it publishes — lead with the concrete change, cut AI-sounding filler, keep quoted evidence verbatim, and match confidence to what was measured. A prose pass, not a review verdict (eshu-code-review) or thread-resolution mechanism (resolve-review-threads).
---

# Eshu humanizer

Apply these principles to the text being written, including delegated drafts.
A short reply needs a short pass. Preserve the reader's context and requested
format; explain project shorthand when the intended reader may not know it.

- Lead with the concrete change or finding and why it matters to the reader.
  Use familiar words, active verbs, and specific actors. Cut filler and inflated
  significance; keep concrete consequences and necessary technical precision.
- Avoid canned AI phrasing such as “delve,” “seamless,” “it's worth noting,”
  “not just X but Y,” and unsupported claims that work is “robust.” Do not turn
  word choice into a mechanical blacklist or force a particular sentence rhythm.
- Preserve quoted evidence exactly: logs, identifiers, test names, commands,
  reviewer text, and cited titles must remain verbatim.
- State what the evidence proves and its limits together. Label hypotheses and
  theoretical failures as such; preserve that confidence when repeating a claim.
  Use measured numbers with their source, or say the measurement is absent.
- Put a needed decision or blocker near the top. Own a mistake briefly and
  continue the task. Follow the repo rule against AI attribution.
- Choose paragraphs, lists, or tables for the information. Avoid ornamental
  bold, repetitive headings, forced triples, and excess em dashes.

For a PR body, review response, or issue closure, consult
[publishing examples](references/publishing.md) when a format example helps.
Scale detail to the change. A PR title and body must describe the final diff;
rewrite them when review changes the implementation. Never resolve a review
thread with only “fixed”: identify the change and supporting evidence.
