package vaultlive

import "context"

// AuthMount is the metadata-only view of one Vault auth method mount. It carries
// no secret value, token, or credential material; the path and accessor are
// fingerprinted downstream by the secretsiam envelope builders.
type AuthMount struct {
	Path                   string
	Accessor               string
	Method                 string
	Local                  bool
	DefaultLeaseTTLSeconds int
	MaxLeaseTTLSeconds     int
}

// Client is the narrow, read-only Vault metadata surface the source lane needs.
//
// It is metadata-only by construction: there is deliberately no method that
// reads a KV /data value, a token, an AppRole secret_id, or any other secret
// material. New methods added here must preserve that invariant — list and
// describe metadata only, never read values.
type Client interface {
	// ListAuthMounts returns auth method mount metadata (sys/auth). It must not
	// read any secret value.
	ListAuthMounts(ctx context.Context) ([]AuthMount, error)
}
