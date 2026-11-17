// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build ignore

// Command capability-stub-satellite serves the in-tree OpenAPI assembly as
// the offline double for the satellite document (Refs eshu-hq/eshu#9669,
// row 7). Gates that verify the surface inventory need the satellite's
// served OpenAPI document, but commit-time hooks and the schema-drift CI job
// have no live satellite: this double serves the same in-tree assembly the
// satellite serves (query/openapi, the fragments the satellite assembles)
// from 127.0.0.1 behind the same RequireAuth-shaped gate the satellite
// enforces — GET /api/v0/openapi.json answers only with the expected
// admin/all-scope bearer token and 401s otherwise — so the fetch path the
// capability-inventory CLI exercises is identical with or without a live
// satellite. Anything else 404s. See scripts/lib/capability-stub-satellite.sh,
// the only caller, and the test double it mirrors in main_test.go
// (newFullSurfaceSatellite).
//
// Location: this file must live under go/ — it imports go/internal packages,
// which the Go internal rule forbids from outside the module tree — and
// carries `//go:build ignore` so normal module builds, tests, vet, and lint
// never compile it; only an explicit `go run` of this file (which the wrapper
// performs) does. It adds no command surface: enumerateCommandBinaries lists
// go/cmd directories, not files in them.
package main

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/openapi"
)

// satelliteOpenAPIPath is the only path this double serves, mirroring the
// satellite route the capability inventory fetches.
const satelliteOpenAPIPath = "/api/v0/openapi.json"

// stubServerTimeout bounds one stub request/response exchange. The double
// serves a single in-memory document to one local gate process, so a short
// bound keeps a wedged reader from holding the gate.
const stubServerTimeout = 10 * time.Second

// stubSafetyTimeout bounds the lifetime of an orphaned stub. The wrapper
// kills the server after its child command exits; the timeout only fires
// when that cleanup cannot run.
const stubSafetyTimeout = 10 * time.Minute

func main() {
	token := flag.String("token", "", "admin/all-scope bearer token the stub expects (required)")
	flag.Parse()
	if *token == "" {
		fmt.Fprintln(os.Stderr, "capability-stub-satellite: -token is required")
		os.Exit(2)
	}
	document := []byte(openapi.Spec())
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintln(os.Stderr, "capability-stub-satellite: listen failed")
		os.Exit(1)
	}
	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			if request.URL.Path != satelliteOpenAPIPath {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			// RequireAuth-shaped gate: the admin/all-scope bearer only,
			// anything else 401s. The token value never leaves the
			// comparison — it is not logged or echoed.
			if request.Header.Get("Authorization") != "Bearer "+*token {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(document)
		}),
		ReadHeaderTimeout: stubServerTimeout,
		ReadTimeout:       stubServerTimeout,
		WriteTimeout:      stubServerTimeout,
		IdleTimeout:       stubServerTimeout,
	}
	// The listen already succeeded, so printing the URL first cannot race a
	// wrapper that dials as soon as it reads this line: Serve only accepts on
	// the bound socket.
	fmt.Printf("http://%s\n", listener.Addr().String())
	go func() {
		_ = server.Serve(listener)
	}()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	select {
	case <-signals:
	case <-time.After(stubSafetyTimeout):
	}
	_ = server.Close()
}
