// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package resolve

import (
	"context"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Refresh-request reasons, the closed value set of the reason label on
// eshu_dp_git_resolve_cache_refresh_requests_total.
const (
	ReasonInitial    = "initial"
	ReasonFailure    = "failure"
	ReasonPeriodic   = "periodic"
	ReasonSuppressed = "suppressed"
)

// Fallback reasons, the closed value set of the reason label on
// eshu_dp_git_resolve_cache_fallback_total.
const (
	FallbackCacheEmpty = "cache_empty"
	FallbackProxySet   = "proxy_set"
)

// Lookup results, the closed value set of the result label on
// eshu_dp_git_resolve_cache_lookups_total and the duration histogram.
const (
	ResultSuccess = "success"
	ResultError   = "error"
	ResultEmpty   = "empty"
)

// LookupFunc resolves one host to every A and AAAA address, in resolver
// order. The nil value uses net.DefaultResolver with network "ip".
type LookupFunc func(ctx context.Context, host string) ([]net.IP, error)

// Options are the non-environment seams of the cache: the resolver, the
// clock, the environment the proxy gate reads, and the refresh tick. A nil
// field takes its production default.
type Options struct {
	// Lookup resolves a host. Nil means the Go default resolver.
	Lookup LookupFunc
	// Now reads the clock the minimum gap is measured on. Nil means
	// time.Now.
	Now func() time.Time
	// Getenv reads the proxy settings. Nil means os.Getenv.
	Getenv func(string) string
	// Tick drives the background refresh. Nil means a ticker at the
	// configured refresh interval.
	Tick <-chan time.Time
}

type lookupRequest struct {
	host   string
	reason string
}

// Cache is the per-host address cache. The zero value is not usable; build
// one with New. A Cache is safe for concurrent use: every lookup runs on
// its single receiver goroutine, and the address map is read and written
// under a mutex so a command never waits on the receiver to read a hit.
//
// Conflict domain: one host's address set. Single-flight state (the
// in-flight lookup and the last-lookup instant) is per host; lookups of
// different hosts queue on the one receiver, each bounded by its own
// deadline, so unrelated hosts delay each other by at most one lookup
// deadline and never corrupt each other.
type Cache struct {
	settings Settings
	lookup   LookupFunc
	now      func() time.Time
	getenv   func(string) string
	tick     <-chan time.Time

	instruments atomic.Pointer[telemetry.Instruments]
	logger      atomic.Pointer[slog.Logger]

	ctx    context.Context
	cancel context.CancelFunc
	start  sync.Once
	reqCh  chan lookupRequest

	mu         sync.Mutex
	addrs      map[string][]string
	inflight   map[string]chan struct{}
	lastLookup map[string]time.Time
	loggedOnce map[string]bool
	closed     bool
}

// New builds a cache. The receiver goroutine starts lazily on the first
// request, so a process that never runs a network git command never starts
// one. Close stops it.
func New(settings Settings, options Options) *Cache {
	lookup := options.Lookup
	if lookup == nil {
		lookup = func(ctx context.Context, host string) ([]net.IP, error) {
			return net.DefaultResolver.LookupIP(ctx, "ip", host)
		}
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	getenv := options.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Cache{
		settings:   settings,
		lookup:     lookup,
		now:        now,
		getenv:     getenv,
		tick:       options.Tick,
		ctx:        ctx,
		cancel:     cancel,
		reqCh:      make(chan lookupRequest, 64),
		addrs:      make(map[string][]string),
		inflight:   make(map[string]chan struct{}),
		lastLookup: make(map[string]time.Time),
		loggedOnce: make(map[string]bool),
	}
}

// Bind attaches the telemetry instruments and the logger of the current
// runtime. They arrive per sweep in the collector wiring, after the cache
// was built at config load, so they are bound late and swapped atomically.
// Either may be nil, which keeps the previous binding for that half.
func (c *Cache) Bind(instruments *telemetry.Instruments, logger *slog.Logger) {
	if c == nil {
		return
	}
	if instruments != nil {
		c.instruments.Store(instruments)
	}
	if logger != nil {
		c.logger.Store(logger)
	}
}

// Enabled reports whether the kill switch lets the cache pin addresses.
func (c *Cache) Enabled() bool { return c != nil && c.settings.Enabled }

// Close stops the receiver goroutine. A closed cache pins nothing and
// starts no lookup.
func (c *Cache) Close() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	c.cancel()
}

// normalizeHost returns the cache key of a provider host, or "" when the
// host must never be pinned: empty, the metrics "unknown" placeholder, or
// an IP literal (which curl needs no lookup for).
func normalizeHost(host string) string {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if host == "" || host == "unknown" || net.ParseIP(host) != nil {
		return ""
	}
	return host
}

// Args returns the [-c, http.curloptResolve=<host>:<port>:<ip>,...] pair
// that pins host's addresses for one network git command, or nil to run
// the command exactly as without the cache. On a miss it starts (or joins)
// the one lookup for the host and waits at most the lookup deadline, then
// falls back unpinned, counted reason=cache_empty. A proxy setting falls
// back at once, counted reason=proxy_set.
func (c *Cache) Args(ctx context.Context, host string, port int) []string {
	if !c.Enabled() || port <= 0 {
		return nil
	}
	host = normalizeHost(host)
	if host == "" {
		return nil
	}
	if proxyIsSet(c.getenv) {
		c.recordFallback(ctx, host, FallbackProxySet)
		c.logOnce(ctx, host, FallbackProxySet, "git resolve cache pin skipped: a proxy is configured")
		return nil
	}
	if args := c.cachedArgs(host, port); args != nil {
		return args
	}
	if wait := c.request(host, ReasonInitial); wait != nil {
		timer := time.NewTimer(c.settings.lookupTimeout())
		select {
		case <-wait:
		case <-ctx.Done():
		case <-timer.C:
		}
		timer.Stop()
	}
	if args := c.cachedArgs(host, port); args != nil {
		return args
	}
	c.recordFallback(ctx, host, FallbackCacheEmpty)
	c.logOnce(ctx, host, FallbackCacheEmpty, "git resolve cache pin skipped: no addresses cached yet")
	return nil
}

func (c *Cache) cachedArgs(host string, port int) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	addrs, ok := c.addrs[host]
	if !ok || len(addrs) == 0 {
		return nil
	}
	return []string{"-c", "http.curloptResolve=" + host + ":" + strconv.Itoa(port) + ":" + strings.Join(addrs, ",")}
}

// RequestRefresh asks for a fresh lookup of host after a resolve or
// connect failure. It never blocks. The returned channel closes when that
// lookup finishes; it is the same channel for every caller that joins an
// in-flight lookup, nil when the cache is off, and already closed when the
// minimum gap suppresses the request.
func (c *Cache) RequestRefresh(host string) <-chan struct{} {
	if !c.Enabled() {
		return nil
	}
	host = normalizeHost(host)
	if host == "" {
		return nil
	}
	return c.request(host, ReasonFailure)
}

// request is the one entry into the single-flight machinery. It joins an
// in-flight lookup, suppresses a request inside the minimum gap, or queues
// exactly one lookup on the receiver.
func (c *Cache) request(host, reason string) <-chan struct{} {
	c.ensureStarted()
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	if wait, ok := c.inflight[host]; ok {
		c.mu.Unlock()
		return wait
	}
	if last, ok := c.lastLookup[host]; ok && c.now().Sub(last) < c.settings.minGap() {
		done := make(chan struct{})
		close(done)
		c.mu.Unlock()
		c.recordRefreshRequest(c.ctx, host, ReasonSuppressed)
		return done
	}
	wait := make(chan struct{})
	c.inflight[host] = wait
	c.lastLookup[host] = c.now()
	c.mu.Unlock()
	c.recordRefreshRequest(c.ctx, host, reason)
	select {
	case c.reqCh <- lookupRequest{host: host, reason: reason}:
	default:
		// The receiver's queue is full (64 hosts ahead): give the slot
		// back rather than block a sync worker on the send.
		c.mu.Lock()
		delete(c.inflight, host)
		c.mu.Unlock()
		close(wait)
		c.recordRefreshRequest(c.ctx, host, ReasonSuppressed)
	}
	return wait
}

func (c *Cache) ensureStarted() {
	c.start.Do(func() { go c.run() })
}

// run is the single receiver: every lookup and every periodic sweep
// happens here, one at a time.
func (c *Cache) run() {
	tick := c.tick
	var ticker *time.Ticker
	if tick == nil {
		ticker = time.NewTicker(c.settings.refreshInterval())
		defer ticker.Stop()
		tick = ticker.C
	}
	for {
		select {
		case <-c.ctx.Done():
			return
		case req := <-c.reqCh:
			c.doLookup(req)
		case <-tick:
			c.refreshKnownHosts()
		}
	}
}

func (c *Cache) refreshKnownHosts() {
	c.mu.Lock()
	hosts := make([]string, 0, len(c.lastLookup))
	for host := range c.lastLookup {
		hosts = append(hosts, host)
	}
	c.mu.Unlock()
	for _, host := range hosts {
		c.request(host, ReasonPeriodic)
	}
}

// doLookup runs one lookup on the receiver, stores the answer only when it
// is a non-empty success (a failed or empty refresh keeps the last good
// set), then releases every waiter.
func (c *Cache) doLookup(req lookupRequest) {
	ctx, span := otel.Tracer("github.com/eshu-hq/eshu/go/internal/collector/repo/git/resolve").Start(c.ctx, "git.resolve_cache.lookup", trace.WithAttributes(
		attribute.String(telemetry.MetricDimensionHost, req.host),
		attribute.String(telemetry.MetricDimensionReason, req.reason),
	))
	defer span.End()
	lookupCtx, cancel := context.WithTimeout(ctx, c.settings.lookupTimeout())
	started := time.Now()
	ips, err := c.lookup(lookupCtx, req.host)
	cancel()
	elapsed := time.Since(started)

	result := ResultSuccess
	var stored []string
	switch {
	case err != nil:
		result = ResultError
	default:
		for _, ip := range ips {
			if ip != nil {
				stored = append(stored, ip.String())
			}
		}
		if len(stored) == 0 {
			result = ResultEmpty
		}
	}

	c.mu.Lock()
	if result == ResultSuccess {
		c.addrs[req.host] = stored
	}
	wait := c.inflight[req.host]
	delete(c.inflight, req.host)
	c.mu.Unlock()

	c.recordLookup(ctx, req.host, result, elapsed)
	if result == ResultSuccess {
		c.logLookup(ctx, slog.LevelInfo, req, result, len(stored), elapsed, nil)
	} else {
		c.logLookup(ctx, slog.LevelWarn, req, result, 0, elapsed, err)
	}
	if wait != nil {
		close(wait)
	}
}

func (c *Cache) logLookup(ctx context.Context, level slog.Level, req lookupRequest, result string, addresses int, elapsed time.Duration, err error) {
	logger := c.logger.Load()
	if logger == nil {
		return
	}
	attrs := []any{
		slog.String("host", req.host),
		slog.String("reason", req.reason),
		slog.String("result", result),
		slog.Int("addresses", addresses),
		slog.Float64("elapsed_seconds", elapsed.Seconds()),
	}
	if err != nil {
		attrs = append(attrs, slog.String("error", err.Error()))
	}
	logger.Log(ctx, level, "git resolve cache lookup", attrs...)
}

// logOnce logs a fallback explanation once per host and reason, so a fleet
// of commands against one uncached host writes one line.
func (c *Cache) logOnce(ctx context.Context, host, reason, message string) {
	c.mu.Lock()
	key := host + "|" + reason
	seen := c.loggedOnce[key]
	c.loggedOnce[key] = true
	c.mu.Unlock()
	if seen {
		return
	}
	if logger := c.logger.Load(); logger != nil {
		logger.WarnContext(ctx, message, slog.String("host", host), slog.String("reason", reason))
	}
}
