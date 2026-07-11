# Performance Run Manifest

Use one JSON object per run. Store machine-specific paths and connection data
outside the repository. A public issue or PR may cite only `run_id`, commits,
immutable image identifiers, non-sensitive profile fields, counts, timings, and
the run directory basename.

This is the operator-local detailed manifest. Do not commit it. A committed
public aggregate must conform to `specs/scale-benchmark-artifact.v1.yaml` and
pass `scripts/verify-scale-benchmark-artifact.sh`.

## Required Shape

```json
{
  "schema_version": 1,
  "run_id": "issue-purpose-YYYYMMDDTHHMMSSZ",
  "experiment": {
    "issue": 0,
    "hypothesis": "bounded statement of the theory",
    "classification": ["phase_wall_clock_win", "target_missed"],
    "primary_metric": "launch_to_api_ready_seconds",
    "start_event": "controller_launch",
    "terminal_event": "api_representative_query_success",
    "target_seconds": 1800
  },
  "code": {
    "eshu_commit": "full-commit-id",
    "stable_patch_id": "stable-patch-id-or-empty",
    "base_commit": "full-base-commit-id",
    "worktree_clean": true
  },
  "backend": {
    "kind": "nornicdb",
    "commit": "full-backend-commit-or-empty",
    "image_id": "immutable-image-id",
    "platform": "linux/amd64"
  },
  "topology": {
    "profile_name": "named-known-good-profile",
    "services": ["postgres", "nornicdb", "bootstrap-index", "api", "mcp"],
    "clean_volumes": true,
    "schema_ready": true,
    "pprof_enabled": true,
    "effective_knobs": {
      "parse_workers": 0,
      "projection_workers": 0,
      "reducer_workers": 0,
      "postgres_max_connections": 0
    }
  },
  "workload": {
    "corpus_id": "stable-corpus-name",
    "repository_count": 0,
    "largest_partition": "non-sensitive-partition-label",
    "input_rows": 0
  },
  "milestones_seconds": {
    "schema_ready": 0,
    "collection_complete": 0,
    "source_local_complete": 0,
    "queue_terminal": 0,
    "shared_materialization_complete": 0,
    "search_ready": 0,
    "post_drain_finalizers_complete": 0,
    "bootstrap_exit": 0,
    "api_ready": 0,
    "mcp_ready": 0,
    "representative_query_success": 0
  },
  "truth": {
    "queue_total": 0,
    "queue_succeeded": 0,
    "queue_failed": 0,
    "queue_dead_letter": 0,
    "repositories": 0,
    "files": 0,
    "entities": 0,
    "search_documents": 0,
    "search_vectors": 0,
    "ready_scopes": 0,
    "pending_scopes": 0
  },
  "readback": {
    "api_health": "pass",
    "mcp_health": "pass",
    "index_status": "pass",
    "representative_api_query": "pass",
    "representative_mcp_query": "pass"
  },
  "resources": {
    "cpu_sample_artifact": "basename-or-empty",
    "io_sample_artifact": "basename-or-empty",
    "pprof_artifacts": []
  },
  "cleanup": {
    "containers_removed": true,
    "volumes_removed": true,
    "controllers_stopped": true
  },
  "caveats": []
}
```

## Duration Rendering

Keep seconds numeric in the manifest. Render reports with both exact seconds
and a human value:

- `94.700s (1m34.700s)`
- `1205.924s (20m05.924s)`
- `1869s (31m09s)`

## Comparison Checklist

Before computing a delta, compare the two manifests' start event, terminal
event, corpus, repository count, backend image, platform, profile, services,
clean-volume state, effective knobs, and terminal truth. If a required field
differs, list it and mark the end-to-end comparison non-comparable.

Matching sub-phases may still be compared when their boundaries and inputs are
identical. State explicitly that the narrower comparison does not prove the
overall target.
