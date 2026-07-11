// pages/ProfilePage.tsx
// Caller profile — identity provider, MFA state, active context + memberships,
// browser session list, and API token list. Data comes from three Slice-B
// endpoints. No secrets are rendered: session_hash, token_hash, csrf tokens,
// and MFA credential handles are never surfaced. An error on any section
// renders "—" / unavailable rather than fabricated data.
import { useEffect, useState } from "react";

import type { EshuApiClient } from "../api/client";
import { loadProfile, loadSessions, loadTokens } from "../api/userProfile";
import type { ProfileData, BrowserSessionItem, APITokenItem } from "../api/userProfile";
import { beginTOTPEnrollment, confirmTOTPEnrollment } from "../api/totpEnrollment";
import type { TOTPBeginResult } from "../api/totpEnrollment";
import { Panel, Badge } from "../components/atoms";
import "./liveInventory.css";
import "./totpEnrollment.css";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function fmt(iso: string | undefined): string {
  if (!iso) return "—";
  try {
    return new Date(iso).toLocaleString();
  } catch {
    return "—";
  }
}

function providerLabel(id: string | undefined): string {
  return id && id.length > 0 ? id : "Local";
}

// isExpired reports whether a token's expiry has passed. An expired-but-not-
// revoked token must not be labeled "active" — that would imply it is still
// usable. Tokens with no expiry never expire.
function isExpired(iso: string | undefined): boolean {
  if (!iso) return false;
  const ms = new Date(iso).getTime();
  return Number.isFinite(ms) && ms < Date.now();
}

// ---------------------------------------------------------------------------
// Identity section
// ---------------------------------------------------------------------------

function IdentitySection({
  profile,
  unavailable,
  client,
  onEnrolled,
}: {
  readonly profile: ProfileData | null;
  readonly unavailable: boolean;
  readonly client?: EshuApiClient;
  readonly onEnrolled: () => void;
}): React.JSX.Element {
  if (unavailable) {
    return (
      <Panel title="Identity">
        <p className="unavailable-note">Profile unavailable from this source.</p>
      </Panel>
    );
  }
  const mfa = profile?.mfa;
  return (
    <Panel title="Identity">
      <dl className="kv-list">
        <dt>Identity provider</dt>
        <dd>{providerLabel(profile?.external_provider_config_id)}</dd>
        <dt>MFA active</dt>
        <dd>
          {mfa?.has_active_mfa ? (
            <Badge tone="teal">enabled</Badge>
          ) : (
            <Badge tone="neutral">none</Badge>
          )}
        </dd>
        {mfa?.has_active_mfa && mfa.factor_kind ? (
          <>
            <dt>Factor kind</dt>
            <dd>{mfa.factor_kind}</dd>
          </>
        ) : null}
      </dl>
      {client ? <TOTPEnrollmentControl client={client} onEnrolled={onEnrolled} /> : null}
    </Panel>
  );
}

// ---------------------------------------------------------------------------
// TOTP (authenticator app) enrollment (issue #4986)
// ---------------------------------------------------------------------------

type TOTPEnrollmentPhase = "idle" | "provisioning" | "confirming" | "activated";

// TOTPEnrollmentControl is the self-service authenticator-app enrollment
// flow: begin -> render the otpauth URI + manual key (never re-fetchable
// after this render) -> submit the first code -> confirm. No QR image is
// rendered client-side (no new frontend dependency was added for this) —
// the otpauth:// URI is shown as copyable text for a password manager /
// authenticator app that accepts a pasted URI, alongside the manual-entry
// secret. Recovery codes remain the primary enrollment path from the setup
// wizard (SetupMFAStep.tsx); this is the ADDITIONAL self-service factor a
// signed-in user may enable for themselves at any time.
function TOTPEnrollmentControl({
  client,
  onEnrolled,
}: {
  readonly client: EshuApiClient;
  readonly onEnrolled: () => void;
}): React.JSX.Element {
  const [phase, setPhase] = useState<TOTPEnrollmentPhase>("idle");
  const [begin, setBegin] = useState<TOTPBeginResult | null>(null);
  const [code, setCode] = useState("");
  const [message, setMessage] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function handleBegin(): Promise<void> {
    setBusy(true);
    setMessage(null);
    try {
      const result = await beginTOTPEnrollment(client);
      setBegin(result);
      setPhase("confirming");
    } catch (err) {
      setMessage(err instanceof Error ? err.message : "Failed to start authenticator app setup.");
    } finally {
      setBusy(false);
    }
  }

  async function handleConfirm(): Promise<void> {
    if (!begin) return;
    setBusy(true);
    setMessage(null);
    try {
      const result = await confirmTOTPEnrollment(client, begin.factor_id, code);
      if (result.status === "activated") {
        setPhase("activated");
        onEnrolled();
      } else if (result.status === "invalid_code") {
        setMessage("That code did not match. Check the time on your device and try again.");
      } else {
        setMessage(result.message);
      }
    } finally {
      setBusy(false);
    }
  }

  if (phase === "activated") {
    return (
      <div className="totp-enroll">
        <p className="totp-enroll-success">Authenticator app enabled.</p>
      </div>
    );
  }

  if (phase === "idle") {
    return (
      <div className="totp-enroll">
        {message ? <p className="totp-enroll-error">{message}</p> : null}
        <button
          type="button"
          className="totp-enroll-start"
          disabled={busy}
          onClick={() => void handleBegin()}
        >
          {busy ? "Starting…" : "Set up authenticator app"}
        </button>
      </div>
    );
  }

  return (
    <div className="totp-enroll">
      <p>Scan or paste this into your authenticator app, or enter the key manually.</p>
      <label htmlFor="totp-uri">Provisioning URI</label>
      <input
        id="totp-uri"
        type="text"
        readOnly
        value={begin?.otpauth_uri ?? ""}
        onFocus={(e) => e.currentTarget.select()}
      />
      <label htmlFor="totp-secret">Manual entry key</label>
      <input
        id="totp-secret"
        type="text"
        readOnly
        value={begin?.secret ?? ""}
        onFocus={(e) => e.currentTarget.select()}
      />
      <label htmlFor="totp-code">Enter the 6-digit code from your app</label>
      <input
        id="totp-code"
        type="text"
        inputMode="numeric"
        autoComplete="one-time-code"
        value={code}
        disabled={busy}
        onChange={(e) => setCode(e.target.value)}
      />
      {message ? <p className="totp-enroll-error">{message}</p> : null}
      <button
        type="button"
        disabled={busy || code.trim().length === 0}
        onClick={() => void handleConfirm()}
      >
        {busy ? "Verifying…" : "Activate"}
      </button>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Active context + memberships section
// ---------------------------------------------------------------------------

function ContextSection({
  profile,
  unavailable,
}: {
  readonly profile: ProfileData | null;
  readonly unavailable: boolean;
}): React.JSX.Element {
  if (unavailable) {
    return (
      <Panel title="Active context">
        <p className="unavailable-note">Profile unavailable from this source.</p>
      </Panel>
    );
  }
  const roleIds = profile?.role_ids ?? [];
  const memberships = profile?.memberships ?? [];
  return (
    <Panel title="Active context">
      <dl className="kv-list" aria-label="Active context details">
        <dt>Tenant</dt>
        <dd>{profile?.active_tenant_id ?? "—"}</dd>
        <dt>Workspace</dt>
        <dd>{profile?.active_workspace_id ?? "—"}</dd>
        <dt>Roles</dt>
        <dd>
          {roleIds.length > 0
            ? roleIds.map((r) => (
                <Badge key={r} tone="violet">
                  {r}
                </Badge>
              ))
            : "—"}
        </dd>
        <dt>Catalog enforced</dt>
        <dd>
          {profile?.permission_catalog_enforced ? (
            <Badge tone="teal">yes</Badge>
          ) : (
            <Badge tone="neutral">no</Badge>
          )}
        </dd>
      </dl>
      {memberships.length > 0 ? (
        <table className="data-table" aria-label="Memberships">
          <thead>
            <tr>
              <th>Tenant</th>
              <th>Workspace</th>
            </tr>
          </thead>
          <tbody>
            {memberships.map((m, i) => (
              <tr key={i}>
                <td>{m.tenant_id}</td>
                <td>{m.workspace_id}</td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : null}
    </Panel>
  );
}

// ---------------------------------------------------------------------------
// Effective permissions section (issue #4969): self-serve "why can't I see
// X" — each session's resolved roles and permission families, straight from
// GET /api/v0/auth/profile. This is the same signal capabilityAccess.ts (nav)
// and AdminPage's per-panel gating derive from server-side, so a user who is
// missing a nav item or Admin panel can check here what they'd need.
// ---------------------------------------------------------------------------

function PermissionsSection({
  profile,
  unavailable,
}: {
  readonly profile: ProfileData | null;
  readonly unavailable: boolean;
}): React.JSX.Element {
  if (unavailable) {
    return (
      <Panel title="Effective permissions">
        <p className="unavailable-note">Permissions unavailable from this source.</p>
      </Panel>
    );
  }
  const roleIds = profile?.role_ids ?? [];
  const families = profile?.allowed_permission_features ?? [];
  const enforced = profile?.permission_catalog_enforced ?? false;
  return (
    <Panel
      title="Effective permissions"
      sub="Your resolved roles and permission families — the same signal that gates navigation and the Admin area."
    >
      <dl className="kv-list" aria-label="Effective permissions details">
        <dt>Roles</dt>
        <dd>
          {roleIds.length > 0
            ? roleIds.map((r) => (
                <Badge key={r} tone="violet">
                  {r}
                </Badge>
              ))
            : "—"}
        </dd>
        <dt>Permission families</dt>
        <dd>
          {!enforced ? (
            <span className="unavailable-note">
              Catalog not enforced — every area is currently visible regardless of granted families.
            </span>
          ) : families.length > 0 ? (
            families.map((f) => (
              <Badge key={f} tone="teal">
                {f}
              </Badge>
            ))
          ) : (
            <span className="unavailable-note">No permission families granted.</span>
          )}
        </dd>
      </dl>
    </Panel>
  );
}

// ---------------------------------------------------------------------------
// Sessions section
// ---------------------------------------------------------------------------

function SessionsSection({
  sessions,
  unavailable,
}: {
  readonly sessions: readonly BrowserSessionItem[];
  readonly unavailable: boolean;
}): React.JSX.Element {
  if (unavailable) {
    return (
      <Panel title="Browser sessions">
        <p className="unavailable-note">Sessions unavailable from this source.</p>
      </Panel>
    );
  }
  if (sessions.length === 0) {
    return (
      <Panel title="Browser sessions">
        <p className="empty-note">No sessions found.</p>
      </Panel>
    );
  }
  return (
    <Panel title="Browser sessions">
      <table className="data-table" aria-label="Browser sessions">
        <thead>
          <tr>
            <th>Issued</th>
            <th>Last seen</th>
            <th>Idle expires</th>
            <th>Absolute expires</th>
            <th>Workspace</th>
            <th>Status</th>
          </tr>
        </thead>
        <tbody>
          {sessions.map((s, i) => (
            <tr key={i} aria-current={s.current ? "true" : undefined}>
              <td>{fmt(s.issued_at)}</td>
              <td>{fmt(s.last_seen_at)}</td>
              <td>{fmt(s.idle_expires_at)}</td>
              <td>{fmt(s.absolute_expires_at)}</td>
              <td>{s.workspace_id ?? "—"}</td>
              <td>
                {s.revoked_at ? (
                  <Badge tone="crit">revoked</Badge>
                ) : s.current ? (
                  <Badge tone="teal">current</Badge>
                ) : (
                  <Badge tone="neutral">active</Badge>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </Panel>
  );
}

// ---------------------------------------------------------------------------
// Tokens section
// ---------------------------------------------------------------------------

function TokensSection({
  tokens,
  unavailable,
}: {
  readonly tokens: readonly APITokenItem[];
  readonly unavailable: boolean;
}): React.JSX.Element {
  if (unavailable) {
    return (
      <Panel title="API tokens">
        <p className="unavailable-note">Tokens unavailable from this source.</p>
      </Panel>
    );
  }
  if (tokens.length === 0) {
    return (
      <Panel title="API tokens">
        <p className="empty-note">No tokens found.</p>
      </Panel>
    );
  }
  return (
    <Panel title="API tokens">
      <table className="data-table" aria-label="API tokens">
        <thead>
          <tr>
            <th>ID</th>
            <th>Class</th>
            <th>Issued</th>
            <th>Expires</th>
            <th>Status</th>
          </tr>
        </thead>
        <tbody>
          {tokens.map((t) => (
            <tr key={t.token_id}>
              <td>{t.token_id}</td>
              <td>{t.token_class ?? "—"}</td>
              <td>{fmt(t.issued_at)}</td>
              <td>{fmt(t.expires_at)}</td>
              <td>
                {t.revoked_at ? (
                  <Badge tone="crit">revoked</Badge>
                ) : isExpired(t.expires_at) ? (
                  <Badge tone="neutral">expired</Badge>
                ) : (
                  <Badge tone="teal">active</Badge>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </Panel>
  );
}

// ---------------------------------------------------------------------------
// ProfilePage
// ---------------------------------------------------------------------------

export function ProfilePage({ client }: { readonly client?: EshuApiClient }): React.JSX.Element {
  const [profile, setProfile] = useState<ProfileData | null>(null);
  const [profileUnavailable, setProfileUnavailable] = useState(false);
  const [sessions, setSessions] = useState<readonly BrowserSessionItem[]>([]);
  const [sessionsUnavailable, setSessionsUnavailable] = useState(false);
  const [tokens, setTokens] = useState<readonly APITokenItem[]>([]);
  const [tokensUnavailable, setTokensUnavailable] = useState(false);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    if (!client) {
      setLoading(false);
      setProfileUnavailable(true);
      setSessionsUnavailable(true);
      setTokensUnavailable(true);
      return;
    }

    void Promise.all([loadProfile(client), loadSessions(client), loadTokens(client)]).then(
      ([p, s, t]) => {
        if (cancelled) return;
        setProfile(p.data);
        setProfileUnavailable(p.provenance === "unavailable");
        setSessions(s.sessions);
        setSessionsUnavailable(s.provenance === "unavailable");
        setTokens(t.tokens);
        setTokensUnavailable(t.provenance === "unavailable");
        setLoading(false);
      },
    );

    return () => {
      cancelled = true;
    };
  }, [client]);

  // reloadProfile refreshes only the MFA-status-bearing profile panel after
  // a successful TOTP enrollment (issue #4986), so "MFA active" flips to
  // enabled without a full page reload.
  function reloadProfile(): void {
    if (!client) return;
    void loadProfile(client).then((p) => {
      setProfile(p.data);
      setProfileUnavailable(p.provenance === "unavailable");
    });
  }

  if (loading) {
    return (
      <section className="page-shell">
        <h1>Profile</h1>
        <p>Loading profile…</p>
      </section>
    );
  }

  return (
    <section className="page-shell">
      <h1>Profile</h1>
      <div className="panel-grid">
        <IdentitySection
          profile={profile}
          unavailable={profileUnavailable}
          client={client}
          onEnrolled={reloadProfile}
        />
        <ContextSection profile={profile} unavailable={profileUnavailable} />
        <PermissionsSection profile={profile} unavailable={profileUnavailable} />
        <SessionsSection sessions={sessions} unavailable={sessionsUnavailable} />
        <TokensSection tokens={tokens} unavailable={tokensUnavailable} />
      </div>
    </section>
  );
}
