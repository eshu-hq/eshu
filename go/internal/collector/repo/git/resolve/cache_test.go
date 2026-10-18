// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package resolve

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// fakeLookup is a scripted LookupFunc. Each call returns results[min(call,last)]
// and may block on gate so concurrency can be observed.
type fakeLookup struct {
	mu      sync.Mutex
	results []lookupResult
	calls   atomic.Int64
	gate    chan struct{} // when non-nil, every call waits for it (or ctx)
}

type lookupResult struct {
	ips []net.IP
	err error
}

func (f *fakeLookup) lookup(ctx context.Context, _ string) ([]net.IP, error) {
	n := int(f.calls.Add(1)) - 1
	if f.gate != nil {
		select {
		case <-f.gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if n >= len(f.results) {
		n = len(f.results) - 1
	}
	r := f.results[n]
	return r.ips, r.err
}

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func testCache(t *testing.T, lookup *fakeLookup, clock *fakeClock, mutate func(*Settings, *Options)) (*Cache, *sdkmetric.ManualReader) {
	t.Helper()
	settings := Settings{Enabled: true, LookupTimeout: 2 * time.Second, MinGap: 30 * time.Second, RefreshInterval: time.Hour}
	options := Options{Lookup: lookup.lookup, Getenv: func(string) string { return "" }}
	if clock != nil {
		options.Now = clock.Now
	}
	if mutate != nil {
		mutate(&settings, &options)
	}
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	cache := New(settings, options)
	cache.Bind(instruments, nil)
	t.Cleanup(cache.Close)
	return cache, reader
}

func counterValue(t *testing.T, reader *sdkmetric.ManualReader, name string, want map[string]string) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	var total int64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric %s data = %T, want Sum[int64]", name, m.Data)
			}
			for _, dp := range sum.DataPoints {
				match := true
				for k, v := range want {
					got, ok := dp.Attributes.Value(attribute.Key(k))
					if !ok || got.AsString() != v {
						match = false
						break
					}
				}
				if match {
					total += dp.Value
				}
			}
		}
	}
	return total
}

// TestArgsCacheMissDoesOneBoundedLookupAndHitPassesAllAddresses: the first
// command for a host waits for one lookup, then carries every A and AAAA
// address in resolver order; a later command is a hit with no new lookup.
func TestArgsCacheMissDoesOneBoundedLookupAndHitPassesAllAddresses(t *testing.T) {
	lookup := &fakeLookup{results: []lookupResult{
		{ips: []net.IP{net.ParseIP("140.82.121.4"), net.ParseIP("2606:50c0:8000::154"), net.ParseIP("140.82.121.3")}},
	}}
	cache, reader := testCache(t, lookup, nil, nil)

	want := []string{"-c", "http.curloptResolve=github.com:443:140.82.121.4,2606:50c0:8000::154,140.82.121.3"}
	if got := cache.Args(context.Background(), "github.com", 443); !slices.Equal(got, want) {
		t.Fatalf("Args (miss) = %v, want %v", got, want)
	}
	if got := cache.Args(context.Background(), "github.com", 443); !slices.Equal(got, want) {
		t.Fatalf("Args (hit) = %v, want %v", got, want)
	}
	if n := lookup.calls.Load(); n != 1 {
		t.Fatalf("lookups = %d, want exactly 1", n)
	}
	if n := counterValue(t, reader, "eshu_dp_git_resolve_cache_lookups_total", map[string]string{"host": "github.com", "result": "success"}); n != 1 {
		t.Fatalf("lookups_total{success} = %d, want 1", n)
	}
}

// TestArgsEmptyCacheAfterFailedLookupFallsBack: a failed first lookup leaves
// the cache empty, the command runs without the option (counted cache_empty),
// and an empty result is never written over a last-good set.
func TestArgsEmptyCacheAfterFailedLookupFallsBack(t *testing.T) {
	lookup := &fakeLookup{results: []lookupResult{{err: errors.New("no such host")}}}
	cache, reader := testCache(t, lookup, nil, nil)

	if got := cache.Args(context.Background(), "github.com", 443); got != nil {
		t.Fatalf("Args = %v, want nil fallback", got)
	}
	if n := counterValue(t, reader, "eshu_dp_git_resolve_cache_fallback_total", map[string]string{"host": "github.com", "reason": "cache_empty"}); n != 1 {
		t.Fatalf("fallback_total{cache_empty} = %d, want 1", n)
	}
	if n := counterValue(t, reader, "eshu_dp_git_resolve_cache_lookups_total", map[string]string{"host": "github.com", "result": "error"}); n != 1 {
		t.Fatalf("lookups_total{error} = %d, want 1", n)
	}
}

// TestFailedRefreshKeepsLastGoodSet: a refresh that fails or returns no
// addresses leaves the previous addresses pinned.
func TestFailedRefreshKeepsLastGoodSet(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1_700_000_000, 0)}
	lookup := &fakeLookup{results: []lookupResult{
		{ips: []net.IP{net.ParseIP("10.0.0.1")}},
		{err: errors.New("resolver down")},
		{ips: nil},
	}}
	cache, _ := testCache(t, lookup, clock, nil)
	want := []string{"-c", "http.curloptResolve=example.com:443:10.0.0.1"}
	if got := cache.Args(context.Background(), "example.com", 443); !slices.Equal(got, want) {
		t.Fatalf("Args = %v, want %v", got, want)
	}
	for i := 0; i < 2; i++ {
		clock.advance(31 * time.Second)
		<-cache.RequestRefresh("example.com")
		if got := cache.Args(context.Background(), "example.com", 443); !slices.Equal(got, want) {
			t.Fatalf("after failed refresh %d Args = %v, want last good %v", i, got, want)
		}
	}
	if n := lookup.calls.Load(); n != 3 {
		t.Fatalf("lookups = %d, want 3 (initial + error + empty)", n)
	}
}

// TestConcurrentRefreshSingleFlight: N goroutines failing at once cause
// exactly one lookup (concurrency proof; run with -race).
func TestConcurrentRefreshSingleFlight(t *testing.T) {
	lookup := &fakeLookup{
		results: []lookupResult{{ips: []net.IP{net.ParseIP("10.0.0.9")}}},
		gate:    make(chan struct{}),
	}
	cache, _ := testCache(t, lookup, nil, nil)

	const goroutines = 32
	waits := make([]<-chan struct{}, goroutines)
	var wg sync.WaitGroup
	for i := range waits {
		wg.Add(1)
		go func() {
			defer wg.Done()
			waits[i] = cache.RequestRefresh("github.com")
		}()
	}
	wg.Wait()
	close(lookup.gate)
	for _, w := range waits {
		<-w
	}
	if n := lookup.calls.Load(); n != 1 {
		t.Fatalf("lookups = %d, want exactly 1 for %d concurrent requests", n, goroutines)
	}
}

// TestRefreshWithinMinGapIsSuppressed: a second failure inside the minimum
// gap starts no lookup and is counted reason=suppressed.
func TestRefreshWithinMinGapIsSuppressed(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1_700_000_000, 0)}
	lookup := &fakeLookup{results: []lookupResult{{ips: []net.IP{net.ParseIP("10.0.0.1")}}}}
	cache, reader := testCache(t, lookup, clock, nil)

	<-cache.RequestRefresh("github.com")
	clock.advance(10 * time.Second) // inside the 30s gap
	<-cache.RequestRefresh("github.com")
	if n := lookup.calls.Load(); n != 1 {
		t.Fatalf("lookups = %d, want 1 (second request suppressed)", n)
	}
	if n := counterValue(t, reader, "eshu_dp_git_resolve_cache_refresh_requests_total", map[string]string{"host": "github.com", "reason": "suppressed"}); n != 1 {
		t.Fatalf("refresh_requests_total{suppressed} = %d, want 1", n)
	}
	clock.advance(21 * time.Second) // past the gap
	<-cache.RequestRefresh("github.com")
	if n := lookup.calls.Load(); n != 2 {
		t.Fatalf("lookups = %d, want 2 after the gap", n)
	}
}

// TestArgsFirstCommandWaitsAtMostLookupTimeout: a hung resolver costs the
// first command at most the lookup deadline, then it runs unpinned.
func TestArgsFirstCommandWaitsAtMostLookupTimeout(t *testing.T) {
	lookup := &fakeLookup{
		results: []lookupResult{{ips: []net.IP{net.ParseIP("10.0.0.1")}}},
		gate:    make(chan struct{}), // never closed: the lookup only ends by ctx
	}
	cache, _ := testCache(t, lookup, nil, func(s *Settings, _ *Options) {
		s.LookupTimeout = 100 * time.Millisecond
	})
	start := time.Now()
	if got := cache.Args(context.Background(), "slow.example", 443); got != nil {
		t.Fatalf("Args = %v, want nil after the lookup deadline", got)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Args blocked %v, want about the 100ms lookup deadline", elapsed)
	}
}

// TestPeriodicRefreshReplacesAddresses: the hourly tick refreshes each known
// host through the same single-flight path and replaces only on success.
func TestPeriodicRefreshReplacesAddresses(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1_700_000_000, 0)}
	tick := make(chan time.Time, 1)
	lookup := &fakeLookup{results: []lookupResult{
		{ips: []net.IP{net.ParseIP("10.0.0.1")}},
		{ips: []net.IP{net.ParseIP("10.0.0.2")}},
	}}
	cache, reader := testCache(t, lookup, clock, func(_ *Settings, o *Options) { o.Tick = tick })
	_ = cache.Args(context.Background(), "example.com", 443)

	clock.advance(2 * time.Hour)
	tick <- clock.Now()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := cache.Args(context.Background(), "example.com", 443); slices.Equal(got, []string{"-c", "http.curloptResolve=example.com:443:10.0.0.2"}) {
			if n := counterValue(t, reader, "eshu_dp_git_resolve_cache_refresh_requests_total", map[string]string{"host": "example.com", "reason": "periodic"}); n != 1 {
				t.Fatalf("refresh_requests_total{periodic} = %d, want 1", n)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("periodic refresh did not replace the addresses")
}

// TestNoInjectionUnderProxyOrKillSwitchOrSSHishInputs covers the three
// no-pin gates: a proxy setting, the kill switch, and non-host inputs.
func TestNoInjectionUnderProxyOrKillSwitchOrIPHost(t *testing.T) {
	lookup := &fakeLookup{results: []lookupResult{{ips: []net.IP{net.ParseIP("10.0.0.1")}}}}

	proxyCache, proxyReader := testCache(t, lookup, nil, func(_ *Settings, o *Options) {
		o.Getenv = func(key string) string {
			if key == "HTTPS_PROXY" {
				return "http://proxy.example:3128"
			}
			return ""
		}
	})
	if got := proxyCache.Args(context.Background(), "github.com", 443); got != nil {
		t.Fatalf("Args under proxy = %v, want nil", got)
	}
	if n := counterValue(t, proxyReader, "eshu_dp_git_resolve_cache_fallback_total", map[string]string{"host": "github.com", "reason": "proxy_set"}); n != 1 {
		t.Fatalf("fallback_total{proxy_set} = %d, want 1", n)
	}

	offCache, _ := testCache(t, lookup, nil, func(s *Settings, _ *Options) { s.Enabled = false })
	if got := offCache.Args(context.Background(), "github.com", 443); got != nil {
		t.Fatalf("Args with kill switch off = %v, want nil", got)
	}
	if ch := offCache.RequestRefresh("github.com"); ch != nil {
		t.Fatalf("RequestRefresh with kill switch off = %v, want nil", ch)
	}

	onCache, _ := testCache(t, lookup, nil, nil)
	for _, host := range []string{"", "unknown", "10.1.2.3", "2001:db8::1"} {
		if got := onCache.Args(context.Background(), host, 443); got != nil {
			t.Fatalf("Args(%q) = %v, want nil", host, got)
		}
	}
	if n := lookup.calls.Load(); n != 0 {
		t.Fatalf("lookups = %d, want 0: no gate may reach the resolver", n)
	}
}

// TestArgsArePerHost: two providers keep separate entries and pins.
func TestArgsArePerHost(t *testing.T) {
	byHost := map[string][]net.IP{
		"github.com": {net.ParseIP("10.0.0.1")},
		"gitlab.com": {net.ParseIP("10.0.0.2")},
	}
	lookup := LookupFunc(func(_ context.Context, host string) ([]net.IP, error) {
		ips, ok := byHost[host]
		if !ok {
			return nil, fmt.Errorf("unexpected host %q", host)
		}
		return ips, nil
	})
	cache, _ := testCache(t, nil, nil, func(_ *Settings, o *Options) { o.Lookup = lookup })

	if got, want := cache.Args(context.Background(), "github.com", 443), []string{"-c", "http.curloptResolve=github.com:443:10.0.0.1"}; !slices.Equal(got, want) {
		t.Fatalf("github Args = %v, want %v", got, want)
	}
	if got, want := cache.Args(context.Background(), "gitlab.com", 8443), []string{"-c", "http.curloptResolve=gitlab.com:8443:10.0.0.2"}; !slices.Equal(got, want) {
		t.Fatalf("gitlab Args = %v, want %v", got, want)
	}
}
