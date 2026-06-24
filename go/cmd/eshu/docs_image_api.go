// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"net/url"
	"os"
	"strings"

	"github.com/spf13/pflag"

	"github.com/eshu-hq/eshu/go/internal/doctruth"
)

type remoteFlagReader interface {
	Flags() *pflag.FlagSet
}

type docsVerifyContainerImageIdentityPage struct {
	Identities []struct {
		IdentityID string `json:"identity_id"`
		ImageRef   string `json:"image_ref"`
		Outcome    string `json:"outcome"`
	} `json:"identities"`
}

type docsVerifyContainerImageIdentityEnvelope struct {
	Data  docsVerifyContainerImageIdentityPage `json:"data"`
	Error *docsVerifyError                     `json:"error"`
}

func docsVerifyContainerImageAPIResolver(client *APIClient) doctruth.ContainerImageResolver {
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
		var envelope docsVerifyContainerImageIdentityEnvelope
		err := client.GetEnvelope("/api/v0/supply-chain/container-images/identities?"+query.Encode(), &envelope)
		if err != nil || envelope.Error != nil {
			cache[normalized] = doctruth.ContainerImageResolution{}
			return cache[normalized]
		}
		cache[normalized] = doctruth.ContainerImageResolution{Supported: true, Exists: len(envelope.Data.Identities) > 0}
		return cache[normalized]
	}
}

func docsVerifyRemoteImageTruthConfigured(cmd remoteFlagReader) bool {
	for _, name := range []string{"service-url", "api-key", "profile"} {
		if flag := cmd.Flags().Lookup(name); flag != nil && flag.Changed && strings.TrimSpace(flag.Value.String()) != "" {
			return true
		}
	}
	return strings.TrimSpace(os.Getenv("ESHU_SERVICE_URL")) != "" ||
		strings.TrimSpace(os.Getenv("ESHU_API_KEY")) != ""
}

func apiClientFromRemoteFlags(cmd remoteFlagReader) *APIClient {
	return NewAPIClient(
		docsVerifyRemoteFlagValue(cmd, "service-url"),
		docsVerifyRemoteFlagValue(cmd, "api-key"),
		docsVerifyRemoteFlagValue(cmd, "profile"),
	)
}

func docsVerifyRemoteFlagValue(cmd remoteFlagReader, name string) string {
	if flag := cmd.Flags().Lookup(name); flag != nil {
		return flag.Value.String()
	}
	return ""
}
