---
paths:
  - "docs/**/*.md"
---

# Documentation

Run the docs build for any navigation or project-guidance change:

```bash
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
  mkdocs build --strict --clean --config-file docs/mkdocs.yml
```

Cite sections by heading, not line number. Line numbers in a cross-file citation
drift silently and no gate catches them; heading anchors cannot.

A capability, maturity, or support claim needs evidence at the tier it asserts.
A dangling proof pointer is not evidence of absence — the committed evidence
usually lives elsewhere, and the canon lists where to look before downgrading
anything outward-facing.
