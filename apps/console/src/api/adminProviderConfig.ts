// api/adminProviderConfig.ts
// Loaders and mutators for the DB-backed identity provider-config CRUD surface
// (#4966, epic #4962) consumed by the Admin -> Identity & Access -> Providers
// tab (#4967). Field names mirror the backend OpenAPI fragments verbatim
// (go/internal/query/openapi_paths_auth_admin_provider_configs.go,
//  openapi_components_provider_configs.go). No loader, mutator, or type here
// ever models a plaintext secret: has_secret, secret_fingerprint, and key_id
// are the only secret-adjacent fields, matching the backend's own leakage
// boundary (go/internal/query/admin_provider_config_leakage_test.go). Every
// secret input field (client_secret, sp_private_key, sp_certificate) is
// write-only and is sent to the backend but never read back from any
// response modeled here.
import type { AdminProvenance } from "./adminConsoleTypes";
import type { EshuApiClient } from "./client";

const PROVIDER_CONFIGS_PATH = "/api/v0/auth/admin/provider-configs";

// ---------------------------------------------------------------------------
// Read types — GET /api/v0/auth/admin/provider-configs[/{id}][/revisions]
// ---------------------------------------------------------------------------

// AdminProviderConfigKind is the vocabulary the GET responses use for
// provider_kind. Distinct from ProviderConfigFormKind (the create/update
// request vocabulary) — see toFormKind below for why the two differ.
export type AdminProviderConfigKind = "external_oidc" | "external_saml";

// AdminProviderConfigConfiguration is the non-secret settings blob. Only the
// fields relevant to the provider's kind are populated by the backend; the
// rest are absent, never null-filled.
export interface AdminProviderConfigConfiguration {
  readonly issuer?: string;
  readonly client_id?: string;
  readonly scopes?: readonly string[];
  readonly group_claim?: string;
  readonly redirect_url?: string;
  readonly metadata_url?: string;
  readonly metadata_xml?: string;
  readonly entity_id?: string;
  readonly group_attribute?: string;
  readonly service_provider_entity_id?: string;
  readonly service_provider_acs_url?: string;
}

export interface AdminProviderConfigItem {
  readonly provider_config_id: string;
  readonly provider_kind: AdminProviderConfigKind;
  readonly status: string; // "draft" | "active"
  readonly active_revision_id?: string;
  readonly configuration: AdminProviderConfigConfiguration;
  readonly has_secret: boolean;
  readonly secret_fingerprint?: string;
  readonly key_id?: string;
  readonly shadowed_by_environment: boolean;
  readonly managed_by: string; // "database" | "environment"
  readonly created_at?: string;
  readonly updated_at?: string;
}

export interface AdminProviderConfigsResult {
  readonly items: readonly AdminProviderConfigItem[];
  readonly truncated: boolean;
  readonly provenance: AdminProvenance;
}

interface AdminProviderConfigsWire {
  readonly provider_configs?: readonly AdminProviderConfigItem[];
  readonly truncated?: boolean;
}

// loadProviderConfigs lists the tenant's DB-backed and env-registered
// provider configs. On a load error it returns an EMPTY list with
// provenance "unavailable" — never fabricated rows, matching the existing
// admin-panel convention (see AdminIdPGroupMappingsPanel doc comment).
export async function loadProviderConfigs(
  client: EshuApiClient,
): Promise<AdminProviderConfigsResult> {
  try {
    const resp = await client.getJson<AdminProviderConfigsWire>(PROVIDER_CONFIGS_PATH);
    return {
      items: resp.provider_configs ?? [],
      truncated: resp.truncated ?? false,
      provenance: "live",
    };
  } catch (err) {
    console.error("[adminProviderConfig] loadProviderConfigs failed", err);
    return { items: [], truncated: false, provenance: "unavailable" };
  }
}

export interface AdminProviderConfigRevisionItem {
  readonly revision_id: string;
  readonly status: string;
  readonly has_secret: boolean;
  readonly created_at?: string;
  readonly activated_at?: string;
  readonly superseded_at?: string;
}

interface AdminProviderConfigRevisionsWire {
  readonly revisions?: readonly AdminProviderConfigRevisionItem[];
}

export interface AdminProviderConfigRevisionsResult {
  readonly revisions: readonly AdminProviderConfigRevisionItem[];
  readonly provenance: AdminProvenance;
}

export async function loadProviderConfigRevisions(
  client: EshuApiClient,
  providerConfigId: string,
): Promise<AdminProviderConfigRevisionsResult> {
  try {
    const resp = await client.getJson<AdminProviderConfigRevisionsWire>(
      `${PROVIDER_CONFIGS_PATH}/${encodeURIComponent(providerConfigId)}/revisions`,
    );
    return { revisions: resp.revisions ?? [], provenance: "live" };
  } catch (err) {
    console.error("[adminProviderConfig] loadProviderConfigRevisions failed", err);
    return { revisions: [], provenance: "unavailable" };
  }
}

// ---------------------------------------------------------------------------
// Write types — POST create/update/revert/enable/disable/test-connection
// ---------------------------------------------------------------------------

// ProviderConfigFormKind is the create/update request body's provider_kind
// vocabulary ("oidc" | "saml" — go/internal/query/admin_provider_config_types.go
// adminProviderConfigWriteRequest). The backend deliberately uses a DIFFERENT
// vocabulary on read (AdminProviderConfigKind, "external_oidc" | "external_saml"
// — see admin_provider_config_build.go buildProviderConfigWrite's builtKind).
// toFormKind bridges the two so the same value can drive an edit form.
export type ProviderConfigFormKind = "oidc" | "saml";

export function toFormKind(kind: string | undefined): ProviderConfigFormKind {
  return kind === "external_saml" ? "saml" : "oidc";
}

export interface OIDCProviderConfigInput {
  readonly kind: "oidc";
  readonly providerConfigId?: string;
  readonly issuer: string;
  readonly clientId: string;
  readonly clientSecret: string;
  readonly scopes: readonly string[];
  readonly groupClaim: string;
  readonly redirectUrl: string;
}

export interface SAMLProviderConfigInput {
  readonly kind: "saml";
  readonly providerConfigId?: string;
  readonly metadataUrl: string;
  readonly metadataXml: string;
  readonly entityId: string;
  readonly groupAttribute: string;
  readonly serviceProviderEntityId: string;
  readonly serviceProviderAcsUrl: string;
  readonly spPrivateKey: string;
  readonly spCertificate: string;
}

export type ProviderConfigInput = OIDCProviderConfigInput | SAMLProviderConfigInput;

// OIDCProviderConfigWireBody / SAMLProviderConfigWireBody are the exact JSON
// shapes adminProviderConfigWriteRequest expects (go/internal/query/
// admin_provider_config_types.go). provider_config_id is optional on both —
// omitted on update (the URL id wins) and on an edit-existing-row create.
export interface OIDCProviderConfigWireBody {
  readonly provider_kind: "oidc";
  readonly provider_config_id?: string;
  readonly issuer: string;
  readonly client_id: string;
  readonly client_secret: string;
  readonly scopes: readonly string[];
  readonly group_claim: string;
  readonly redirect_url: string;
}

export interface SAMLProviderConfigWireBody {
  readonly provider_kind: "saml";
  readonly provider_config_id?: string;
  readonly metadata_url: string;
  readonly metadata_xml: string;
  readonly entity_id: string;
  readonly group_attribute: string;
  readonly service_provider_entity_id: string;
  readonly service_provider_acs_url: string;
  readonly sp_private_key: string;
  readonly sp_certificate: string;
}

export type ProviderConfigWireBody = OIDCProviderConfigWireBody | SAMLProviderConfigWireBody;

// toWireBody maps a typed form input to the exact JSON field names
// adminProviderConfigWriteRequest expects. Exported for direct unit testing
// of the field-name mapping without a network mock. The return type is a
// discriminated union (not Record<string, unknown>) so a field-name typo or
// a missing required field fails at compile time.
export function toWireBody(input: ProviderConfigInput): ProviderConfigWireBody {
  const idField = input.providerConfigId ? { provider_config_id: input.providerConfigId } : {};
  if (input.kind === "oidc") {
    return {
      provider_kind: "oidc",
      ...idField,
      issuer: input.issuer,
      client_id: input.clientId,
      client_secret: input.clientSecret,
      scopes: input.scopes,
      group_claim: input.groupClaim,
      redirect_url: input.redirectUrl,
    };
  }
  return {
    provider_kind: "saml",
    ...idField,
    metadata_url: input.metadataUrl,
    metadata_xml: input.metadataXml,
    entity_id: input.entityId,
    group_attribute: input.groupAttribute,
    service_provider_entity_id: input.serviceProviderEntityId,
    service_provider_acs_url: input.serviceProviderAcsUrl,
    sp_private_key: input.spPrivateKey,
    sp_certificate: input.spCertificate,
  };
}

export interface ProviderConfigWriteResult {
  readonly provider_config_id: string;
  readonly revision_id: string;
  readonly status: string;
  readonly changed: boolean;
}

// ProviderConfigWriteOutcome never throws to the caller — a failed write
// (validation, conflict, keyring unavailable, managed-by-environment) is
// reported as ok:false with a display-safe message, mirroring the rest of
// this admin surface's "mutators return boolean/outcome, never throw"
// convention (see adminConsole.ts).
export interface ProviderConfigWriteOutcome {
  readonly ok: boolean;
  readonly result?: ProviderConfigWriteResult;
  readonly errorMessage?: string;
}

async function runWrite(
  label: string,
  run: () => Promise<ProviderConfigWriteResult>,
): Promise<ProviderConfigWriteOutcome> {
  try {
    const result = await run();
    return { ok: true, result };
  } catch (err) {
    console.error(`[adminProviderConfig] ${label} failed`, err);
    return {
      ok: false,
      errorMessage: err instanceof Error ? err.message : `${label} failed`,
    };
  }
}

// createProviderConfig — POST /api/v0/auth/admin/provider-configs. The
// resulting provider config is always a draft; it cannot be enabled until a
// test-connection call passes (see enableProviderConfig).
export async function createProviderConfig(
  client: EshuApiClient,
  input: ProviderConfigInput,
): Promise<ProviderConfigWriteOutcome> {
  return runWrite("createProviderConfig", () =>
    client.postJson<ProviderConfigWriteResult>(PROVIDER_CONFIGS_PATH, toWireBody(input)),
  );
}

// updateProviderConfig — POST /api/v0/auth/admin/provider-configs/{id}. Every
// call creates a new revision superseding the current one; the full secret
// must be resupplied (write-only secrets are never carried forward).
export async function updateProviderConfig(
  client: EshuApiClient,
  providerConfigId: string,
  input: ProviderConfigInput,
): Promise<ProviderConfigWriteOutcome> {
  return runWrite("updateProviderConfig", () =>
    client.postJson<ProviderConfigWriteResult>(
      `${PROVIDER_CONFIGS_PATH}/${encodeURIComponent(providerConfigId)}`,
      toWireBody(input),
    ),
  );
}

// enableProviderConfig — POST .../{id}/enable. The server re-runs
// test-connection synchronously for the current active revision and only
// activates the provider if it passes.
export async function enableProviderConfig(
  client: EshuApiClient,
  providerConfigId: string,
): Promise<ProviderConfigWriteOutcome> {
  return runWrite("enableProviderConfig", () =>
    client.postJson<ProviderConfigWriteResult>(
      `${PROVIDER_CONFIGS_PATH}/${encodeURIComponent(providerConfigId)}/enable`,
      {},
    ),
  );
}

// disableProviderConfig — POST .../{id}/disable. Idempotent.
export async function disableProviderConfig(
  client: EshuApiClient,
  providerConfigId: string,
): Promise<ProviderConfigWriteOutcome> {
  return runWrite("disableProviderConfig", () =>
    client.postJson<ProviderConfigWriteResult>(
      `${PROVIDER_CONFIGS_PATH}/${encodeURIComponent(providerConfigId)}/disable`,
      {},
    ),
  );
}

// ProviderConfigTestResult reports a test-connection outcome. `ran` is false
// only when the call itself could not be made (network/HTTP failure) — the
// drawer uses this to distinguish "test failed" from "could not run the
// test" in its notice text. `detail` never carries a secret (server-enforced;
// see AdminProviderConfigConnectionTestResult's doc comment).
export interface ProviderConfigTestResult {
  readonly ok: boolean;
  readonly detail?: string;
  readonly ran: boolean;
}

interface TestConnectionWire {
  readonly provider_config_id: string;
  readonly ok: boolean;
  readonly detail?: string;
}

// testProviderConfigConnection — POST .../{id}/test-connection. Requires the
// provider config to already exist server-side (it opens the stored sealed
// secret), so callers must create/save the draft first.
export async function testProviderConfigConnection(
  client: EshuApiClient,
  providerConfigId: string,
): Promise<ProviderConfigTestResult> {
  try {
    const resp = await client.postJson<TestConnectionWire>(
      `${PROVIDER_CONFIGS_PATH}/${encodeURIComponent(providerConfigId)}/test-connection`,
      {},
    );
    return { ok: resp.ok, detail: resp.detail, ran: true };
  } catch (err) {
    console.error("[adminProviderConfig] testProviderConfigConnection failed", err);
    return {
      ok: false,
      detail: err instanceof Error ? err.message : "connection test failed to run",
      ran: false,
    };
  }
}

// ---------------------------------------------------------------------------
// Client-side id + IdP-facing endpoint URI helpers
// ---------------------------------------------------------------------------

// newClientProviderConfigId generates an opaque id in the Add drawer, BEFORE
// the first Save, so the SAML ACS URL / SP entity id (both path-scoped by
// provider_config_id — go/internal/samlauth/db_provider_config_test.go) can be
// rendered read-only for the operator to copy into their IdP immediately, not
// only after a round trip. The backend's create route accepts a caller-
// supplied provider_config_id verbatim (adminProviderConfigWriteRequest's
// ProviderConfigID doc comment), so this id is sent as-is on create.
//
// Entropy comes from crypto.getRandomValues() only — never Math.random(),
// which is not cryptographically secure and is flagged by CodeQL in any
// security-relevant path (this id becomes part of a URL registered with an
// external IdP, so collisions/predictability matter). getRandomValues is
// used directly (not crypto.randomUUID(), which wraps the same primitive)
// so there is a single, unconditional entropy source with no fallback
// branch that could silently degrade to a weaker one.
export function newClientProviderConfigId(): string {
  const bytes = new Uint8Array(16);
  globalThis.crypto.getRandomValues(bytes);
  const hex = Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
  return `pc_${hex}`;
}

function normalizeBase(baseUrl: string): string {
  return baseUrl.endsWith("/") ? baseUrl.slice(0, -1) : baseUrl;
}

// oidcRedirectUri is the single, deployment-wide OIDC callback every DB-backed
// OIDC provider config must register with its IdP — confirmed as a fixed
// route (not per-provider) in go/internal/oidclogin/service.go /
// db_provider_config_test.go, which set the same callback URL across
// providers. This is the exact value submitted as `redirect_url`.
export function oidcRedirectUri(baseUrl: string): string {
  return `${normalizeBase(baseUrl)}/api/v0/auth/oidc/callback`;
}

// samlAcsUrl is Eshu's per-provider SAML Assertion Consumer Service URL —
// confirmed path shape in go/internal/samlauth/db_provider_config_test.go
// ("/api/v0/auth/saml/providers/{id}/acs"). This is the exact value submitted
// as `service_provider_acs_url`.
export function samlAcsUrl(baseUrl: string, providerConfigId: string): string {
  return `${normalizeBase(baseUrl)}/api/v0/auth/saml/providers/${encodeURIComponent(providerConfigId)}/acs`;
}

// samlServiceProviderEntityId derives a stable, deterministic SP entity id
// from the same per-provider path as samlAcsUrl (minus the /acs suffix). The
// backend does not compute or validate this value server-side — it is a
// free-text admin-supplied field (go/internal/samlauth/db_provider_config.go
// only requires it be non-empty) — so this is a UI convention chosen to keep
// the field read-only and copy-pasteable per #4967's acceptance criteria,
// not a backend-enforced identifier. An operator who needs a different SP
// entity id can still register a DB row directly against the API.
export function samlServiceProviderEntityId(baseUrl: string, providerConfigId: string): string {
  return `${normalizeBase(baseUrl)}/api/v0/auth/saml/providers/${encodeURIComponent(providerConfigId)}`;
}

// deriveProviderLabel renders a human-readable label from the provider's own
// non-secret configuration — the backend has no dedicated `label` field
// (go/internal/query/admin_provider_config_types.go AdminProviderConfigDetail
// has none), so this reuses the issuer (OIDC) or entity id (SAML) the admin
// already entered, falling back to the opaque id. This is real, previously
// entered data reused for display — never a fabricated value.
export function deriveProviderLabel(item: AdminProviderConfigItem): string {
  const cfg = item.configuration ?? {};
  const raw = item.provider_kind === "external_saml" ? cfg.entity_id : cfg.issuer;
  const trimmed = raw?.trim() ?? "";
  return trimmed.length > 0 ? trimmed : item.provider_config_id;
}
