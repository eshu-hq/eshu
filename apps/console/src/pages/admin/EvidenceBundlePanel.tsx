// pages/admin/EvidenceBundlePanel.tsx
// Console surface for GET /api/v0/evidence/bundle (issue #4045, final
// acceptance criterion: "Console and CLI both link to or generate the same
// artifact"). This panel calls the exact route the CLI's
// `eshu evidence bundle export --live` composes from, so the two surfaces
// never drift into independently maintained readings. See
// docs/public/reference/evidence-bundle.md.
//
// Stale-load guard: the load effect sets `cancelled = true` in its cleanup so
// a swapped client's in-flight response never commits over a newer source,
// mirroring AdminTokensPanel's pattern.
//
// The route is stack-wide (go/internal/query/evidence_bundle_live.go) and is
// rejected with HTTP 403 for a scoped-bearer token or a browser session that
// is not this deployment's tenant-bound all-scopes owner session — in a
// hosted multi-tenant deployment that includes every browser session, even an
// admin one. That is the route's documented posture, not a bug, so a 403
// renders an explanatory note (provenance "forbidden"), distinct from a real
// error ("unavailable") — the same distinction AdminAuditPanel already draws
// for the global-operator-only audit routes.
//
// This panel deliberately does NOT restate the bundle's redaction or
// validation fields as a safety guarantee: `redaction.rules` names what was
// SCREENED (pattern-matched), not what is proven absent, and
// `validation.status: "passed"` describes the exporter that built the bundle,
// not a certification for whoever reads it later. See
// docs/public/reference/evidence-bundle.md#redaction. This panel shows those
// fields as plain facts and nothing more.
import { useEffect, useState } from "react";

import type { EshuApiClient } from "../../api/client";
import { loadEvidenceBundle } from "../../api/evidenceBundle";
import type {
  EvidenceBundleMissingEvidenceRow,
  EvidenceBundleProvenance,
  EvidenceBundleWire,
} from "../../api/evidenceBundle";
import { Badge, Panel } from "../../components/atoms";

const FORBIDDEN_NOTE =
  "Evidence bundle is stack-wide (no repository or tenant selector) and this session's auth mode is not accepted for it, the same posture as its two stack-wide source routes. Run `eshu evidence bundle export --live`, or call GET /api/v0/evidence/bundle with a shared or admin token, instead.";

const SAFETY_NOTE =
  "Redaction screens known-sensitive shapes; it does not prove their absence. Review a bundle before sending it outside your organization.";

const HEALTH_TONE: Record<string, "crit" | "warn" | "teal" | "neutral" | "violet"> = {
  healthy: "teal",
  progressing: "violet",
  degraded: "warn",
  stalled: "crit",
};

const SEMANTIC_TONE: Record<string, "crit" | "warn" | "teal" | "neutral"> = {
  available: "teal",
  available_but_disabled_for_scope: "neutral",
  disabled_by_policy: "neutral",
  unavailable: "neutral",
  provider_unhealthy: "crit",
};

export function EvidenceBundlePanel({
  client,
}: {
  readonly client?: EshuApiClient;
}): React.JSX.Element {
  const [bundle, setBundle] = useState<EvidenceBundleWire | null>(null);
  const [provenance, setProvenance] = useState<EvidenceBundleProvenance>("live");
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    if (!client) {
      setProvenance("unavailable");
      setBundle(null);
      setLoading(false);
      return;
    }
    setLoading(true);
    void loadEvidenceBundle(client).then((result) => {
      if (cancelled) return;
      setBundle(result.bundle);
      setProvenance(result.provenance);
      setLoading(false);
    });
    return () => {
      cancelled = true;
    };
  }, [client]);

  if (loading) {
    return (
      <Panel title="Evidence bundle">
        <p className="empty-note">Loading evidence bundle…</p>
      </Panel>
    );
  }

  if (provenance === "forbidden") {
    return (
      <Panel title="Evidence bundle">
        <p className="empty-note" role="status">
          {FORBIDDEN_NOTE}
        </p>
      </Panel>
    );
  }

  if (provenance === "unavailable" || bundle === null) {
    return (
      <Panel title="Evidence bundle">
        <p className="unavailable-note">Evidence bundle unavailable from this source.</p>
      </Panel>
    );
  }

  return (
    <Panel
      title="Evidence bundle"
      sub="GET /api/v0/evidence/bundle — the same artifact `eshu evidence bundle export --live` produces"
      action={<DownloadButton bundle={bundle} />}
    >
      <BundleFacts bundle={bundle} />
      <MissingEvidenceTable bundle={bundle} />
      <p className="empty-note">{SAFETY_NOTE}</p>
    </Panel>
  );
}

function DownloadButton({ bundle }: { readonly bundle: EvidenceBundleWire }): React.JSX.Element {
  function download(): void {
    const blob = new Blob([JSON.stringify(bundle, null, 2)], { type: "application/json" });
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = "evidence-bundle.json";
    document.body.appendChild(anchor);
    anchor.click();
    anchor.remove();
    setTimeout(() => URL.revokeObjectURL(url), 2000);
  }

  return (
    <button className="btn-ghost sm" onClick={download} type="button">
      Download JSON
    </button>
  );
}

// BundleFacts reads every field defensively (optional chaining, "not
// reported" fallbacks) even though EvidenceBundleWire types them as required:
// getJson<TData> only casts the parsed JSON, it never validates it against
// the type, so a malformed or truncated payload must render "not reported"
// rather than throw or fabricate a value.
function BundleFacts({ bundle }: { readonly bundle: EvidenceBundleWire }): React.JSX.Element {
  const pipeline = bundle.contents?.pipeline_state;
  const semantic = bundle.contents?.semantic_provider_state;
  const healthTone = pipeline ? (HEALTH_TONE[pipeline.health_state] ?? "neutral") : "neutral";
  const semanticTone = semantic ? (SEMANTIC_TONE[semantic.state] ?? "neutral") : "neutral";
  return (
    <dl className="kv-list" aria-label="Evidence bundle summary">
      <dt>Repositories</dt>
      <dd>{pipeline ? pipeline.repository_count : "not reported"}</dd>

      <dt>Health</dt>
      <dd>
        {pipeline ? <Badge tone={healthTone}>{pipeline.health_state}</Badge> : "not reported"}
      </dd>

      <dt>Queue</dt>
      <dd>
        {pipeline?.queue
          ? `${pipeline.queue.outstanding} outstanding · ${pipeline.queue.dead_letter} dead-lettered · ${pipeline.queue_blocked_count} blocked`
          : "not reported"}
      </dd>

      <dt>Semantic provider</dt>
      <dd>
        {semantic ? (
          <>
            <Badge tone={semanticTone}>{semantic.state}</Badge>
            {semantic.reason ? ` — ${semantic.reason}` : null}
          </>
        ) : (
          "not reported"
        )}
      </dd>

      <dt>Bundle ID</dt>
      <dd className="mono">{bundle.bundle_id || "not reported"}</dd>

      <dt>Generated</dt>
      <dd>{bundle.identity?.created_at || "not reported"}</dd>

      <dt>Validation</dt>
      <dd>{bundle.validation?.status || "not reported"}</dd>
    </dl>
  );
}

// MissingEvidenceTable distinguishes "the response reported zero gaps" from
// "the response did not report gaps at all". `missing_evidence` is absent
// from the route's OpenAPI required-fields list, so a partial or
// version-skewed response can omit or null it; collapsing that unknown state
// into `?? []` would render "no gaps" and hand an operator false confidence
// in an incomplete artifact. Array.isArray narrows on the actual shape
// received, not the declared (but unvalidated) wire type.
function MissingEvidenceTable({
  bundle,
}: {
  readonly bundle: EvidenceBundleWire;
}): React.JSX.Element {
  if (!Array.isArray(bundle.missing_evidence)) {
    return <p className="empty-note">Missing-evidence gaps not reported.</p>;
  }
  // Array.isArray narrows to `any` per its signature, so retype to the
  // declared element type before rendering (see api/cloudDrift.ts stringList).
  const rows = bundle.missing_evidence as readonly EvidenceBundleMissingEvidenceRow[];
  if (rows.length === 0) {
    return <p className="empty-note">No missing-evidence gaps reported.</p>;
  }
  return (
    <table className="data-table" aria-label="Missing evidence">
      <thead>
        <tr>
          <th>Family</th>
          <th>Reason</th>
        </tr>
      </thead>
      <tbody>
        {rows.map((row) => (
          <tr key={row.family}>
            <td>{row.family}</td>
            <td>{row.reason}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
