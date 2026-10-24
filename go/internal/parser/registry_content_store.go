// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"fmt"
	"slices"
	"strings"
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
// uses. #8833 adds an allowlist-skeleton mode for open-schema files; the field
// exists now so that mode needs no second policy value.
type RedactorMode string

const (
	// RedactorMarker replaces credential values in place with
	// REDACTED_<finding_kind>. It is a denylist, so it is only safe for
	// closed-schema files whose credential keys the definition lists.
	RedactorMarker RedactorMode = "marker"
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
// policy. It is false for the default registry, so the collector skips the
// per-file policy lookup entirely until #8833 binds a definition.
func (r Registry) HasContentStorePolicy() bool {
	return r.governed
}

// normalizeContentStore validates one definition's policy and returns it with
// the policy defaulted and credential keys trimmed, de-duplicated, and sorted.
func normalizeContentStore(parserKey string, store ContentStore) (ContentStore, error) {
	if store.Policy == "" {
		store.Policy = ContentStoreStore
	}
	for _, key := range store.CredentialKeys {
		if strings.TrimSpace(key) == "" {
			return ContentStore{}, fmt.Errorf("parser key %q: content store credential keys must not be blank", parserKey)
		}
	}
	store.CredentialKeys = normalizeCredentialKeys(store.CredentialKeys)

	switch store.Policy {
	case ContentStoreStore, ContentStoreMetadata:
		if store.Redactor != "" {
			return ContentStore{}, fmt.Errorf("parser key %q: content store policy %q takes no redactor mode, got %q", parserKey, store.Policy, store.Redactor)
		}
		if len(store.CredentialKeys) > 0 {
			return ContentStore{}, fmt.Errorf("parser key %q: content store policy %q takes no credential keys", parserKey, store.Policy)
		}
	case ContentStoreRedact:
		switch store.Redactor {
		case RedactorMarker:
			if len(store.CredentialKeys) == 0 {
				return ContentStore{}, fmt.Errorf("parser key %q: marker redaction needs a credential key list", parserKey)
			}
		default:
			return ContentStore{}, fmt.Errorf("parser key %q: content store policy redact needs a known redactor mode, got %q", parserKey, store.Redactor)
		}
	default:
		return ContentStore{}, fmt.Errorf("parser key %q: unknown content store policy %q", parserKey, store.Policy)
	}
	return store, nil
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
	return fmt.Sprintf("\x00content_store=%s:%s:%s", store.Policy, store.Redactor, strings.Join(store.CredentialKeys, ","))
}

func cloneContentStore(store ContentStore) ContentStore {
	store.CredentialKeys = slices.Clone(store.CredentialKeys)
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
