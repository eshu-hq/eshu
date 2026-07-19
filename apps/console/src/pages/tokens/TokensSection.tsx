// pages/tokens/TokensSection.tsx
// ProfilePage's API-tokens panel: self-service create (issue #5164), the
// caller's own token list with a Label column (issue #3708's display_label),
// and per-row rotate/revoke. Extracted out of ProfilePage.tsx to keep that
// file under the repo's 500-line cap (mirrors the TOTPEnrollmentControl.tsx
// extraction, issue #5072).
//
// Stale-load guard: rotate/revoke are confirm-then-call, disable the acted-on
// row while in flight, and call onChanged to make the parent refetch the
// list — the same refreshKey pattern AdminTokensPanel.tsx uses.
import { useState } from "react";

import { CreateApiTokenControl } from "./CreateApiTokenControl";
import { fmt, isExpired } from "./tokenFormat";
import { TokenRevealPanel } from "./TokenRevealPanel";
import type { EshuApiClient } from "../../api/client";
import { rotatePersonalApiToken, revokeApiToken } from "../../api/userProfile";
import type { APITokenItem, CreatedAPIToken } from "../../api/userProfile";
import { Panel, Badge } from "../../components/atoms";

function statusBadge(revoked: boolean, expired: boolean): React.JSX.Element {
  if (revoked) return <Badge tone="crit">revoked</Badge>;
  if (expired) return <Badge tone="neutral">expired</Badge>;
  return <Badge tone="teal">active</Badge>;
}

export function TokensSection({
  client,
  tokens,
  unavailable,
  onChanged,
}: {
  readonly client?: EshuApiClient;
  readonly tokens: readonly APITokenItem[];
  readonly unavailable: boolean;
  // onChanged is called after a successful create/rotate/revoke so the
  // parent can refetch the authoritative list from the server.
  readonly onChanged: () => void;
}): React.JSX.Element {
  const [busyId, setBusyId] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [rotated, setRotated] = useState<CreatedAPIToken | null>(null);

  async function handleRotate(tokenId: string): Promise<void> {
    if (!client) return;
    if (
      !globalThis.confirm?.(`Rotate API token ${tokenId}? The old token stops working immediately.`)
    ) {
      return;
    }
    setBusyId(tokenId);
    setNotice(null);
    const result = await rotatePersonalApiToken(client, tokenId);
    setBusyId(null);
    if (result.status === "created") {
      setRotated(result.token);
      onChanged();
      return;
    }
    setNotice(
      result.status === "forbidden"
        ? "You don't have permission to rotate API tokens. Ask a tenant admin."
        : result.message,
    );
  }

  async function handleRevoke(tokenId: string): Promise<void> {
    if (!client) return;
    if (!globalThis.confirm?.(`Revoke API token ${tokenId}?`)) return;
    setBusyId(tokenId);
    setNotice(null);
    const ok = await revokeApiToken(client, tokenId);
    setBusyId(null);
    if (ok) {
      setNotice(`Token ${tokenId} revoked.`);
      onChanged();
    } else {
      setNotice(`Failed to revoke token ${tokenId}.`);
    }
  }

  if (unavailable) {
    return (
      <Panel title="API tokens">
        <p className="unavailable-note">Tokens unavailable from this source.</p>
      </Panel>
    );
  }

  if (rotated) {
    return (
      <Panel title="API tokens">
        <p className="empty-note">Token rotated — the old token no longer works.</p>
        <TokenRevealPanel token={rotated} onDismiss={() => setRotated(null)} />
      </Panel>
    );
  }

  return (
    <Panel title="API tokens">
      {notice ? (
        <p className="empty-note" role="status">
          {notice}
        </p>
      ) : null}
      {tokens.length === 0 ? (
        <p className="empty-note">No tokens found.</p>
      ) : (
        <table className="data-table" aria-label="API tokens">
          <thead>
            <tr>
              <th>ID</th>
              <th>Label</th>
              <th>Class</th>
              <th>Issued</th>
              <th>Expires</th>
              <th>Status</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {tokens.map((t) => {
              const revoked = Boolean(t.revoked_at);
              const expired = isExpired(t.expires_at);
              return (
                <tr key={t.token_id}>
                  <td>{t.token_id}</td>
                  <td>{t.display_label ?? "—"}</td>
                  <td>{t.token_class ?? "—"}</td>
                  <td>{fmt(t.issued_at)}</td>
                  <td>{fmt(t.expires_at)}</td>
                  <td>{statusBadge(revoked, expired)}</td>
                  <td>
                    <button
                      type="button"
                      className="btn-ghost"
                      disabled={!client || busyId === t.token_id || revoked}
                      onClick={() => void handleRotate(t.token_id)}
                    >
                      {busyId === t.token_id ? "Working…" : "Rotate"}
                    </button>{" "}
                    <button
                      type="button"
                      className="btn-ghost"
                      disabled={!client || busyId === t.token_id || revoked}
                      onClick={() => void handleRevoke(t.token_id)}
                    >
                      {busyId === t.token_id ? "Working…" : "Revoke"}
                    </button>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      )}
      {client ? <CreateApiTokenControl client={client} onCreated={onChanged} /> : null}
    </Panel>
  );
}
