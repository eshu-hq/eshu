// pages/admin/CreateServicePrincipalTokenControl.tsx
// Admin "create a service_principal token" control (issue #5164). A service
// principal is never the admin's own identity, so — unlike the self-service
// personal-token control (pages/tokens/CreateApiTokenControl.tsx) — the
// target service_principal_id is always supplied explicitly. The admin
// session already carries all-scope auth (AdminTokensPanel is only reachable
// there), so this route works today with no backend change. Reuses
// TokenRevealPanel for the identical reveal-once treatment.
import { useState } from "react";

import { createServicePrincipalApiToken } from "../../api/adminConsole";
import type { EshuApiClient } from "../../api/client";
import type { CreatedAPIToken } from "../../api/userProfile";
import { TokenRevealPanel } from "../tokens/TokenRevealPanel";

import "../tokens/apiTokenControls.css";

type Phase = "idle" | "form" | "revealing";

export function CreateServicePrincipalTokenControl({
  client,
  onCreated,
}: {
  readonly client: EshuApiClient;
  readonly onCreated: () => void;
}): React.JSX.Element {
  const [phase, setPhase] = useState<Phase>("idle");
  const [servicePrincipalId, setServicePrincipalId] = useState("");
  const [label, setLabel] = useState("");
  const [expiresAt, setExpiresAt] = useState("");
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState<string | null>(null);
  const [created, setCreated] = useState<CreatedAPIToken | null>(null);

  async function handleCreate(): Promise<void> {
    const trimmedSPID = servicePrincipalId.trim();
    const trimmedLabel = label.trim();
    if (trimmedSPID.length === 0) {
      setMessage("A service principal ID is required.");
      return;
    }
    if (trimmedLabel.length === 0) {
      setMessage("A label is required.");
      return;
    }
    setBusy(true);
    setMessage(null);
    const result = await createServicePrincipalApiToken(client, {
      servicePrincipalId: trimmedSPID,
      displayLabel: trimmedLabel,
      expiresAt: expiresAt.length > 0 ? new Date(expiresAt).toISOString() : undefined,
    });
    setBusy(false);
    if (result.status === "created") {
      setCreated(result.token);
      setPhase("revealing");
      return;
    }
    if (result.status === "forbidden") {
      setMessage("You don't have permission to create API tokens.");
      return;
    }
    setMessage(result.message);
  }

  function handleDismiss(): void {
    setCreated(null);
    setPhase("idle");
    setServicePrincipalId("");
    setLabel("");
    setExpiresAt("");
    setMessage(null);
    onCreated();
  }

  if (phase === "revealing" && created) {
    return <TokenRevealPanel token={created} onDismiss={handleDismiss} />;
  }

  if (phase === "idle") {
    return (
      <div className="token-create">
        {message ? <p className="token-create-error">{message}</p> : null}
        <button
          type="button"
          className="token-create-start"
          onClick={() => {
            setMessage(null);
            setPhase("form");
          }}
        >
          Create service principal token
        </button>
      </div>
    );
  }

  return (
    <div className="token-create">
      <label htmlFor="sp-token-create-id">Service principal ID</label>
      <input
        id="sp-token-create-id"
        type="text"
        value={servicePrincipalId}
        disabled={busy}
        placeholder="e.g. svc_ci_pipeline"
        onChange={(e) => setServicePrincipalId(e.target.value)}
      />
      <label htmlFor="sp-token-create-label">Label</label>
      <input
        id="sp-token-create-label"
        type="text"
        value={label}
        disabled={busy}
        placeholder="e.g. CI pipeline"
        onChange={(e) => setLabel(e.target.value)}
      />
      <label htmlFor="sp-token-create-expires">Expires (optional)</label>
      <input
        id="sp-token-create-expires"
        type="date"
        value={expiresAt}
        disabled={busy}
        onChange={(e) => setExpiresAt(e.target.value)}
      />
      {message ? <p className="token-create-error">{message}</p> : null}
      <div className="token-create-actions">
        <button
          type="button"
          disabled={busy || servicePrincipalId.trim().length === 0 || label.trim().length === 0}
          onClick={() => void handleCreate()}
        >
          {busy ? "Creating…" : "Create"}
        </button>
        <button
          type="button"
          className="btn-ghost"
          disabled={busy}
          onClick={() => {
            setPhase("idle");
            setMessage(null);
          }}
        >
          Cancel
        </button>
      </div>
    </div>
  );
}
