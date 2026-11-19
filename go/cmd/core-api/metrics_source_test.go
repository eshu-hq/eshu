// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func testCoreMetricsInstance(kind scope.CollectorKind, id string, enabled bool) workflow.DesiredCollectorInstance {
	return workflow.DesiredCollectorInstance{
		CollectorKind: kind,
		InstanceID:    id,
		Enabled:       enabled,
		Configuration: `{"targets":[]}`,
	}
}

// TestSelectCoreMetricsCollectorInstance pins the instance-selection rules:
// only enabled prometheus/mimir collectors are eligible, an explicit request
// must match, and ambiguity fails closed instead of picking a winner.
func TestSelectCoreMetricsCollectorInstance(t *testing.T) {
	t.Parallel()
	prom := testCoreMetricsInstance(scope.CollectorPrometheusMimir, "mimir-1", true)
	cases := map[string]struct {
		instances []workflow.DesiredCollectorInstance
		requested string
		wantID    string
		wantOK    bool
		wantErr   string
	}{
		"none configured": {nil, "", "", false, ""},
		"only disabled": {
			[]workflow.DesiredCollectorInstance{testCoreMetricsInstance(scope.CollectorPrometheusMimir, "mimir-1", false)},
			"", "", false, "",
		},
		"wrong kind ignored": {
			[]workflow.DesiredCollectorInstance{testCoreMetricsInstance("loki", "loki-1", true)},
			"", "", false, "",
		},
		"single enabled wins": {[]workflow.DesiredCollectorInstance{prom}, "", "mimir-1", true, ""},
		"explicit request matches": {
			[]workflow.DesiredCollectorInstance{
				prom,
				testCoreMetricsInstance(scope.CollectorPrometheusMimir, "mimir-2", true),
			},
			"mimir-2", "mimir-2", true, "",
		},
		"explicit request unknown": {
			[]workflow.DesiredCollectorInstance{prom},
			"mimir-9", "", false, "not found",
		},
		"ambiguous without request": {
			[]workflow.DesiredCollectorInstance{
				prom,
				testCoreMetricsInstance(scope.CollectorPrometheusMimir, "mimir-2", true),
			},
			"", "", false, "multiple",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, ok, err := selectCoreMetricsCollectorInstance(tc.instances, tc.requested)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v, want nil", err)
			}
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if ok && got.InstanceID != tc.wantID {
				t.Fatalf("instance = %q, want %q", got.InstanceID, tc.wantID)
			}
		})
	}
}

// TestSelectCoreMetricsTimeSeriesTarget pins the target-mapping rules:
// malformed config fails, no enabled target fails closed, ambiguity fails
// closed, and secret references must resolve.
func TestSelectCoreMetricsTimeSeriesTarget(t *testing.T) {
	t.Parallel()
	getenv := func(env map[string]string) func(string) string {
		return func(k string) string { return env[k] }
	}
	base := workflow.DesiredCollectorInstance{
		CollectorKind: scope.CollectorPrometheusMimir,
		InstanceID:    "mimir-1",
		Enabled:       true,
	}
	cases := map[string]struct {
		config  string
		env     map[string]string
		wantURL string
		wantOK  bool
		wantErr string
	}{
		"malformed config": {`{`, nil, "", false, "decode"},
		"no enabled targets": {
			`{"targets":[{"provider":"mimir","base_url":"http://m:8080","enabled":false}]}`,
			nil, "", false, "",
		},
		"single enabled target": {
			`{"targets":[{"provider":"mimir","base_url":"http://m:8080/","path_prefix":"api/v1","enabled":true}]}`,
			nil, "http://m:8080", true, "",
		},
		"token env must resolve": {
			`{"targets":[{"provider":"prometheus","base_url":"http://p:9090","token_env":"MISSING_TOKEN","enabled":true}]}`,
			nil, "", false, "did not resolve",
		},
		"token env resolved": {
			`{"targets":[{"provider":"prometheus","base_url":"http://p:9090","token_env":"P_TOKEN","enabled":true}]}`,
			map[string]string{"P_TOKEN": "secret"},
			"http://p:9090", true, "",
		},
		"ambiguous targets": {
			`{"targets":[{"provider":"mimir","base_url":"http://a:8080","enabled":true},{"provider":"mimir","base_url":"http://b:8080","enabled":true}]}`,
			nil, "", false, "multiple",
		},
		"unknown provider": {
			`{"targets":[{"provider":"thanos","base_url":"http://t:10902","enabled":true}]}`,
			nil, "", false, "provider must be",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			inst := base
			inst.Configuration = tc.config
			got, ok, err := selectCoreMetricsTimeSeriesTarget(inst, getenv(tc.env))
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v, want nil", err)
			}
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if ok && got.BaseURL != tc.wantURL {
				t.Fatalf("baseURL = %q, want %q", got.BaseURL, tc.wantURL)
			}
		})
	}
}
