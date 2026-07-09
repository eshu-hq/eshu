// pages/admin/SamlProviderFields.tsx
// SAML field group for ProviderConfigDrawer (#4967): metadata URL or pasted
// XML, entity id, group attribute, write-only signing material, plus the
// read-only SP entity id + ACS URL the operator copies into their IdP. The
// private key / certificate inputs are never pre-filled and never echo a
// prior value — write-only, resupplied on every create/update. Fields use an
// explicit aria-label on the input (not a wrapping <label>), matching the
// rest of this admin surface's convention. The two sections below reuse the
// global .section-label heading style so "what you enter" and "what you
// register with your IdP" read as distinct groups.
import { CopyField } from "./CopyField";
import type { SamlFormState } from "./providerConfigForm";

export function SamlProviderFields({
  form,
  serviceProviderEntityId,
  acsUrl,
  disabled,
  onChange,
}: {
  readonly form: SamlFormState;
  readonly serviceProviderEntityId: string;
  readonly acsUrl: string;
  readonly disabled: boolean;
  readonly onChange: (next: SamlFormState) => void;
}): React.JSX.Element {
  return (
    <div className="provider-form-fields">
      <div className="section-label">Connect to your identity provider</div>
      <div className="provider-field">
        <span>IdP metadata URL</span>
        <input
          aria-label="IdP metadata URL"
          value={form.metadataUrl}
          disabled={disabled}
          placeholder="https://idp.example.com/metadata"
          onChange={(e) => onChange({ ...form, metadataUrl: e.target.value })}
        />
      </div>
      <div className="provider-field">
        <span>Or paste IdP metadata XML</span>
        <textarea
          aria-label="IdP metadata XML"
          value={form.metadataXml}
          disabled={disabled}
          rows={4}
          placeholder="<EntityDescriptor …>"
          onChange={(e) => onChange({ ...form, metadataXml: e.target.value })}
        />
        <small>Provide either a metadata URL or pasted XML.</small>
      </div>
      <div className="provider-field">
        <span>IdP entity ID</span>
        <input
          aria-label="IdP entity ID"
          value={form.entityId}
          disabled={disabled}
          onChange={(e) => onChange({ ...form, entityId: e.target.value })}
        />
      </div>
      <div className="provider-field">
        <span>Group attribute</span>
        <input
          aria-label="Group attribute"
          value={form.groupAttribute}
          disabled={disabled}
          placeholder="groups"
          onChange={(e) => onChange({ ...form, groupAttribute: e.target.value })}
        />
      </div>
      <div className="provider-field">
        <span>SP private key</span>
        <textarea
          aria-label="SP private key"
          value={form.spPrivateKey}
          disabled={disabled}
          rows={3}
          placeholder="Write-only PEM — re-enter to save changes"
          onChange={(e) => onChange({ ...form, spPrivateKey: e.target.value })}
        />
      </div>
      <div className="provider-field">
        <span>SP certificate</span>
        <textarea
          aria-label="SP certificate"
          value={form.spCertificate}
          disabled={disabled}
          rows={3}
          placeholder="Write-only PEM — re-enter to save changes"
          onChange={(e) => onChange({ ...form, spCertificate: e.target.value })}
        />
        <small>
          Signing material is never displayed after save. Required again on every change.
        </small>
      </div>

      <div className="section-label provider-section-gap">Register with your identity provider</div>
      <CopyField
        label="SP entity ID"
        value={serviceProviderEntityId}
        ariaLabel="SAML SP entity ID"
      />
      <CopyField label="ACS URL" value={acsUrl} ariaLabel="SAML ACS URL" />
      <p className="empty-note provider-field-hint">Register these exact values in your IdP.</p>
    </div>
  );
}
