// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package identity

// Fixed client-facing messages for admin identity route failures (#7674).
// The two audit reads keep their own constants in audit.go.
// Each failing step answers exactly its constant through
// tracing.WriteServerFailure: 500 with the store error recorded on the
// request span, or 499 with only the client-cancel event when the caller
// canceled. The store error never reaches the response body.
const (
	invitationListFailedMessage      = "failed to list invitations"
	roleAssignmentListFailedMessage  = "failed to list role assignments"
	roleListFailedMessage            = "failed to list roles"
	idpProviderListFailedMessage     = "failed to list idp providers"
	idpGroupMappingListFailedMessage = "failed to list idp group mappings"
	apiAccessKeyListFailedMessage    = "failed to list api tokens"

	invitationRevokeFailedMessage      = "failed to revoke invitation"
	roleAssignmentGrantFailedMessage   = "failed to grant role assignment"
	roleAssignmentRevokeFailedMessage  = "failed to revoke role assignment"
	idpGroupMappingCreateFailedMessage = "failed to create idp group mapping"
	idpGroupMappingDeleteFailedMessage = "failed to delete idp group mapping"
)
