// pages/SbomPage.tsx
// SBOM / attestation evidence surface. Shows the provenance that answers
// "which repo / workload / service does an attestation evidence?" using the
// existing reducer-owned supply-chain read models (count + inventory +
// per-subject list). No new graph reads: the page calls the same bounded
// endpoints the API already exposes. Browse subjects on the left, drill into
// one subject on the right for its attachments, components, and missing hops.
import { useEffect, useState } from "react";

import type { EshuApiClient } from "../api/client";
import {
  loadSbomSummary, loadSbomInventory, loadSbomSubjectDetail
} from "../api/sbomEvidence";
import type {
  SbomSummary, SbomInventory, SbomSubjectDetail, SbomAttachment
} from "../api/sbomEvidence";
import { Panel, StatTile, Badge, TruthChip, FreshDot } from "../components/atoms";
import { uiTruth, uiFresh } from "../console/types";
import "./liveInventory.css";

const ENDPOINT = "GET /api/v0/supply-chain/sbom-attestations/attachments";

function statusTone(status: string): "teal" | "warn" | "crit" | "neutral" {
  if (status === "attached_verified") return "teal";
  if (status === "attached_unverified" || status === "attached_parse_only") return "warn";
  if (status === "unparseable" || status === "subject_mismatch") return "crit";
  return "neutral";
}

export function SbomPage({
  client,
  sourceLabel = "live"
}: {
  readonly client?: EshuApiClient;
  readonly sourceLabel?: string;
}): React.JSX.Element {
  const [summary, setSummary] = useState<SbomSummary | null>(null);
  const [inventory, setInventory] = useState<SbomInventory | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const [detail, setDetail] = useState<SbomSubjectDetail | null>(null);
  const [detailBusy, setDetailBusy] = useState(false);

  useEffect(() => {
    let cancelled = false;
    if (!client) { setSummary(null); setInventory(null); return; }
    void loadSbomSummary(client).then((s) => { if (!cancelled) setSummary(s); });
    void loadSbomInventory(client, "subject_digest", 50, 0).then((i) => { if (!cancelled) setInventory(i); });
    return () => { cancelled = true; };
  }, [client]);

  useEffect(() => {
    let cancelled = false;
    if (!client || !selected) { setDetail(null); return; }
    setDetailBusy(true);
    void loadSbomSubjectDetail(client, selected).then((d) => {
      if (!cancelled) { setDetail(d); setDetailBusy(false); }
    });
    return () => { cancelled = true; };
  }, [client, selected]);

  const total = summary?.total ?? 0;
  const verified = summary?.byStatus.attached_verified ?? 0;
  const sbomCount = summary?.byArtifactKind.sbom ?? 0;
  const attestCount = summary?.byArtifactKind.attestation ?? 0;
  const rows = inventory?.buckets ?? [];
  const summaryProv = summary?.provenance ?? "unavailable";

  return (
    <div className="page">
      <div className="page-intro">
        <h2>SBOM &amp; Attestations</h2>
        <p>Supply-chain attestation evidence — which image subject an SBOM/attestation traces to, and the repository, workload, and service it evidences. Source: <span className="mono">{ENDPOINT}</span>.</p>
      </div>

      <div className="grid g-4">
        <StatTile label="Attachments" value={summaryProv === "unavailable" ? "—" : total} color="var(--teal)" sub={summaryProv === "unavailable" ? "API not available" : "subject attachments"} />
        <StatTile label="Verified" value={summaryProv === "unavailable" ? "—" : `${verified}/${total || 0}`} color="var(--blue)" sub="attached_verified" />
        <StatTile label="SBOM docs" value={summaryProv === "unavailable" ? "—" : sbomCount} color="var(--violet)" sub="artifact_kind=sbom" />
        <StatTile label="Attestations" value={summaryProv === "unavailable" ? "—" : attestCount} color="var(--ember)" sub="artifact_kind=attestation" />
      </div>

      <div className="evidence-workbench evidence-workbench-wide mt" aria-label="SBOM evidence workbench">
        <Panel className="flush" title={`${rows.length} subjects`} sub={inventory === null ? "loading…" : inventoryProvLabel(inventory, sourceLabel)}
          action={inventory?.truth ? <TruthChip level={uiTruth(inventory.truth.level)} /> : null}>
          {inventory === null ? (
            <div className="conn-state compact"><div className="conn-spinner" aria-hidden /><p>Loading SBOM subjects…</p></div>
          ) : (
            <table className="tbl">
              <thead><tr><th>Subject digest</th><th>Attachments</th></tr></thead>
              <tbody>
                {rows.map((r) => (
                  <tr key={r.value} className={selected === r.value ? "is-sel" : undefined} onClick={() => setSelected(r.value)}>
                    <td className="t-name mono" style={{ fontSize: ".76rem" }}>{shortDigest(r.value)}</td>
                    <td><Badge tone="teal">{r.count}</Badge></td>
                  </tr>
                ))}
                {rows.length === 0 ? (
                  <tr><td colSpan={2} className="empty">{emptyInventoryMessage(inventory)}</td></tr>
                ) : null}
              </tbody>
            </table>
          )}
          {inventory?.truncated ? <p className="t-mut" style={{ fontSize: ".72rem", padding: "6px 12px" }}>More subjects available — narrow scope or page on the API.</p> : null}
        </Panel>

        <Panel title="Attestation provenance" sub={detail ? shortDigest(detail.subjectDigest) : "select a subject"}>
          {!selected ? (
            <p className="empty">Select a subject digest to see its attachments and provenance.</p>
          ) : detailBusy || detail === null ? (
            <div className="conn-state compact"><div className="conn-spinner" aria-hidden /><p>Loading provenance…</p></div>
          ) : detail.provenance === "unavailable" ? (
            <p className="empty">Attachment detail unavailable from this source.</p>
          ) : detail.attachments.length === 0 ? (
            <p className="empty">No attachments for this subject.</p>
          ) : (
            <div className="evidence-card-list">
              {detail.attachments.map((att) => <AttachmentCard key={att.attachmentId} att={att} />)}
            </div>
          )}
        </Panel>
      </div>
    </div>
  );
}

function AttachmentCard({ att }: { readonly att: SbomAttachment }): React.JSX.Element {
  return (
    <div className="evidence-card">
      <div className="row" style={{ gap: 8, flexWrap: "wrap", alignItems: "center" }}>
        <Badge tone={statusTone(att.attachmentStatus)}>{att.attachmentStatus || "unknown"}</Badge>
        {att.artifactKind ? <Badge tone="neutral">{att.artifactKind}</Badge> : null}
        {att.format ? <span className="t-mut mono" style={{ fontSize: ".72rem" }}>{att.format}{att.specVersion ? ` ${att.specVersion}` : ""}</span> : null}
        {att.sourceFreshness ? <FreshDot state={uiFresh(att.sourceFreshness === "active" ? "fresh" : att.sourceFreshness)} /> : null}
      </div>

      <ProvenanceList label="Repositories" ids={att.repositoryIds} />
      <ProvenanceList label="Workloads" ids={att.workloadIds} />
      <ProvenanceList label="Services" ids={att.serviceIds} />

      <div className="section-label" style={{ marginTop: 10 }}>Components ({att.componentCount})</div>
      {att.components.length ? (
        <ul className="plain-list">
          {att.components.slice(0, 6).map((c) => (
            <li key={c.id} className="t-mut mono" style={{ fontSize: ".74rem" }}>{c.name}{c.version ? `@${c.version}` : ""}{c.purl ? ` · ${c.purl}` : c.cpe ? ` · ${c.cpe}` : ""}</li>
          ))}
          {att.components.length > 6 ? <li className="t-mut">+{att.components.length - 6} more</li> : null}
        </ul>
      ) : <span className="t-mut">—</span>}

      {att.missingEvidence.length ? (
        <>
          <div className="section-label" style={{ marginTop: 10 }}>Missing evidence</div>
          <div className="row" style={{ gap: 6, flexWrap: "wrap" }}>
            {att.missingEvidence.map((m) => <Badge key={m} tone="warn">{m}</Badge>)}
          </div>
        </>
      ) : null}

      {att.reason ? <p className="t-mut" style={{ fontSize: ".72rem", marginTop: 8 }}>{att.reason}</p> : null}
    </div>
  );
}

function ProvenanceList({ label, ids }: { readonly label: string; readonly ids: readonly string[] }): React.JSX.Element {
  return (
    <div className="row" style={{ gap: 6, flexWrap: "wrap", marginTop: 8, alignItems: "baseline" }}>
      <span className="section-label" style={{ margin: 0 }}>{label}</span>
      {ids.length ? ids.map((id) => <Badge key={id} tone="neutral">{id}</Badge>) : <span className="t-mut">—</span>}
    </div>
  );
}

// shortDigest trims a sha256:… digest for table display while keeping it
// recognizable; the full value stays available as the row key/title.
function shortDigest(value: string): string {
  if (value.length <= 24) return value;
  return `${value.slice(0, 19)}…${value.slice(-6)}`;
}

function inventoryProvLabel(inv: SbomInventory, sourceLabel: string): string {
  if (inv.provenance === "unavailable") return "API not available";
  if (inv.provenance === "empty") return "no subjects";
  return sourceLabel;
}

function emptyInventoryMessage(inv: SbomInventory): string {
  if (inv.provenance === "unavailable") return "SBOM evidence unavailable — requires the sbom-attestation collector and reducer read model.";
  return "No SBOM/attestation subjects from this source.";
}
