// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
)

// staticPrivateKeyPEM is a fixed, synthetic 2048-bit RSA private key used
// only to sign ID tokens issued by this mock identity provider. This binary
// exists solely to give the local and CI browser-auth E2E suites (issue
// #4971, epic #4962) a deterministic OIDC counterparty over synthetic
// example.test identities, so the key is committed in the clear rather than
// generated at random per process start — a random key would still verify
// fine within one process lifetime, but a fixed key keeps JWKS output
// reproducible across container restarts and makes a captured id_token
// replayable in a bug report. It is not a secret and must never be reused
// outside this mock IdP or presented as a real OIDC signing key.
// #nosec G101 -- synthetic dev-only test key, not a real credential; see comment above
const staticPrivateKeyPEM = `-----BEGIN RSA PRIVATE KEY-----
MIIEowIBAAKCAQEAzVVruEkswSNmxWOKddSeRkyk3KcXAUIFS6BvQhPUraD9yqkO
UzEPsO+/ygpVBkksGo5ZWtvUhKFGWbg80tu1mMXrXzMW7gnuJccai2l73OkIqLTg
Z6j72LAw6xXvifLtEPyDjqU1pQD5jBzjE8dE3t1fNgPgAko6IrDH9DTX3vcExI+/
flar6tfHLSdmC8lMtaZ3nHtb8JppHAonr5PDQS/wUoYomQgiJkoT/T5ZDzDDnGpw
D8LMOlRPy6v0iIv++sGaMPGzH5WbdVvv5SXSwnRuPw7wOPSo0yYA+e1+M40wAyeQ
7/WQlH9203Hei/cCVJ9Go+whxlqLraptBWhOgwIDAQABAoIBACSuqfvWNsSaSnXg
/o9mWJA+iQVSZG25GbEVDEtFt6z9Idnescxy61a0vhKeHeptkA9+dsAgnarEFQla
PKN+1MZiNVZgjiwgYgcltrxJL/ObPgzFo4FhUvy3HUYiORTv7SInumj1YswDjJFX
Z8nUw5z891FzB8Xg9NnVsyMRfa87J6+Gq+FkplPLRd7lGkahZLzPiqFYvq4nf5M5
efwq1kQM7hwbWxl/AFdLaYcRzJWhQJQkW6FYiaXtNMSMfT6++twd8g3dTC30tnw3
HKThzqEOJeq2Vptu94jLUuthe0Z1YrtGSy1eJxnTjySuncLlAKMnWLs/CTRYqnlq
1usFpSECgYEA/UsyRR6i84is52rdHNSJlApTeuVrGJegyqy10YPE8yIAyYTwzWgZ
v1IXME8FDYaclRALMPQvBBUWKgJF/cEg5l/agZXNiVFzCaMEtUx429o+CNdRmPbU
rt7dzeE0T7pbuCrgCISwdX+UIk0t/Dgx6CjNm3gUmqjIB5No/LYAE5UCgYEAz4cL
ih2RypDWQyfEnn8mSKuUmHb3xfOlnjNOzNIN6DK0qHxjFUpsjYwix0nrjJ4N4UoO
Qxf82FwEzSZXqyQ1zAItXiRh9DUlLw6nf/QbWgah0kasbpJhVS6Ob/+Gkhfr6QFz
j1X82i+JcIHEIwQIMgt1ZjHhwD3OBANmQLC9U7cCgYBM7ExM/3vfV++irCqQed96
TRSDKy82Hb5gLslc4paqe/YfPTdgOjAvkT+nlSfkrq/Y+TFc4ZtxsvGzOlOFN+TE
8fFLD3KHNGwqTg68/IdrxUC3sKSAPt3iaZ6UysL3P5JhQOweyiVI2cDkFepUQcCu
T835XCNtwLpWyqbEsIUfbQKBgH43YTJQR6JOsrHHVxMau/sIt+h+urVfOURdajiy
LJkjdbLfbBe/2wO/zksszyEH4+M4ejIePb6NQLJQ9pL1A+8fB96w/A5d4E6deAwf
OB9p1zOfnjHlv2LiXOkLHRpviCB/rHvpzU4aCVou4k51nlJpm65a+jVEoa7ZLnB7
zbpDAoGBALs5m4T/NgZa/h89y38adwmMDhYHupqwkvMzfZR26HltknyCYS1BDOei
ZS+QuTVpfvs4oHKk4lR973k75Tha9RRmVzvt5WsNnxy8OTmhqJGVG3616clR37JC
nfEKzhdF03GGfLiVdbt8LbDS53SnZP4s+T2VaUDRUFus+TqOwOMI
-----END RSA PRIVATE KEY-----
`

// loadStaticSigningKey parses the embedded key and derives a stable key ID
// (kid) from the SHA-256 of its DER-encoded public key, so JWKS output and
// signed token headers agree with each other across process restarts without
// persisting any state.
func loadStaticSigningKey() (*rsa.PrivateKey, string, error) {
	block, _ := pem.Decode([]byte(staticPrivateKeyPEM))
	if block == nil {
		return nil, "", fmt.Errorf("mock-oidc-idp: decode static private key PEM")
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, "", fmt.Errorf("mock-oidc-idp: parse static private key: %w", err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return nil, "", fmt.Errorf("mock-oidc-idp: marshal static public key: %w", err)
	}
	sum := sha256.Sum256(pubDER)
	kid := hex.EncodeToString(sum[:])[:16]
	return key, kid, nil
}

// jwk is one RSA public signing key in JSON Web Key form (RFC 7517).
type jwk struct {
	Kty string `json:"kty"`
	Use string `json:"use"`
	Kid string `json:"kid"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// jwkSet is the JSON Web Key Set document served at GET /jwks.
type jwkSet struct {
	Keys []jwk `json:"keys"`
}

// buildJWKS renders an RSA public key as a single-key JWKS document. Only
// RSA keys are supported: this mock IdP only ever signs with the static
// RS256 key loaded by loadStaticSigningKey.
func buildJWKS(pub *rsa.PublicKey, kid string) jwkSet {
	eBytes := big.NewInt(int64(pub.E)).Bytes()
	return jwkSet{Keys: []jwk{{
		Kty: "RSA",
		Use: "sig",
		Kid: kid,
		Alg: "RS256",
		N:   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
		E:   base64.RawURLEncoding.EncodeToString(eBytes),
	}}}
}
