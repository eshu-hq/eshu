// api/adminConsole.ts
// Loaders and mutators for the tenant-scoped admin identity surface added in
// issue #3703 (console admin UX, #3462 criterion #4). Every loader mirrors the
// userProfile.ts pattern: typed, metadata-only view models, explicit
// "unavailable" provenance on error, and NO fabricated rows — an error always
// produces an EMPTY result set, never invented data.
//
// Field names match the backend OpenAPI fragments verbatim
// (go/internal/query/openapi_paths_auth_admin_reads.go,
//  openapi_paths_auth_admin_mutations.go, openapi_paths_auth.go,
//  openapi_paths_auth_tokens.go). No secret, hash, invite code, raw external
// group name, or credential handle is ever modeled or rendered: only ids,
// opaque references (mapping_ref), statuses, classes, and timestamps.
//
// The two audit loaders are special: the backend audit routes are GLOBAL
// shared-operator only, so a tenant admin receives HTTP 403. That is a scope
// signal, NOT a failure, so the audit loaders surface provenance "forbidden"
// distinctly from "unavailable" (a real error). See #3717.
import type { EshuApiClient } from "./client";
import type { AdminProvenance } from "./adminConsoleTypes";

// Re-export the shared provenance types and the audit loaders so consumers can
// import the entire admin-console surface from this one module. The audit
// loaders live in adminConsoleAudit.ts to keep each file under the 500-line
// limit; their 403→"forbidden" handling is documented there.
export type { AdminProvenance, AdminAuditProvenance } from "./adminConsoleTypes";
export {
  loadAuditEvents,
  loadAuditSummary
} from "./adminConsoleAudit";
export type {
  AuditEventItem,
  AuditEventsResult,
  AuditCount,
  AuditSummaryData,
  AuditSummaryResult
} from "./adminConsoleAudit";

// ---------------------------------------------------------------------------
// Invitations — GET /api/v0/auth/local/invitations
// ---------------------------------------------------------------------------

export interface InvitationItem {
  readonly invite_id: string;
  readonly role_id?: string;
  readonly status?: string;
  readonly expires_at?: string;
  readonly accepted_at?: string;
  readonly revoked_at?: string;
  readonly created_at?: string;
  readonly updated_at?: string;
  readonly tenant_id?: string;
  readonly workspace_id?: string;
}

export interface InvitationsResult {
  readonly invitations: readonly InvitationItem[];
  readonly truncated: boolean;
  readonly provenance: AdminProvenance;
}

interface InvitationsWire {
  readonly invitations?: readonly InvitationItem[];
  readonly truncated?: boolean;
}

export async function loadInvitations(client: EshuApiClient): Promise<InvitationsResult> {
  try {
    const resp = await client.getJson<InvitationsWire>("/api/v0/auth/local/invitations");
    return {
      invitations: resp.invitations ?? [],
      truncated: resp.truncated ?? false,
      provenance: "live"
    };
  } catch (err) {
    console.error("[adminConsole] loadInvitations failed", err);
    return { invitations: [], truncated: false, provenance: "unavailable" };
  }
}

// ---------------------------------------------------------------------------
// Role assignments — GET /api/v0/auth/admin/role-assignments (?user_id)
// ---------------------------------------------------------------------------

export interface RoleAssignmentItem {
  readonly user_id: string;
  readonly role_id: string;
  readonly assignment_source?: string;
  readonly status?: string;
  readonly effective_at?: string;
  readonly expires_at?: string;
  readonly tenant_id?: string;
  readonly workspace_id?: string;
}

export interface RoleAssignmentsResult {
  readonly assignments: readonly RoleAssignmentItem[];
  readonly truncated: boolean;
  readonly provenance: AdminProvenance;
}

interface RoleAssignmentsWire {
  readonly role_assignments?: readonly RoleAssignmentItem[];
  readonly truncated?: boolean;
}

// loadRoleAssignments lists membership-role assignments, optionally filtered by
// user_id. The user filter is URL-encoded so an id with reserved characters
// cannot break out of the query string.
export async function loadRoleAssignments(
  client: EshuApiClient,
  userId?: string
): Promise<RoleAssignmentsResult> {
  const path =
    userId && userId.length > 0
      ? `/api/v0/auth/admin/role-assignments?user_id=${encodeURIComponent(userId)}`
      : "/api/v0/auth/admin/role-assignments";
  try {
    const resp = await client.getJson<RoleAssignmentsWire>(path);
    return {
      assignments: resp.role_assignments ?? [],
      truncated: resp.truncated ?? false,
      provenance: "live"
    };
  } catch (err) {
    console.error("[adminConsole] loadRoleAssignments failed", err);
    return { assignments: [], truncated: false, provenance: "unavailable" };
  }
}

// ---------------------------------------------------------------------------
// Roles & grants — GET /api/v0/auth/admin/roles (read-only)
// ---------------------------------------------------------------------------

export interface RoleGrantItem {
  readonly grant_id?: string;
  readonly action?: string;
  readonly feature?: string;
  readonly data_class?: string;
  readonly scope_class?: string;
  readonly status?: string;
}

export interface RoleItem {
  readonly role_id: string;
  readonly status?: string;
  readonly built_in?: boolean;
  readonly grants?: readonly RoleGrantItem[];
}

export interface RolesResult {
  readonly roles: readonly RoleItem[];
  readonly truncated: boolean;
  readonly provenance: AdminProvenance;
}

interface RolesWire {
  readonly roles?: readonly RoleItem[];
  readonly truncated?: boolean;
}

export async function loadRoles(client: EshuApiClient): Promise<RolesResult> {
  try {
    const resp = await client.getJson<RolesWire>("/api/v0/auth/admin/roles");
    return { roles: resp.roles ?? [], truncated: resp.truncated ?? false, provenance: "live" };
  } catch (err) {
    console.error("[adminConsole] loadRoles failed", err);
    return { roles: [], truncated: false, provenance: "unavailable" };
  }
}

// ---------------------------------------------------------------------------
// IdP providers — GET /api/v0/auth/admin/idp-providers (read-only)
// ---------------------------------------------------------------------------

export interface IdPProviderItem {
  readonly provider_config_id: string;
  readonly provider_kind?: string;
  readonly status?: string;
}

export interface IdPProvidersResult {
  readonly providers: readonly IdPProviderItem[];
  readonly truncated: boolean;
  readonly provenance: AdminProvenance;
}

interface IdPProvidersWire {
  readonly providers?: readonly IdPProviderItem[];
  readonly truncated?: boolean;
}

export async function loadIdPProviders(client: EshuApiClient): Promise<IdPProvidersResult> {
  try {
    const resp = await client.getJson<IdPProvidersWire>("/api/v0/auth/admin/idp-providers");
    return {
      providers: resp.providers ?? [],
      truncated: resp.truncated ?? false,
      provenance: "live"
    };
  } catch (err) {
    console.error("[adminConsole] loadIdPProviders failed", err);
    return { providers: [], truncated: false, provenance: "unavailable" };
  }
}

// ---------------------------------------------------------------------------
// IdP group mappings — GET /api/v0/auth/admin/idp-group-mappings
// ---------------------------------------------------------------------------

// IdPGroupMappingItem never carries the external group name: only the opaque
// mapping_ref (an md5 over the composite key) is modeled.
export interface IdPGroupMappingItem {
  readonly mapping_ref: string;
  readonly provider_config_id?: string;
  readonly role_id?: string;
  readonly status?: string;
  readonly effective_at?: string;
  readonly expires_at?: string;
  readonly tenant_id?: string;
  readonly workspace_id?: string;
}

export interface IdPGroupMappingsResult {
  readonly mappings: readonly IdPGroupMappingItem[];
  readonly truncated: boolean;
  readonly provenance: AdminProvenance;
}

interface IdPGroupMappingsWire {
  readonly group_mappings?: readonly IdPGroupMappingItem[];
  readonly truncated?: boolean;
}

export async function loadIdPGroupMappings(
  client: EshuApiClient
): Promise<IdPGroupMappingsResult> {
  try {
    const resp = await client.getJson<IdPGroupMappingsWire>(
      "/api/v0/auth/admin/idp-group-mappings"
    );
    return {
      mappings: resp.group_mappings ?? [],
      truncated: resp.truncated ?? false,
      provenance: "live"
    };
  } catch (err) {
    console.error("[adminConsole] loadIdPGroupMappings failed", err);
    return { mappings: [], truncated: false, provenance: "unavailable" };
  }
}

// ---------------------------------------------------------------------------
// API tokens — GET /api/v0/auth/admin/api-tokens
// ---------------------------------------------------------------------------

// AdminAPITokenItem never carries the token hash or display-label hash.
export interface AdminAPITokenItem {
  readonly token_id: string;
  readonly token_class?: string;
  readonly user_id?: string;
  readonly service_principal_id?: string;
  readonly status?: string;
  readonly issued_at?: string;
  readonly expires_at?: string;
  readonly revoked_at?: string;
  readonly tenant_id?: string;
  readonly workspace_id?: string;
}

export interface AdminAPITokensResult {
  readonly tokens: readonly AdminAPITokenItem[];
  readonly truncated: boolean;
  readonly provenance: AdminProvenance;
}

interface AdminAPITokensWire {
  readonly tokens?: readonly AdminAPITokenItem[];
  readonly truncated?: boolean;
}

export async function loadApiTokens(client: EshuApiClient): Promise<AdminAPITokensResult> {
  try {
    const resp = await client.getJson<AdminAPITokensWire>("/api/v0/auth/admin/api-tokens");
    return { tokens: resp.tokens ?? [], truncated: resp.truncated ?? false, provenance: "live" };
  } catch (err) {
    console.error("[adminConsole] loadApiTokens failed", err);
    return { tokens: [], truncated: false, provenance: "unavailable" };
  }
}

// ---------------------------------------------------------------------------
// Mutators — each returns boolean success and never throws to the panel. A
// false result drives an explicit "action failed" surface in the UI.
// ---------------------------------------------------------------------------

// revokeInvitation — POST /api/v0/auth/local/invitations/{invite_id}/revoke
export async function revokeInvitation(
  client: EshuApiClient,
  inviteId: string
): Promise<boolean> {
  try {
    await client.postJson(
      `/api/v0/auth/local/invitations/${encodeURIComponent(inviteId)}/revoke`,
      {}
    );
    return true;
  } catch (err) {
    console.error("[adminConsole] revokeInvitation failed", err);
    return false;
  }
}

export interface RoleAssignmentRef {
  readonly user_id: string;
  readonly role_id: string;
  readonly workspace_id?: string;
}

// withOptionalWorkspace builds a mutation body, including workspace_id only when
// it is a non-empty string (the backend treats it as optional).
function withOptionalWorkspace(ref: RoleAssignmentRef): Record<string, string> {
  const body: Record<string, string> = { user_id: ref.user_id, role_id: ref.role_id };
  if (ref.workspace_id && ref.workspace_id.length > 0) {
    body.workspace_id = ref.workspace_id;
  }
  return body;
}

// grantRoleAssignment — POST /api/v0/auth/admin/role-assignments
export async function grantRoleAssignment(
  client: EshuApiClient,
  ref: RoleAssignmentRef
): Promise<boolean> {
  try {
    await client.postJson("/api/v0/auth/admin/role-assignments", withOptionalWorkspace(ref));
    return true;
  } catch (err) {
    console.error("[adminConsole] grantRoleAssignment failed", err);
    return false;
  }
}

// revokeRoleAssignment — POST /api/v0/auth/admin/role-assignments/revoke
export async function revokeRoleAssignment(
  client: EshuApiClient,
  ref: RoleAssignmentRef
): Promise<boolean> {
  try {
    await client.postJson(
      "/api/v0/auth/admin/role-assignments/revoke",
      withOptionalWorkspace(ref)
    );
    return true;
  } catch (err) {
    console.error("[adminConsole] revokeRoleAssignment failed", err);
    return false;
  }
}

export interface IdPGroupMappingCreate {
  readonly provider_config_id: string;
  readonly external_group: string;
  readonly role_id: string;
  readonly workspace_id?: string;
}

// createIdPGroupMapping — POST /api/v0/auth/admin/idp-group-mappings. The raw
// external_group is sent once for the server to hash; it is never stored or
// returned in clear. We do not retain or render it client-side.
export async function createIdPGroupMapping(
  client: EshuApiClient,
  input: IdPGroupMappingCreate
): Promise<boolean> {
  const body: Record<string, string> = {
    provider_config_id: input.provider_config_id,
    external_group: input.external_group,
    role_id: input.role_id
  };
  if (input.workspace_id && input.workspace_id.length > 0) {
    body.workspace_id = input.workspace_id;
  }
  try {
    await client.postJson("/api/v0/auth/admin/idp-group-mappings", body);
    return true;
  } catch (err) {
    console.error("[adminConsole] createIdPGroupMapping failed", err);
    return false;
  }
}

// deleteIdPGroupMapping — DELETE /api/v0/auth/admin/idp-group-mappings/{mapping_ref}
export async function deleteIdPGroupMapping(
  client: EshuApiClient,
  mappingRef: string
): Promise<boolean> {
  try {
    await client.delete(
      `/api/v0/auth/admin/idp-group-mappings/${encodeURIComponent(mappingRef)}`
    );
    return true;
  } catch (err) {
    console.error("[adminConsole] deleteIdPGroupMapping failed", err);
    return false;
  }
}

// revokeApiToken — POST /api/v0/auth/local/api-tokens/{token_id}/revoke. This
// route returns HTTP 204 (no body), so postNoContent is used instead of
// postJson (which would throw parsing an empty body).
export async function revokeApiToken(
  client: EshuApiClient,
  tokenId: string
): Promise<boolean> {
  try {
    await client.postNoContent(
      `/api/v0/auth/local/api-tokens/${encodeURIComponent(tokenId)}/revoke`,
      {}
    );
    return true;
  } catch (err) {
    console.error("[adminConsole] revokeApiToken failed", err);
    return false;
  }
}
