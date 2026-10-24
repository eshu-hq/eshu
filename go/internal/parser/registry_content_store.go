// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
)

// ContentStorePolicy decides what the collector write boundary stores in the
// content store for a file bound to a parser definition (#8812). The policy is
// applied before any content fact or content_files row exists, so no later
// reader, search index, or export sees a body the policy withheld.
type ContentStorePolicy string

const (
	// ContentStoreStore keeps the file body verbatim. It is the zero-value
	// behavior: a definition that names no policy stores.
	ContentStoreStore ContentStorePolicy = "store"
	// ContentStoreMetadata keeps an empty-body row for the path. The row stays
	// so the repository tree, file counts, language inventory, and coverage keep
	// the path. It is never a tombstone. content_hash is still the digest of the
	// source bytes.
	ContentStoreMetadata ContentStorePolicy = "metadata"
	// ContentStoreRedact stores the body with credential values replaced, using
	// the definition's RedactorMode.
	ContentStoreRedact ContentStorePolicy = "redact"
)

// RedactorMode names the redaction algorithm a ContentStoreRedact definition
// uses: marker for closed-schema files (#8812) and skeleton for open-schema
// files (#8833).
type RedactorMode string

const (
	// RedactorMarker replaces credential values in place with
	// REDACTED_<finding_kind>. It is a denylist, so it is only safe for
	// closed-schema files whose credential keys the definition lists.
	RedactorMarker RedactorMode = "marker"
	// RedactorSkeleton stores an allowlist skeleton of an open-schema file such
	// as *.tfvars: structure and keys are kept, a value survives only when its
	// shape cannot carry a credential (boolean, number, var./local./module./
	// data. reference, version string), a detector-flagged value becomes
	// REDACTED_<finding_kind>, and every other value becomes REDACTED_value.
	// Whether a tfvars variable is secret is declared in variables.tf, not in
	// its name, so a key denylist cannot make such a file safe. It takes no key
	// list.
	RedactorSkeleton RedactorMode = "skeleton"
)

// ContentStore is the per-definition content-store policy. The zero value is
// ContentStoreStore.
type ContentStore struct {
	// Policy is the admission policy. Empty means ContentStoreStore.
	Policy ContentStorePolicy
	// Redactor is required when Policy is ContentStoreRedact and must be empty
	// otherwise.
	Redactor RedactorMode
	// CredentialKeys lists the keys whose values RedactorMarker replaces at any
	// length. It is required for RedactorMarker and must be empty otherwise.
	CredentialKeys []string
	// URLUserinfoKeys lists URL-valued keys (for .yarnrc.yml npmRegistryServer,
	// npmPublishRegistry, httpProxy, httpsProxy) whose value RedactorMarker
	// keeps except for the URL userinfo, which becomes a marker, so the host
	// stays readable. It is optional for RedactorMarker, must be empty
	// otherwise, and must not share a key with CredentialKeys (keys match
	// case-insensitively, so a shared key would be ambiguous).
	URLUserinfoKeys []string
}

// ContentStoreForPath returns the content-store policy for one file path. A
// path no definition binds reports ContentStoreStore. The returned key slice
// is a copy.
func (r Registry) ContentStoreForPath(path string) ContentStore {
	if !r.governed {
		return ContentStore{Policy: ContentStoreStore}
	}
	definition, ok := r.LookupByPath(path)
	if !ok {
		return ContentStore{Policy: ContentStoreStore}
	}
	return definition.ContentStore
}

// HasContentStorePolicy reports whether any definition binds a non-store
// policy. The default registry is governed since #8978 binds .npmrc to
// metadata, so the collector looks up each file's policy; a registry that binds
// only store skips the lookup entirely.
func (r Registry) HasContentStorePolicy() bool {
	return r.governed
}

// normalizeContentStore validates one definition's policy and returns it with
// the policy defaulted and credential keys trimmed, de-duplicated, and sorted.
func normalizeContentStore(parserKey string, store ContentStore) (ContentStore, error) {
	if store.Policy == "" {
		store.Policy = ContentStoreStore
	}
	if err := validateKeyList(parserKey, "credential", store.CredentialKeys); err != nil {
		return ContentStore{}, err
	}
	if err := validateKeyList(parserKey, "URL userinfo", store.URLUserinfoKeys); err != nil {
		return ContentStore{}, err
	}
	store.CredentialKeys = normalizeCredentialKeys(store.CredentialKeys)
	store.URLUserinfoKeys = normalizeCredentialKeys(store.URLUserinfoKeys)

	switch store.Policy {
	case ContentStoreStore, ContentStoreMetadata:
		if store.Redactor != "" {
			return ContentStore{}, fmt.Errorf("parser key %q: content store policy %q takes no redactor mode, got %q", parserKey, store.Policy, store.Redactor)
		}
		if len(store.CredentialKeys) > 0 || len(store.URLUserinfoKeys) > 0 {
			return ContentStore{}, fmt.Errorf("parser key %q: content store policy %q takes no credential keys", parserKey, store.Policy)
		}
	case ContentStoreRedact:
		switch store.Redactor {
		case RedactorMarker:
			if len(store.CredentialKeys) == 0 {
				return ContentStore{}, fmt.Errorf("parser key %q: marker redaction needs a credential key list", parserKey)
			}
			if shared := sharedKeyFold(store.CredentialKeys, store.URLUserinfoKeys); shared != "" {
				return ContentStore{}, fmt.Errorf("parser key %q: key %q is both a credential key and a URL userinfo key", parserKey, shared)
			}
		case RedactorSkeleton:
			if len(store.CredentialKeys) > 0 || len(store.URLUserinfoKeys) > 0 {
				return ContentStore{}, fmt.Errorf("parser key %q: skeleton redaction is an allowlist and takes no key list", parserKey)
			}
		default:
			return ContentStore{}, fmt.Errorf("parser key %q: content store policy redact needs a known redactor mode, got %q", parserKey, store.Redactor)
		}
	default:
		return ContentStore{}, fmt.Errorf("parser key %q: unknown content store policy %q", parserKey, store.Policy)
	}
	return store, nil
}

// validateKeyList rejects a blank key and a key holding a comma or a control
// character. The fingerprint joins keys with "," and ends each definition with
// a newline, so such a key could make two different key lists hash alike.
func validateKeyList(parserKey, list string, keys []string) error {
	for _, key := range keys {
		if strings.TrimSpace(key) == "" {
			return fmt.Errorf("parser key %q: content store %s keys must not be blank", parserKey, list)
		}
		if strings.ContainsFunc(key, func(r rune) bool { return r == ',' || unicode.IsControl(r) }) {
			return fmt.Errorf("parser key %q: content store %s key %q must not hold a comma or a control character", parserKey, list, key)
		}
	}
	return nil
}

// sharedKeyFold returns a key present in both lists under case folding, or "".
func sharedKeyFold(a, b []string) string {
	for _, left := range a {
		for _, right := range b {
			if strings.EqualFold(left, right) {
				return right
			}
		}
	}
	return ""
}

// normalizeCredentialKeys trims, de-duplicates, and sorts keys. Keys are
// case-sensitive: the file format decides what a key is.
func normalizeCredentialKeys(keys []string) []string {
	if len(keys) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(keys))
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	slices.Sort(out)
	return out
}

// fingerprintContentStore renders one definition's policy for Registry
// .Fingerprint. A store policy renders empty so a registry that binds no
// non-store policy keeps the hash it had before #8812; the first move happens
// when a definition binds metadata or redact.
func fingerprintContentStore(store ContentStore) string {
	if store.Policy == "" || store.Policy == ContentStoreStore {
		return ""
	}
	token := fmt.Sprintf("\x00content_store=%s:%s:%s", store.Policy, store.Redactor, strings.Join(store.CredentialKeys, ","))
	// The URL key list renders only when present, so a #8812 marker definition
	// keeps its exact token.
	if len(store.URLUserinfoKeys) > 0 {
		token += ":url_userinfo=" + strings.Join(store.URLUserinfoKeys, ",")
	}
	return token
}

func cloneContentStore(store ContentStore) ContentStore {
	store.CredentialKeys = slices.Clone(store.CredentialKeys)
	store.URLUserinfoKeys = slices.Clone(store.URLUserinfoKeys)
	return store
}

// contentStoreLabel renders one definition's policy for the generated
// admission table: store, metadata, or redact (<mode>).
func contentStoreLabel(store ContentStore) string {
	switch store.Policy {
	case ContentStoreMetadata:
		return "metadata"
	case ContentStoreRedact:
		return fmt.Sprintf("redact (%s)", store.Redactor)
	default:
		return "store"
	}
}
