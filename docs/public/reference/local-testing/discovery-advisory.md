# Discovery Advisory Playbook

Use this loop when a repository is slow, unexpectedly large, or timeout-heavy.
It is diagnostic evidence, not a stable API contract.

## Loop

Capture before and after reports:

```bash
eshu index /path/to/repo --discovery-report /tmp/eshu-discovery-before.json
```

```bash
eshu index /path/to/repo --discovery-report /tmp/eshu-discovery-after.json
```

Inspect:

- `summary.content_files`
- `summary.content_entities`
- `top_noisy_directories`
- `top_noisy_files`
- `entity_counts.by_type`
- `skip_breakdown`

Choose the narrowest config:

- `.eshu/discovery.json` for auditable vendored, generated, archive, or copied
  third-party roots.
- `preserved_path_globs` when a broad ignored root may contain authored code.
- `.eshuignore` when a plain ignore is enough.

Accept the config only when the after-report shows the intended skip reason and
the repository became cheaper for the intended reason.

Do not change graph-write timeouts, global batch sizes, or NornicDB row caps
until the report proves the input shape is already correct.
