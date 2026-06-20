import { useEffect, useState, type FormEvent } from "react";
import type { EshuApiClient } from "../api/client";
import {
  loadDeployableUnitPacket,
  type InvestigationPacketResult
} from "../api/investigationPacket";
import { InvestigationEvidencePacketReader } from "../components/InvestigationEvidencePacketReader";
import { Panel } from "../components/atoms";

interface DeployablePacketFormState {
  readonly generationId: string;
  readonly repositoryId: string;
  readonly scopeId: string;
}

interface DeployableUnitPacketPanelProps {
  readonly canLoad: boolean;
  readonly client?: EshuApiClient;
  readonly initial: DeployablePacketFormState;
}

export function DeployableUnitPacketPanel({
  canLoad,
  client,
  initial
}: DeployableUnitPacketPanelProps): React.JSX.Element {
  const [form, setForm] = useState<DeployablePacketFormState>(initial);
  const [packet, setPacket] = useState<InvestigationPacketResult | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    setForm(initial);
    setPacket(null);
    setError("");
  }, [initial]);

  function loadPacket(event: FormEvent<HTMLFormElement>): void {
    event.preventDefault();
    if (!client) return;
    const scopeId = form.scopeId.trim();
    const generationId = form.generationId.trim();
    if (scopeId.length === 0 || generationId.length === 0) {
      setError("scope_id and generation_id are required.");
      return;
    }
    setBusy(true);
    setError("");
    void loadDeployableUnitPacket(client, {
      generationId,
      maxSourceFacts: 50,
      repositoryId: cleanInput(form.repositoryId),
      scopeId
    }).then((result) => {
      setPacket(result);
      setBusy(false);
    }).catch((err: unknown) => {
      setPacket(null);
      setBusy(false);
      setError(err instanceof Error ? err.message : "failed to load deployable-unit packet");
    });
  }

  return (
    <Panel className="mt" title="Deployable-unit packet" sub="Bounded admission truth from the shared investigation packet route">
      <form className="impact-query" onSubmit={loadPacket}>
        <label>
          <span>Scope ID</span>
          <input
            aria-label="Packet scope ID"
            className="popover-input mono"
            onChange={(event) => setForm((current) => ({ ...current, scopeId: event.target.value }))}
            placeholder="scope_id"
            value={form.scopeId}
          />
        </label>
        <label>
          <span>Generation ID</span>
          <input
            aria-label="Packet generation ID"
            className="popover-input mono"
            onChange={(event) => setForm((current) => ({ ...current, generationId: event.target.value }))}
            placeholder="generation_id"
            value={form.generationId}
          />
        </label>
        <label>
          <span>Repository ID</span>
          <input
            aria-label="Packet repository ID"
            className="popover-input mono"
            onChange={(event) => setForm((current) => ({ ...current, repositoryId: event.target.value }))}
            placeholder="optional repository_id"
            value={form.repositoryId}
          />
        </label>
        <button className="btn-ghost active" disabled={!canLoad || busy} type="submit">
          {busy ? "Loading packet..." : "Load deployable-unit packet"}
        </button>
      </form>
      {error ? <p className="src-err">{error}</p> : null}
      {packet ? (
        <div className="mt">
          <InvestigationEvidencePacketReader packet={packet.packet} />
        </div>
      ) : null}
    </Panel>
  );
}

export function packetFormFromSearch(searchParams: URLSearchParams): DeployablePacketFormState {
  return {
    generationId: searchParams.get("generation_id") ?? "",
    repositoryId: searchParams.get("repository_id") ?? searchParams.get("repo_id") ?? "",
    scopeId: searchParams.get("scope_id") ?? ""
  };
}

function cleanInput(value: string): string | undefined {
  const trimmed = value.trim();
  return trimmed.length === 0 ? undefined : trimmed;
}
