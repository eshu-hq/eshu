// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import (
	"io"
	"strings"
	"time"

	"golang.org/x/mod/semver"

	componentcore "github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// The Run functions in this file are the bodies of the `eshu component`
// producer-grant subcommands: core-issued authorizations for first-party
// producers to emit approved core-owned fact kinds. go/cmd/eshu resolves
// every flag, environment variable, and stream, then passes plain values
// here; each function returns the error the command exits with, already
// rendered onto w in the shape the operator asked for. None of them read
// process state (the issuance instant arrives as now), which is what makes
// them testable outside the binary.

// RunGrant records a core-issued producer grant in the registry at home. The
// kind must be core-owned: namespaced kinds need no grant, so issuing one is
// an operator error. schemaVersions must be non-empty, scope must name the
// source scope the grant is valid for, and expiresIn must be positive; the
// stored expiry is now plus expiresIn. Re-issuing the same producer, version,
// kind, and scope replaces the stored grant so rotation converges.
func RunGrant(
	w io.Writer,
	jsonOutput bool,
	home string,
	producerID string,
	version string,
	kind string,
	schemaVersions []string,
	scope string,
	expiresIn time.Duration,
	now time.Time,
) error {
	if strings.TrimSpace(producerID) == "" {
		err := componentcore.Errorf(componentcore.ErrorCodeInvalidInput, "producer ID is required")
		return renderError(w, jsonOutput, "grant", err)
	}
	if !validGrantVersion(version) {
		err := componentcore.Errorf(componentcore.ErrorCodeInvalidInput, "--%s %q must be semantic version", VersionFlag, version)
		return renderError(w, jsonOutput, "grant", err)
	}
	if !facts.IsCoreFactKind(kind) {
		err := componentcore.Errorf(
			componentcore.ErrorCodeInvalidInput,
			"grant kind %q is not core-owned by Eshu; namespaced kinds need no producer grant",
			kind,
		)
		return renderError(w, jsonOutput, "grant", err)
	}
	if len(schemaVersions) == 0 {
		err := componentcore.Errorf(componentcore.ErrorCodeInvalidInput, "at least one --%s is required", GrantSchemaVersionFlag)
		return renderError(w, jsonOutput, "grant", err)
	}
	for _, schemaVersion := range schemaVersions {
		if strings.TrimSpace(schemaVersion) == "" {
			err := componentcore.Errorf(componentcore.ErrorCodeInvalidInput, "--%s must not be blank", GrantSchemaVersionFlag)
			return renderError(w, jsonOutput, "grant", err)
		}
	}
	if strings.TrimSpace(scope) == "" {
		err := componentcore.Errorf(componentcore.ErrorCodeInvalidInput, "--%s is required", GrantScopeFlag)
		return renderError(w, jsonOutput, "grant", err)
	}
	if expiresIn <= 0 {
		err := componentcore.Errorf(componentcore.ErrorCodeInvalidInput, "--%s must be positive", GrantExpiresInFlag)
		return renderError(w, jsonOutput, "grant", err)
	}
	grant := componentcore.ProducerGrant{
		ProducerID:     producerID,
		Version:        version,
		Kind:           kind,
		SchemaVersions: append([]string(nil), schemaVersions...),
		Scope:          scope,
		ExpiresAt:      now.UTC().Add(expiresIn),
	}
	if err := componentcore.NewRegistry(home).RecordGrant(grant); err != nil {
		return renderError(w, jsonOutput, "grant", err)
	}
	if jsonOutput {
		payload := newCLIOutput("grant", "granted")
		payload.Grant = &grant
		return writeJSON(w, payload)
	}
	return writef(
		w, "granted %s@%s kind %s scope %s expires %s\n",
		producerID, version, kind, scope, grant.ExpiresAt.Format(time.RFC3339),
	)
}

// RunRevokeGrant marks the stored grant for a producer, version, kind, and
// scope revoked so subsequent emissions fail closed. Revoking an absent grant
// fails with grant_not_found so a typo cannot masquerade as a completed
// revocation.
func RunRevokeGrant(
	w io.Writer,
	jsonOutput bool,
	home string,
	producerID string,
	version string,
	kind string,
	scope string,
) error {
	if strings.TrimSpace(producerID) == "" {
		err := componentcore.Errorf(componentcore.ErrorCodeInvalidInput, "producer ID is required")
		return renderError(w, jsonOutput, "revoke-grant", err)
	}
	if err := componentcore.NewRegistry(home).RevokeGrant(producerID, version, kind, scope); err != nil {
		return renderError(w, jsonOutput, "revoke-grant", err)
	}
	if jsonOutput {
		payload := newCLIOutput("revoke-grant", "revoked")
		grant := componentcore.ProducerGrant{
			ProducerID: producerID,
			Version:    version,
			Kind:       kind,
			Scope:      scope,
			Revoked:    true,
		}
		payload.Grant = &grant
		return writeJSON(w, payload)
	}
	return writef(w, "revoked grant %s@%s kind %s scope %s\n", producerID, version, kind, scope)
}

// RunGrants lists the registry's durable producer grants in record order so
// audit evidence can identify the grant behind an admission decision,
// including revoked and expired ones. A non-empty producer filters to one
// producer identity.
func RunGrants(w io.Writer, jsonOutput bool, home string, producer string) error {
	grants, err := componentcore.NewRegistry(home).ProducerGrants()
	if err != nil {
		return renderError(w, jsonOutput, "grants", err)
	}
	listed := make([]componentcore.ProducerGrant, 0, len(grants))
	for _, grant := range grants {
		if producer != "" && grant.ProducerID != producer {
			continue
		}
		listed = append(listed, grant)
	}
	if jsonOutput {
		payload := newCLIOutput("grants", "listed")
		payload.Grants = listed
		return writeJSON(w, payload)
	}
	if len(listed) == 0 {
		return writef(w, "no producer grants\n")
	}
	for _, grant := range listed {
		if err := writef(
			w, "%s\t%s\t%s\tscope=%s\tschemas=%s\texpires=%s\trevoked=%t\n",
			grant.ProducerID,
			grant.Version,
			grant.Kind,
			grant.Scope,
			strings.Join(grant.SchemaVersions, ","),
			grant.ExpiresAt.UTC().Format(time.RFC3339),
			grant.Revoked,
		); err != nil {
			return err
		}
	}
	return nil
}

// validGrantVersion mirrors the manifest metadata.version rule: a semantic
// version with an optional v prefix, compared in normalized form.
func validGrantVersion(version string) bool {
	trimmed := strings.TrimSpace(version)
	if trimmed == "" {
		return false
	}
	normalized := trimmed
	if !strings.HasPrefix(normalized, "v") {
		normalized = "v" + normalized
	}
	return semver.IsValid(normalized)
}
