// pages/admin/GithubProviderFields.tsx
// GitHub field group for ProviderConfigDrawer (issue #5166, F-5): client id,
// write-only client secret, the required org allow-list, optional GitHub
// Enterprise Server base/API URLs and scopes, plus the read-only OAuth2
// callback URL the operator registers in their GitHub OAuth App. The secret
// input is never pre-filled and never echoes a prior value — write-only,
// required on every create/update, matching OidcProviderFields. Fields use an
// explicit aria-label on the input (not a wrapping <label>), matching the
// rest of this admin surface's convention.
import { CopyField } from "./CopyField";
import type { GithubFormState } from "./providerConfigForm";

export function GithubProviderFields({
  form,
  callbackUri,
  disabled,
  onChange,
}: {
  readonly form: GithubFormState;
  readonly callbackUri: string;
  readonly disabled: boolean;
  readonly onChange: (next: GithubFormState) => void;
}): React.JSX.Element {
  return (
    <div className="provider-form-fields">
      <div className="section-label">Connect to GitHub</div>
      <div className="provider-field">
        <span>Client ID</span>
        <input
          aria-label="GitHub client ID"
          value={form.clientId}
          disabled={disabled}
          onChange={(e) => onChange({ ...form, clientId: e.target.value })}
        />
      </div>
      <div className="provider-field">
        <span>Client secret</span>
        <input
          aria-label="GitHub client secret"
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
        <span>Allowed organizations</span>
        <input
          aria-label="Allowed organizations"
          value={form.allowedOrgsText}
          disabled={disabled}
          placeholder="my-org, another-org"
          onChange={(e) => onChange({ ...form, allowedOrgsText: e.target.value })}
        />
        <small>
          Comma-separated. Required — a user must belong to one of these GitHub organizations to
          sign in.
        </small>
      </div>
      <div className="provider-field">
        <span>Scopes</span>
        <input
          aria-label="GitHub scopes"
          value={form.scopesText}
          disabled={disabled}
          placeholder="read:org, user:email"
          onChange={(e) => onChange({ ...form, scopesText: e.target.value })}
        />
        <small>Comma-separated. Defaults to read:org and user:email when left blank.</small>
      </div>
      <div className="provider-field">
        <span>Enterprise Server base URL</span>
        <input
          aria-label="GitHub base URL"
          value={form.baseUrl}
          disabled={disabled}
          placeholder="https://github.example.com (blank for github.com)"
          onChange={(e) => onChange({ ...form, baseUrl: e.target.value })}
        />
        <small>Only for GitHub Enterprise Server. Leave blank for github.com.</small>
      </div>
      <div className="provider-field">
        <span>Enterprise Server API URL</span>
        <input
          aria-label="GitHub API base URL"
          value={form.apiBaseUrl}
          disabled={disabled}
          placeholder="https://github.example.com/api/v3 (blank for github.com)"
          onChange={(e) => onChange({ ...form, apiBaseUrl: e.target.value })}
        />
        <small>Only for GitHub Enterprise Server. Leave blank for github.com.</small>
      </div>

      <div className="section-label provider-section-gap">Register with GitHub</div>
      <CopyField
        label="Authorization callback URL"
        value={callbackUri}
        ariaLabel="GitHub callback URL"
      />
      <p className="empty-note provider-field-hint">
        Register this exact URL as the Authorization callback URL in your GitHub OAuth App.
      </p>
    </div>
  );
}
