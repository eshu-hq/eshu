// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package docs

import (
	"net/url"

	"github.com/eshu-hq/eshu/go/internal/doctruth"
)

// EnvelopeGetter is the one method this package needs from the CLI's API
// client: issue a GET against an Eshu API path and decode the response
// envelope. The CLI wrapper owns building the client (service URL, API key,
// profile all come from flags and the environment) and passes it in here.
type EnvelopeGetter interface {
	GetEnvelope(path string, result any) error
}

// containerImageIdentityPage is the data half of the supply-chain container
// image identity response. Only the fields this resolver reads are declared.
type containerImageIdentityPage struct {
	Identities []struct {
		IdentityID string `json:"identity_id"`
		ImageRef   string `json:"image_ref"`
		Outcome    string `json:"outcome"`
	} `json:"identities"`
}

// containerImageIdentityEnvelope is the response envelope wrapping
// containerImageIdentityPage.
type containerImageIdentityEnvelope struct {
	Data  containerImageIdentityPage `json:"data"`
	Error *EnvelopeError             `json:"error"`
}

// APIContainerImageResolver builds the resolver that checks a documented
// container image reference against a running Eshu API instead of the local
// manifests. Results are memoized per normalized reference, so a reference
// repeated across documents costs one request.
//
// A nil client, a transport error, or an error envelope all yield an
// unsupported resolution, which reports the claim as missing evidence rather
// than contradicted -- an unreachable API is not evidence that an image is
// absent.
func APIContainerImageResolver(client EnvelopeGetter) doctruth.ContainerImageResolver {
	cache := map[string]doctruth.ContainerImageResolution{}
	return func(_ doctruth.DocumentInput, imageRef string) doctruth.ContainerImageResolution {
		normalized := doctruth.NormalizeContainerImageRefClaim(imageRef)
		if normalized == "" || client == nil {
			return doctruth.ContainerImageResolution{}
		}
		if cached, ok := cache[normalized]; ok {
			return cached
		}
		query := url.Values{}
		query.Set("image_ref", normalized)
		query.Set("limit", "1")
		var envelope containerImageIdentityEnvelope
		err := client.GetEnvelope("/api/v0/supply-chain/container-images/identities?"+query.Encode(), &envelope)
		if err != nil || envelope.Error != nil {
			cache[normalized] = doctruth.ContainerImageResolution{}
			return cache[normalized]
		}
		cache[normalized] = doctruth.ContainerImageResolution{Supported: true, Exists: len(envelope.Data.Identities) > 0}
		return cache[normalized]
	}
}
