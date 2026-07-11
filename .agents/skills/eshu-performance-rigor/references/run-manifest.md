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
    "target_seconds": 1800,
    "target_contribution": {
      "current_total_seconds": 1869,
      "required_saving_seconds": 69,
      "candidate_stage": "relationship_backfill",
      "candidate_stage_seconds": 555.188,
      "maximum_recoverable_seconds": 555.188,
      "minimum_worthwhile_saving_seconds": 120,
      "expected_saving_seconds": 180,
      "expected_margin_seconds": 111
    }
  },
  "code": {
    "eshu_commit": "full-commit-id",
    "accepted_commit": "merged-equivalent-commit-or-empty",
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
  "environment": {
    "hardware_class": "stable-non-sensitive-hardware-class",
    "machine_profile": {
      "category": "cloud_vm",
      "provider": "aws",
      "model": "ec2",
      "instance_type": "provider-instance-type-or-empty",
      "display_name": "AWS EC2 provider-instance-type, 128 GiB"
    },
    "reference_profile": "named-reference-profile-or-empty",
    "absolute_target_applicable": true,
    "resource_envelope": {
      "cpu_architecture": "amd64",
      "logical_cpu_count": 0,
      "memory_bytes": 0,
      "storage_kind": "local-ssd",
      "os_class": "linux",
      "container_cpu_limit": null,
      "container_memory_limit_bytes": null
    }
  },
  "topology": {
    "profile_name": "named-known-good-profile",
    "services": ["postgres", "nornicdb", "bootstrap-index", "api", "mcp"],
    "clean_volumes": true,
    "schema_ready": true,
    "pprof_enabled": true,
    "compose_service_limits": {
      "postgres": {
        "replicas": 1,
        "cpu_limit": null,
        "memory_limit_bytes": null,
        "memory_reservation_bytes": null
      }
    },
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
  "phase_durations_seconds": {
    "collection": 0,
    "relationship_backfill": 0,
    "post_drain_finalization": 0
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
    "sampling_interval_seconds": 5,
    "compose_service_config_artifact": "compose-service-config.json",
    "compose_service_usage_artifact": "compose-service-usage.jsonl",
    "host_pressure_artifact": "host-pressure.jsonl",
    "service_usage_summary": [
      {
        "service": "postgres",
        "peak_cpu_percent": 0,
        "peak_memory_bytes": 0,
        "peak_memory_percent": 0,
        "block_read_bytes": 0,
        "block_write_bytes": 0,
        "restart_count": 0,
        "oom_killed": false
      }
    ],
    "cpu_sample_artifact": "basename-or-empty",
    "io_sample_artifact": "basename-or-empty",
    "pprof_artifacts": []
  },
  "retention": {
    "mode": "stop-and-preserve",
    "compose_project": "issue-run-project",
    "decided_at": "YYYY-MM-DDTHH:MM:SSZ"
  },
  "cleanup": {
    "containers_removed": true,
    "volumes_removed": true,
    "controllers_stopped": true
  },
  "caveats": []
}
```

Use `null` plus a caveat for a value that was not captured. Do not write `0`
for an unknown count or timestamp; zero is an observed result.

The example uses zero only as a shape placeholder. A real evidence manifest
must record observed positive CPU and memory values or use `null` with a caveat.

Resource sampling must span the measured run and tag samples with the current
pipeline phase. `compose_service_limits` is an input to comparability;
`service_usage_summary` is measured output. Report before/after resource deltas
per service rather than requiring observed consumption to be identical.

`eshu_commit` is the commit that produced the evidence. Set `accepted_commit`
only when a later merged or rebased commit is proven equivalent through the
stable patch-ID carry-forward contract.

## Target Contribution

The target budget belongs in the manifest before the expensive run. Calculate:

- `required_saving_seconds = max(current_total_seconds - target_seconds, 0)`;
- `maximum_recoverable_seconds` from the measured candidate stage;
- `expected_saving_seconds` from the theory shim or bounded replay; and
- `expected_margin_seconds = expected_saving_seconds - required_saving_seconds`.

Do not escalate a candidate whose stage is too small to recover the target gap
unless it serves a separately named SLO or resource objective.

Milestones are elapsed from the declared start event. Phase durations are the
measured work inside a phase. Keep both when they differ; a phase duration must
not be substituted for launch-to-milestone elapsed time.

## Duration Rendering

Keep seconds numeric in the manifest. Render reports with both exact seconds
and a human value:

- `94.700s (1m34.700s)`
- `1205.924s (20m05.924s)`
- `1869s (31m09s)`

## Comparison Checklist

Before computing a delta, compare the two manifests' start event, terminal
event, corpus, repository count, backend image, platform, profile, services,
clean-volume state, effective knobs, measured resource envelope, absolute-target
applicability, and terminal truth. If a required field differs, list it and
mark the end-to-end comparison non-comparable.

Compare configured Compose replicas, CPU limits, and memory limits/reservations
as inputs. Separately compare per-service peaks and host pressure as outcomes;
differences there may explain the speedup or reveal a capacity regression.

Use `machine_profile.display_name` in human reports. Examples include
`AWS EC2 <instance type>, 128 GiB`, `MacBook Pro, 16 GiB`,
`MacBook Pro, 32 GiB`, and `MacBook Pro, 128 GiB`. Compare the structured
profile and measured resource envelope, not the display string alone.

An absolute target such as a full-corpus wall-clock threshold applies only when
`reference_profile` names the accepted profile,
`absolute_target_applicable` is true, and the measured resource envelope is
comparable. A smaller contributor machine can still supply valid correctness
and same-machine relative before/after proof; it must not be classified as a
product regression merely because it misses the reference machine's absolute
duration.

Matching sub-phases may still be compared when their boundaries and inputs are
identical. State explicitly that the narrower comparison does not prove the
overall target.

## Promotion And Retention

Promote a manifest to the operator-local accepted baseline registry only after
the worktree/commit, queue, readiness, API, MCP, and representative readback are
clean. A target-missed run can still be the truthful current baseline.

Use one of these retention values:

- `stop-and-preserve` while expensive data may be needed for review;
- `keep-live` only with explicit user intent; or
- `destroy` after merge or final disposition.

The Compose project must be unique to the issue/run. Cleanup acts only on that
label and records the resulting container, volume, and controller state.
