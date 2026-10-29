// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import "testing"

func TestLoadRuntimeConfigEnablesDerivedRegistryImages(t *testing.T) {
	t.Parallel()
	raw := `[{
      "instance_id":"scanner-worker-derived", "collector_kind":"scanner_worker",
      "mode":"continuous", "enabled":true, "claims_enabled":true,
      "configuration":{
        "analyzer":"image_unpacking", "derive_from_running_images":true,
        "registry_domains":[{"authorization_domain":"qa-public","registry":"registry.example",
          "base_url":"https://registry.example","auth_mode":"anonymous"}]
      }
    }]`
	cfg, err := loadRuntimeConfig(func(key string) string {
		if key == envCollectorInstances {
			return raw
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.DeriveFromRunningImages {
		t.Fatalf("derived config = %#v", cfg)
	}
	domains, err := parseRegistryDomains(cfg.Instance.Configuration)
	if err != nil || len(domains) != 1 {
		t.Fatalf("domains = %#v, err = %v", domains, err)
	}
}
