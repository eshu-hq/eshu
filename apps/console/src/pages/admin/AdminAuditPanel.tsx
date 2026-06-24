// pages/admin/AdminAuditPanel.tsx
// Audit panel: renders governance audit events and an aggregate summary. The
// backend audit routes are GLOBAL shared-operator only, so a tenant admin
// receives HTTP 403. That is a scope boundary, not a failure: when either
// loader reports provenance "forbidden", the panel renders an operator-scope
// note (referencing #3717) instead of an error. A real error renders
// "unavailable". Renders only audit-safe fields (event type, classes, decision,
// reason, timestamps, correlation id) — never actor, scope, or policy hashes.
import { useEffect, useState } from "react";
import type { EshuApiClient } from "../../api/client";
import { loadAuditEvents, loadAuditSummary } from "../../api/adminConsole";
import type {
  AuditEventItem,
  AuditSummaryData,
  AdminAuditProvenance
} from "../../api/adminConsole";
import { Panel } from "../../components/atoms";
import { fmt, dash } from "./adminFormat";

// OPERATOR_SCOPE_NOTE is shown when the audit routes return 403 for a tenant
// admin. The audit surface is global-operator-only; tenant scoping is tracked
// in #3717.
const OPERATOR_SCOPE_NOTE =
  "Global operator audit — not available for tenant admins (#3717).";

function SummaryView({ summary }: { readonly summary: AuditSummaryData }): React.JSX.Element {
  return (
    <dl className="kv-list" aria-label="Audit summary">
      <dt>Total</dt>
      <dd>{summary.total ?? 0}</dd>
      <dt>Allowed</dt>
      <dd>{summary.allowed ?? 0}</dd>
      <dt>Denied</dt>
      <dd>{summary.denied ?? 0}</dd>
      <dt>Unavailable</dt>
      <dd>{summary.unavailable ?? 0}</dd>
      <dt>Last occurred</dt>
      <dd>{fmt(summary.last_occurred_at)}</dd>
    </dl>
  );
}

export function AdminAuditPanel({
  client
}: {
  readonly client?: EshuApiClient;
}): React.JSX.Element {
  const [events, setEvents] = useState<readonly AuditEventItem[]>([]);
  const [summary, setSummary] = useState<AuditSummaryData | null>(null);
  const [eventsProv, setEventsProv] = useState<AdminAuditProvenance>("live");
  const [summaryProv, setSummaryProv] = useState<AdminAuditProvenance>("live");
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    if (!client) {
      setEventsProv("unavailable");
      setSummaryProv("unavailable");
      setLoading(false);
      return;
    }
    void Promise.all([loadAuditEvents(client), loadAuditSummary(client)]).then(([e, s]) => {
      if (cancelled) return;
      setEvents(e.events);
      setEventsProv(e.provenance);
      setSummary(s.summary);
      setSummaryProv(s.provenance);
      setLoading(false);
    });
    return () => {
      cancelled = true;
    };
  }, [client]);

  if (loading) {
    return (
      <Panel title="Audit">
        <p className="empty-note">Loading audit…</p>
      </Panel>
    );
  }

  // Forbidden on either route → render the operator-scope note, not an error.
  if (eventsProv === "forbidden" || summaryProv === "forbidden") {
    return (
      <Panel title="Audit">
        <p className="empty-note" role="status">
          {OPERATOR_SCOPE_NOTE}
        </p>
      </Panel>
    );
  }

  if (eventsProv === "unavailable" && summaryProv === "unavailable") {
    return (
      <Panel title="Audit">
        <p className="unavailable-note">Audit unavailable from this source.</p>
      </Panel>
    );
  }

  return (
    <Panel title="Audit">
      {summaryProv === "unavailable" ? (
        <p className="unavailable-note">Audit summary unavailable from this source.</p>
      ) : summary ? (
        <SummaryView summary={summary} />
      ) : null}

      {eventsProv === "unavailable" ? (
        <p className="unavailable-note">Audit events unavailable from this source.</p>
      ) : events.length === 0 ? (
        <p className="empty-note">No audit events found.</p>
      ) : (
        <table className="data-table" aria-label="Audit events">
          <thead>
            <tr>
              <th>Event type</th>
              <th>Decision</th>
              <th>Reason</th>
              <th>Actor class</th>
              <th>Scope class</th>
              <th>Occurred</th>
            </tr>
          </thead>
          <tbody>
            {events.map((ev, i) => (
              <tr key={ev.correlation_id ?? i}>
                <td>{dash(ev.event_type)}</td>
                <td>{dash(ev.decision)}</td>
                <td>{dash(ev.reason_code)}</td>
                <td>{dash(ev.actor_class)}</td>
                <td>{dash(ev.scope_class)}</td>
                <td>{fmt(ev.occurred_at)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </Panel>
  );
}
