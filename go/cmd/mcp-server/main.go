// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/eshu-hq/eshu/go/internal/buildinfo"
	"github.com/eshu-hq/eshu/go/internal/mcp"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func main() {
	if handled, err := printMCPServerVersionFlag(os.Args[1:], os.Stdout); handled {
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	bootstrap, err := telemetry.NewBootstrap("mcp-server")
	if err != nil {
		fallback := slog.New(slog.NewJSONHandler(os.Stderr, nil))
		fallback.Error("mcp bootstrap failed", "event_name", "runtime.startup.failed", "error", err)
		os.Exit(1)
	}
	logger := newLogger(bootstrap, os.Stderr)
	providers, err := telemetry.NewProviders(ctx, bootstrap)
	if err != nil {
		logger.Error("mcp telemetry providers failed", telemetry.EventAttr("runtime.startup.failed"), "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := providers.Shutdown(context.Background()); err != nil {
			logger.Error("telemetry shutdown failed", telemetry.EventAttr("runtime.shutdown.failed"), "error", err)
		}
	}()

	pprofSrv, err := runtimecfg.NewPprofServer(os.Getenv)
	if err != nil {
		logger.Error("pprof server failed", telemetry.EventAttr("runtime.startup.failed"), "error", err)
		os.Exit(1)
	}
	if pprofSrv != nil {
		if err := pprofSrv.Start(ctx); err != nil {
			logger.Error("pprof server start failed", telemetry.EventAttr("runtime.startup.failed"), "error", err)
			os.Exit(1)
		}
		logger.Info("pprof server listening", telemetry.EventAttr("runtime.server.listening"), "addr", pprofSrv.Addr())
		defer func() {
			_ = pprofSrv.Stop(context.Background())
		}()
	}

	transport := strings.ToLower(strings.TrimSpace(os.Getenv("ESHU_MCP_TRANSPORT")))
	if transport == "" {
		transport = "http"
	}

	queryMux, adminMux, cleanup, authWiring, err := wireAPI(ctx, os.Getenv, logger, providers.PrometheusHandler)
	if err != nil {
		logger.Error("wire api failed", telemetry.EventAttr("runtime.startup.failed"), "error", err)
		os.Exit(1)
	}
	defer cleanup()

	// No silent open mode over HTTP (issue #5168): refuse to start
	// ESHU_MCP_TRANSPORT=http with no resolvable credential source, unless
	// the operator explicitly opted into the dev/loopback escape hatch.
	// stdio is never gated -- requireMCPHTTPCredentialSource is a no-op for
	// any transport other than "http".
	allowUnauthenticated := runtimecfg.IsTruthy(os.Getenv("ESHU_MCP_ALLOW_UNAUTHENTICATED"))
	if err := requireMCPHTTPCredentialSource(transport, authWiring, allowUnauthenticated); err != nil {
		logger.Error("mcp server refused to start", telemetry.EventAttr("runtime.startup.failed"), "error", err)
		os.Exit(1)
	}
	if transport == "http" && !authWiring.credentialSourceConfigured && allowUnauthenticated {
		logger.Warn(
			"ESHU_MCP_ALLOW_UNAUTHENTICATED=true: MCP HTTP transport starting with no credential source -- "+
				"every initialize/tools/list/tools/call/ping request and SSE session is unauthenticated. "+
				"Dev/loopback use only; never expose this port publicly with the escape hatch set.",
			telemetry.EventAttr("runtime.startup.warning"),
		)
	}

	// The MCP server's internal httpMux authenticates GET /sse and
	// POST /mcp/message with authWiring.transportAuth -- the SAME credential
	// chain protecting /api/ and tools/call's internal dispatch (issue
	// #5168). The query API routes mounted under /api/ are protected by the
	// query mux itself (queryMux is already an authed handler).
	server := mcp.NewServer(queryMux, logger, mcp.WithTransportAuth(authWiring.transportAuth))

	switch transport {
	case "stdio":
		if err := server.Run(ctx); err != nil {
			logger.Error("mcp server exited", "transport", "stdio", "error", err)
			os.Exit(1)
		}
	case "http":
		addr := os.Getenv("ESHU_MCP_ADDR")
		if addr == "" {
			addr = ":8080"
		}
		if err := server.RunHTTP(ctx, addr, adminMux); err != nil {
			logger.Error("mcp server exited", "transport", "http", "error", err)
			os.Exit(1)
		}
	default:
		logger.Error("unknown transport", "ESHU_MCP_TRANSPORT", transport)
		os.Exit(1)
	}
}

func printMCPServerVersionFlag(args []string, stdout io.Writer) (bool, error) {
	return buildinfo.PrintVersionFlag(args, stdout, "eshu-mcp-server")
}

func newLogger(bootstrap telemetry.Bootstrap, writer io.Writer) *slog.Logger {
	return telemetry.NewLoggerWithWriter(bootstrap, "mcp-server", "mcp-server", writer)
}
