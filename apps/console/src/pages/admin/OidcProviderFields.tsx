// pages/admin/OidcProviderFields.tsx
// OIDC field group for ProviderConfigDrawer (#4967): issuer, client id,
// write-only secret, scopes, group claim, plus the read-only redirect URI the
// operator copies into their IdP. The secret input is never pre-filled and
// never echoes a prior value — it renders empty on every mount, matching the
// write-only contract (client_secret is required on every create/update).
// Fields use an explicit aria-label on the input (not a wrapping <label>),
// matching the rest of this admin surface's convention (see
// AdminAssignmentsPanel/AdminIdPGroupMappingsPanel). The two sections below
// reuse the global .section-label heading style (ServiceDrawer's "Deployment
// path"/"Dependencies" pattern) so "what you enter" and "what you register
// with your IdP" read as distinct groups without inventing a new style.
import { CopyField } from "./CopyField";
import type { OidcFormState } from "./providerConfigForm";

export function OidcProviderFields({
  form,
  redirectUri,
  disabled,
  onChange,
}: {
  readonly form: OidcFormState;
  readonly redirectUri: string;
  readonly disabled: boolean;
  readonly onChange: (next: OidcFormState) => void;
}): React.JSX.Element {
  return (
    <div className="provider-form-fields">
      <div className="section-label">Connect to your identity provider</div>
      <div className="provider-field">
        <span>Issuer</span>
        <input
          aria-label="Issuer"
          value={form.issuer}
          disabled={disabled}
          placeholder="https://idp.example.com"
          onChange={(e) => onChange({ ...form, issuer: e.target.value })}
        />
      </div>
      <div className="provider-field">
        <span>Client ID</span>
        <input
          aria-label="Client ID"
          value={form.clientId}
          disabled={disabled}
          onChange={(e) => onChange({ ...form, clientId: e.target.value })}
        />
      </div>
      <div className="provider-field">
        <span>Client secret</span>
        <input
          aria-label="Client secret"
          type="password"
          autoComplete="new-password"
          value={form.clientSecret}
          disabled={disabled}
          placeholder="Write-only — re-enter to save changes"
          onChange={(e) => onChange({ ...form, clientSecret: e.target.value })}
        />
        <small>Never displayed after save. Required again on every change.</small>
      </div>
      <div className="provider-field">
        <span>Scopes</span>
        <input
          aria-label="Scopes"
          value={form.scopesText}
          disabled={disabled}
          placeholder="openid, profile, email"
          onChange={(e) => onChange({ ...form, scopesText: e.target.value })}
        />
        <small>Comma-separated.</small>
      </div>
      <div className="provider-field">
        <span>Group claim</span>
        <input
          aria-label="Group claim"
          value={form.groupClaim}
          disabled={disabled}
          placeholder="groups"
          onChange={(e) => onChange({ ...form, groupClaim: e.target.value })}
        />
      </div>

      <div className="section-label provider-section-gap">Register with your identity provider</div>
      <CopyField label="Redirect URI" value={redirectUri} ariaLabel="OIDC redirect URI" />
      <p className="empty-note provider-field-hint">
        Register this exact URL as the allowed redirect URI in your IdP.
      </p>
    </div>
  );
}
