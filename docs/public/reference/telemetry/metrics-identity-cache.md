# Identity Fact Cache Metrics

This catalog covers the reducer identity fact epoch cache. It is part of the
[Reducer And Storage Metrics](metrics-reducer-storage.md) family and has its
own page so that catalog stays under the Markdown file cap.

The reducer shares one identity fact set (container-image and Kubernetes correlation handlers) through an
epoch-validated cache. These counters show whether callers share one load or queue behind many.

| Metric | Type | Use |
| --- | --- | --- |
| `eshu_dp_identity_cache_reload_total` | counter | Load attempts, including the one in-flight retry after an epoch move. |
| `eshu_dp_identity_cache_load_retry_total` | counter | Loads discarded and repeated inside one flight because the epoch moved during the paged load (at most one retry per flight). |
| `eshu_dp_identity_cache_reload_duration_seconds` | histogram | Duration of every load, cached or not. |
| `eshu_dp_identity_cache_passthrough_total` | counter | Loads that were not cached. Label `reason`: `cap_exceeded` and `size_unknown` serve the consistent set to its callers uncached; `epoch_moved` and `probe_error` discard an unvalidated set (the leader's item fails with the `identity_epoch_unstable` failure class). |
| `eshu_dp_identity_cache_flight_waiter_total` | counter | Callers that arrived during a load. Label `outcome`: `shared` (served that load's rows), `shared_error`, `stale_epoch` (the caller's epoch probe differs from the load's start epoch; retried), `leader_canceled` (retried), `torn_set` (the load could not be validated against a stable epoch; retried), `gave_up_flights` (the caller waited out 3 flights that did not serve it; sustained churn), `gave_up_wall` (the caller waited one heartbeat interval in total; a slow flight). Both `gave_up_*` outcomes follow a final epoch probe that found no cache entry for the current epoch and fail the item with `identity_epoch_unstable`; a give-up that waited runs its own probe, and a caller that arrives at the lead decision with its budget spent is covered by the probe at the top of that same pass. `shared` is the served-from-a-flight outcome.|
| `eshu_dp_identity_cache_hit_total`, `eshu_dp_identity_cache_miss_total` | counter | Epoch-validated cache hits and misses. |
| `eshu_dp_identity_cache_probe_duration_seconds` | histogram | Duration of the epoch probe, which runs on every call. |

A leader whose epoch moved on both load attempts fails its item with the retryable failure class
`identity_epoch_unstable`; no item is decided on such a set. The class is the `failure_class` label of
`eshu_dp_reducer_retry_surge_total` and, once `ESHU_REDUCER_MAX_ATTEMPTS` is exhausted, of
`eshu_dp_queue_dead_letters_total{queue="reducer"}`, where the dead-letter row keeps the class. Count torn items with
`sum(increase(eshu_dp_queue_dead_letters_total{queue="reducer",failure_class="identity_epoch_unstable"}[1h]))`. The
class counts claim attempts, so the existing attempt limit bounds it. Waiters of that flight show as
`flight_waiter_total{outcome="torn_set"}`.

A waiter does not wait without limit. It gives up after 3 flights that did not serve it, or after one reducer heartbeat
interval of total waiting (30 s with the default one-minute claim lease), whichever comes first. It then makes one final
probe: if a consistent flight has filled the cache for the current epoch, the waiter is served that set as a hit. Otherwise
(a caller that arrives at the lead decision with its budget already spent relies on the probe it ran at the top of that
same pass and does not probe twice) its item fails with the retryable `identity_epoch_unstable` class, counted as
`flight_waiter_total{outcome="gave_up_flights"}` or `{outcome="gave_up_wall"}`. When `gave_up_flights` dominates, the epoch
is churning; when `gave_up_wall` dominates, a single flight is slow but stable (a load longer than about 30 s). A call's
time inside the identity cache is bounded by one heartbeat interval plus two load attempts and a few epoch probes. A call
that has exhausted its wait budget never starts a load: it fails retryably and the next caller leads with a fresh budget.
Without these bounds, sustained epoch churn parked every worker of the pool on a flight and the pool stalled.

A load error is shared. When a flight's load fails with an error other than the leader's own cancellation, every joinable waiter
receives that error (`flight_waiter_total{outcome="shared_error"}`) and each of those items loses one queue
attempt. A burst of `shared_error` outcomes alongside `eshu_dp_queue_dead_letters_total{queue="reducer"}` growth means one
database error cost each parked item one queue attempt. See the identity-flight evidence note for the tradeoff.

A healthy domain shows many `shared` waiters per `reload_total`. A high `passthrough_total{reason="epoch_moved"}`,
`flight_waiter_total{outcome="stale_epoch"}` or `load_retry_total` means the active set moves faster than a load.
