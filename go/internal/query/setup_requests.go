// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// setupClaimRequest carries the generated one-time bootstrap credential the
// operator is claiming the instance with (step 1).
type setupClaimRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// setupAdminRequest reproves the bootstrap credential and supplies the new
// password that replaces it (step 2). The login username is not
// renameable through the wizard — subject_id_hash is derived from it and
// used as an identity key across grants and sessions, so changing it after
// creation is out of scope here.
type setupAdminRequest struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	NewPassword string `json:"new_password"`
}

// setupMFARequest reproves the bootstrap credential to authorize the final
// MFA-enrollment step, which also permanently seals the wizard.
type setupMFARequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}
