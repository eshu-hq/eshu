// pages/admin/SignInPolicyPanel.tsx
// Sign-in policy panel (#4968, epic #4962): fills the Identity & Access ->
// Sign-in policy tab that shipped as a placeholder in #4967
// (AdminIdentityAccessPanel.tsx). Reads and writes the tenant sign-in
// policy — require SSO, allow local user creation, require MFA for all
// users, and session idle/absolute lifetime — via the #4968 admin API
// (api/adminSignInPolicy.ts). The server is the sole authorization and
// guardrail boundary: this panel only reflects and requests changes, and
// surfaces a rejected require_sso enable (the guardrail message) as an
// inline error rather than a client-side check — the guardrail cannot be
// evaluated correctly from the console alone (it depends on server-side
// provider-connection-test and SSO-admin-proof state).
import { useEffect, useState, useCallback } from "react";

import { loadAdminSignInPolicy, updateAdminSignInPolicy } from "../../api/adminSignInPolicy";
import type { AdminSignInPolicy, SignInPolicyUpdateInput } from "../../api/adminSignInPolicy";
import type { EshuApiClient } from "../../api/client";
import { Panel } from "../../components/atoms";
import { fmt } from "./adminFormat";

// secondsToMinutesInput/minutesInputToSeconds convert the wire's
// idle/absolute *_timeout_seconds fields to a whole-minutes form field —
// operators think in minutes/hours, not raw seconds. 0 (the wire's "use the
// process default" sentinel — go/internal/storage/postgres/
// identity_sign_in_policy_types.go) renders as an empty field rather than
// "0 minutes", so the default state is visually distinct from "explicitly
// set to zero" (which the server would reject as meaningless anyway).
function secondsToMinutesInput(seconds: number | undefined): string {
  if (!seconds) return "";
  return String(Math.round(seconds / 60));
}

function minutesInputToSeconds(value: string): number | undefined {
  const trimmed = value.trim();
  if (trimmed === "") return undefined;
  const minutes = Number(trimmed);
  if (!Number.isFinite(minutes) || minutes <= 0) return undefined;
  return Math.round(minutes * 60);
}

export function SignInPolicyPanel({
  client,
}: {
  readonly client?: EshuApiClient;
}): React.JSX.Element {
  const [policy, setPolicy] = useState<AdminSignInPolicy | undefined>(undefined);
  const [unavailable, setUnavailable] = useState(false);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);
  const [noticeIsError, setNoticeIsError] = useState(false);
  const [idleMinutesInput, setIdleMinutesInput] = useState("");
  const [absoluteMinutesInput, setAbsoluteMinutesInput] = useState("");

  useEffect(() => {
    let cancelled = false;
    if (!client) {
      setUnavailable(true);
      setLoading(false);
      return;
    }
    setLoading(true);
    void loadAdminSignInPolicy(client).then((r) => {
      if (cancelled) return;
      setPolicy(r.policy);
      setUnavailable(r.provenance === "unavailable");
      setIdleMinutesInput(secondsToMinutesInput(r.policy?.idle_timeout_seconds));
      setAbsoluteMinutesInput(secondsToMinutesInput(r.policy?.absolute_timeout_seconds));
      setLoading(false);
    });
    return () => {
      cancelled = true;
    };
  }, [client]);

  const applyUpdate = useCallback(
    async (input: SignInPolicyUpdateInput) => {
      if (!client) return;
      setSaving(true);
      setNotice(null);
      setNoticeIsError(false);
      const outcome = await updateAdminSignInPolicy(client, input);
      setSaving(false);
      if (outcome.ok && outcome.policy) {
        setPolicy(outcome.policy);
        setIdleMinutesInput(secondsToMinutesInput(outcome.policy.idle_timeout_seconds));
        setAbsoluteMinutesInput(secondsToMinutesInput(outcome.policy.absolute_timeout_seconds));
        setNotice("Sign-in policy updated.");
      } else {
        setNoticeIsError(true);
        setNotice(outcome.errorMessage ?? "Failed to update sign-in policy.");
      }
    },
    [client],
  );

  if (loading) {
    return (
      <Panel title="Sign-in policy">
        <p className="empty-note">Loading sign-in policy…</p>
      </Panel>
    );
  }
  if (unavailable || !policy) {
    return (
      <Panel title="Sign-in policy">
        <p className="unavailable-note">Sign-in policy unavailable from this source.</p>
      </Panel>
    );
  }

  const ssoProven = Boolean(policy.sso_admin_verified_at);

  return (
    <Panel title="Sign-in policy">
      {notice ? (
        <p className={noticeIsError ? "unavailable-note" : "empty-note"} role="status">
          {notice}
        </p>
      ) : null}

      <div className="provider-form-fields">
        <label className="policy-toggle-row" htmlFor="policy-require-sso">
          <input
            id="policy-require-sso"
            type="checkbox"
            checked={policy.require_sso}
            disabled={saving || !client}
            onChange={(e) => void applyUpdate({ requireSso: e.target.checked })}
          />
          <span>
            Require SSO for sign-in
            <small>
              Hides the local password form on the login page. Rejected unless at least one provider
              has a passing connection test and an admin has signed in via SSO. Break-glass local
              admin sign-in stays reachable at <code>/login?local=1</code>.
            </small>
          </span>
        </label>
        {!ssoProven ? (
          <p className="empty-note provider-field-hint">
            No admin has signed in via SSO yet for this tenant — required before Require SSO can be
            enabled.
          </p>
        ) : null}

        <label className="policy-toggle-row" htmlFor="policy-allow-local-creation">
          <input
            id="policy-allow-local-creation"
            type="checkbox"
            checked={policy.allow_local_user_creation}
            disabled={saving || !client}
            onChange={(e) => void applyUpdate({ allowLocalUserCreation: e.target.checked })}
          />
          <span>
            Allow local user creation
            <small>Off means invitations can only link SSO identities.</small>
          </span>
        </label>

        <label className="policy-toggle-row" htmlFor="policy-require-mfa">
          <input
            id="policy-require-mfa"
            type="checkbox"
            checked={policy.require_mfa_for_all_users}
            disabled={saving || !client}
            onChange={(e) => void applyUpdate({ requireMfaForAllUsers: e.target.checked })}
          />
          <span>
            Require MFA for all users
            <small>
              Admins always require MFA. This extends the requirement to every local user.
            </small>
          </span>
        </label>

        <div className="provider-field">
          <label htmlFor="policy-idle-timeout">Idle session timeout (minutes)</label>
          <input
            id="policy-idle-timeout"
            type="number"
            min={1}
            placeholder="Default (30)"
            value={idleMinutesInput}
            disabled={saving || !client}
            onChange={(e) => setIdleMinutesInput(e.target.value)}
            onBlur={() =>
              void applyUpdate({ idleTimeoutSeconds: minutesInputToSeconds(idleMinutesInput) ?? 0 })
            }
          />
          <small>
            Blank uses the process default. Shown alongside the OIDC session-refresh window, which
            is separate.
          </small>
        </div>

        <div className="provider-field">
          <label htmlFor="policy-absolute-timeout">Absolute session timeout (minutes)</label>
          <input
            id="policy-absolute-timeout"
            type="number"
            min={1}
            placeholder="Default (720)"
            value={absoluteMinutesInput}
            disabled={saving || !client}
            onChange={(e) => setAbsoluteMinutesInput(e.target.value)}
            onBlur={() =>
              void applyUpdate({
                absoluteTimeoutSeconds: minutesInputToSeconds(absoluteMinutesInput) ?? 0,
              })
            }
          />
          <small>Blank uses the process default.</small>
        </div>

        <p className="grant-line">
          Last updated {fmt(policy.updated_at)}
          {policy.sso_admin_verified_provider_config_id
            ? ` · SSO admin proof via ${policy.sso_admin_verified_provider_config_id}`
            : ""}
        </p>
      </div>
    </Panel>
  );
}
