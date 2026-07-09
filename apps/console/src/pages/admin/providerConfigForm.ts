// pages/admin/providerConfigForm.ts
// Pure, render-free form-state helpers for ProviderConfigDrawer (#4967). Kept
// separate from the component so the field-mapping and validity rules are
// unit-testable without rendering React, and so ProviderConfigDrawer.tsx stays
// under the repository's 500-line file limit.
import {
  oidcRedirectUri,
  samlAcsUrl,
  samlServiceProviderEntityId,
} from "../../api/adminProviderConfig";
import type {
  AdminProviderConfigItem,
  OIDCProviderConfigInput,
  SAMLProviderConfigInput,
} from "../../api/adminProviderConfig";

export interface OidcFormState {
  readonly issuer: string;
  readonly clientId: string;
  readonly clientSecret: string;
  readonly scopesText: string;
  readonly groupClaim: string;
}

export interface SamlFormState {
  readonly metadataUrl: string;
  readonly metadataXml: string;
  readonly entityId: string;
  readonly groupAttribute: string;
  readonly spPrivateKey: string;
  readonly spCertificate: string;
}

export const emptyOidcForm: OidcFormState = {
  issuer: "",
  clientId: "",
  clientSecret: "",
  scopesText: "",
  groupClaim: "",
};

export const emptySamlForm: SamlFormState = {
  metadataUrl: "",
  metadataXml: "",
  entityId: "",
  groupAttribute: "",
  spPrivateKey: "",
  spCertificate: "",
};

// oidcFormFromExisting seeds the editable fields from an existing provider's
// non-secret configuration. clientSecret is intentionally NEVER seeded — it
// is write-only and the backend requires it be resupplied on every update.
export function oidcFormFromExisting(item: AdminProviderConfigItem): OidcFormState {
  const cfg = item.configuration;
  return {
    issuer: cfg.issuer ?? "",
    clientId: cfg.client_id ?? "",
    clientSecret: "",
    scopesText: (cfg.scopes ?? []).join(", "),
    groupClaim: cfg.group_claim ?? "",
  };
}

// samlFormFromExisting seeds the editable fields from an existing provider.
// spPrivateKey/spCertificate are intentionally never seeded (write-only,
// resupplied every update).
export function samlFormFromExisting(item: AdminProviderConfigItem): SamlFormState {
  const cfg = item.configuration;
  return {
    metadataUrl: cfg.metadata_url ?? "",
    metadataXml: cfg.metadata_xml ?? "",
    entityId: cfg.entity_id ?? "",
    groupAttribute: cfg.group_attribute ?? "",
    spPrivateKey: "",
    spCertificate: "",
  };
}

function parseScopes(scopesText: string): readonly string[] {
  return scopesText
    .split(",")
    .map((s) => s.trim())
    .filter((s) => s.length > 0);
}

// buildOidcInput assembles the typed write-request input, including the
// deployment-wide, read-only OIDC callback URI (see oidcRedirectUri's doc
// comment — it is the same value for every provider, never user-editable).
export function buildOidcInput(
  form: OidcFormState,
  providerConfigId: string,
  baseUrl: string,
): OIDCProviderConfigInput {
  return {
    kind: "oidc",
    providerConfigId,
    issuer: form.issuer.trim(),
    clientId: form.clientId.trim(),
    clientSecret: form.clientSecret,
    scopes: parseScopes(form.scopesText),
    groupClaim: form.groupClaim.trim(),
    redirectUrl: oidcRedirectUri(baseUrl),
  };
}

// buildSamlInput assembles the typed write-request input, including the
// read-only, per-provider SP entity id and ACS URL (see samlAcsUrl /
// samlServiceProviderEntityId doc comments).
export function buildSamlInput(
  form: SamlFormState,
  providerConfigId: string,
  baseUrl: string,
): SAMLProviderConfigInput {
  return {
    kind: "saml",
    providerConfigId,
    metadataUrl: form.metadataUrl.trim(),
    metadataXml: form.metadataXml.trim(),
    entityId: form.entityId.trim(),
    groupAttribute: form.groupAttribute.trim(),
    serviceProviderEntityId: samlServiceProviderEntityId(baseUrl, providerConfigId),
    serviceProviderAcsUrl: samlAcsUrl(baseUrl, providerConfigId),
    spPrivateKey: form.spPrivateKey,
    spCertificate: form.spCertificate,
  };
}

// oidcFormValid mirrors the backend's buildOIDCProviderConfigWrite validation
// (go/internal/query/admin_provider_config_build.go) closely enough to
// disable Save/Test until the request would not be rejected with 400 — the
// server remains the actual security/validation boundary.
export function oidcFormValid(form: OidcFormState): boolean {
  return (
    form.issuer.trim().length > 0 &&
    form.clientId.trim().length > 0 &&
    form.clientSecret.trim().length > 0
  );
}

// samlFormValid mirrors buildSAMLProviderConfigWrite's validation.
export function samlFormValid(form: SamlFormState): boolean {
  const hasMetadata = form.metadataUrl.trim().length > 0 || form.metadataXml.trim().length > 0;
  return (
    form.entityId.trim().length > 0 &&
    hasMetadata &&
    form.spPrivateKey.trim().length > 0 &&
    form.spCertificate.trim().length > 0
  );
}
