// pages/tokens/CreateApiTokenControl.tsx
// Self-service "create my own personal API token" control (issue #5164).
// Flow: idle -> form (label required, expiry optional) -> submit ->
// TokenRevealPanel (reveal-once) -> dismiss refreshes the caller's list.
//
// createPersonalApiToken (api/userProfile.ts) omits user_id; the backend
// resolves it from the caller's own session (go/internal/query/
// local_identity_api_tokens.go resolveSelfServiceAPITokenUserID), so this
// mints a token for the CALLER, never an arbitrary target. A 403 is
// rendered as an explicit "you don't have permission" message, not a
// crash — see createPersonalApiToken's CreateTokenResult union.
import { useState } from "react";

import type { EshuApiClient } from "../../api/client";
import { createPersonalApiToken } from "../../api/userProfile";
import type { CreatedAPIToken } from "../../api/userProfile";
import { TokenRevealPanel } from "./TokenRevealPanel";

import "./apiTokenControls.css";

type Phase = "idle" | "form" | "revealing";

export function CreateApiTokenControl({
  client,
  onCreated,
}: {
  readonly client: EshuApiClient;
  readonly onCreated: () => void;
}): React.JSX.Element {
  const [phase, setPhase] = useState<Phase>("idle");
  const [label, setLabel] = useState("");
  const [expiresAt, setExpiresAt] = useState("");
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState<string | null>(null);
  const [created, setCreated] = useState<CreatedAPIToken | null>(null);

  async function handleCreate(): Promise<void> {
    const trimmedLabel = label.trim();
    if (trimmedLabel.length === 0) {
      setMessage("A label is required.");
      return;
    }
    setBusy(true);
    setMessage(null);
    const result = await createPersonalApiToken(client, {
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
      setMessage("You don't have permission to create API tokens. Ask a tenant admin.");
      return;
    }
    setMessage(result.message);
  }

  function handleDismiss(): void {
    setCreated(null);
    setPhase("idle");
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
          Create token
        </button>
      </div>
    );
  }

  return (
    <div className="token-create">
      <label htmlFor="token-create-label">Label</label>
      <input
        id="token-create-label"
        type="text"
        value={label}
        disabled={busy}
        placeholder="e.g. laptop, CI pipeline"
        onChange={(e) => setLabel(e.target.value)}
      />
      <label htmlFor="token-create-expires">Expires (optional)</label>
      <input
        id="token-create-expires"
        type="date"
        value={expiresAt}
        disabled={busy}
        onChange={(e) => setExpiresAt(e.target.value)}
      />
      {message ? <p className="token-create-error">{message}</p> : null}
      <div className="token-create-actions">
        <button
          type="button"
          disabled={busy || label.trim().length === 0}
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
