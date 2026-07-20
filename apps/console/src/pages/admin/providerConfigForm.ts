// pages/admin/providerConfigForm.ts
// Pure, render-free form-state helpers for ProviderConfigDrawer (#4967). Kept
// separate from the component so the field-mapping and validity rules are
// unit-testable without rendering React, and so ProviderConfigDrawer.tsx stays
// under the repository's 500-line file limit.
import {
  oidcRedirectUri,
  githubCallbackUri,
  samlAcsUrl,
  samlServiceProviderEntityId,
} from "../../api/adminProviderConfig";
import type {
  AdminProviderConfigItem,
  OIDCProviderConfigInput,
  SAMLProviderConfigInput,
  GitHubProviderConfigInput,
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

// GithubFormState is the editable GitHub provider form (issue #5166, F-5).
// allowedOrgsText is a comma-separated list; the backend lowercases and
// dedupes it. baseUrl/apiBaseUrl are blank for github.com and set only for a
// GitHub Enterprise Server host.
export interface GithubFormState {
  readonly clientId: string;
  readonly clientSecret: string;
  readonly baseUrl: string;
  readonly apiBaseUrl: string;
  readonly scopesText: string;
  readonly allowedOrgsText: string;
}

export const emptyOidcForm: OidcFormState = {
  issuer: "",
  clientId: "",
  clientSecret: "",
  scopesText: "",
  groupClaim: "",
};

export const emptyGithubForm: GithubFormState = {
  clientId: "",
  clientSecret: "",
  baseUrl: "",
  apiBaseUrl: "",
  scopesText: "",
  allowedOrgsText: "",
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

// githubFormFromExisting seeds the editable fields from an existing GitHub
// provider. clientSecret is intentionally NEVER seeded — write-only,
// resupplied on every update, matching oidcFormFromExisting.
export function githubFormFromExisting(item: AdminProviderConfigItem): GithubFormState {
  const cfg = item.configuration;
  return {
    clientId: cfg.client_id ?? "",
    clientSecret: "",
    baseUrl: cfg.base_url ?? "",
    apiBaseUrl: cfg.api_base_url ?? "",
    scopesText: (cfg.scopes ?? []).join(", "),
    allowedOrgsText: (cfg.allowed_orgs ?? []).join(", "),
  };
}

function parseScopes(scopesText: string): readonly string[] {
  return commaList(scopesText);
}

// commaList splits a comma-separated text field into a trimmed, non-empty
// list. Shared by scopes and allowed-orgs parsing. The backend performs its
// own lowercase/dedupe normalization on allowed_orgs
// (buildGitHubProviderConfigWrite), so this does not lowercase here — it only
// removes blanks and surrounding whitespace.
function commaList(text: string): readonly string[] {
  return text
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

// buildGithubInput assembles the typed write-request input (issue #5166,
// F-5), including the fixed deployment-wide GitHub callback URL (see
// githubCallbackUri — the same value for every provider, never editable).
// baseUrl/apiBaseUrl are sent as-is (empty string for github.com — the
// backend defaults them).
export function buildGithubInput(
  form: GithubFormState,
  providerConfigId: string,
  baseUrl: string,
): GitHubProviderConfigInput {
  return {
    kind: "github",
    providerConfigId,
    clientId: form.clientId.trim(),
    clientSecret: form.clientSecret,
    baseUrl: form.baseUrl.trim(),
    apiBaseUrl: form.apiBaseUrl.trim(),
    scopes: parseScopes(form.scopesText),
    allowedOrgs: commaList(form.allowedOrgsText),
    redirectUrl: githubCallbackUri(baseUrl),
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

// githubFormValid mirrors buildGitHubProviderConfigWrite's validation: client
// id, client secret, and a NON-EMPTY allowed-orgs list are all required. The
// non-empty allowed_orgs check is the client-side mirror of the backend's
// mandatory org allow-list (a GitHub OAuth App can authenticate any GitHub
// account, so an empty allow-list would let anyone sign in) — it blocks
// Save/Test before submit, but the server remains the enforcement boundary.
export function githubFormValid(form: GithubFormState): boolean {
  return (
    form.clientId.trim().length > 0 &&
    form.clientSecret.trim().length > 0 &&
    commaList(form.allowedOrgsText).length > 0
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
