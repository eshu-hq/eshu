// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"

	sdkcollector "github.com/eshu-hq/eshu/sdk/go/collector"
)

const (
	// defaultOCIRuntime is the container runtime CLI used when a component
	// activation does not name one. docker and podman share the run/--rm/-i
	// surface this adapter depends on.
	defaultOCIRuntime = "docker"
	// defaultOCINetwork isolates the extension container from all networking by
	// default; an operator must opt a component into egress explicitly.
	defaultOCINetwork = "none"
	// defaultOCIUser runs the extension as a fixed non-root uid:gid.
	defaultOCIUser = "65532:65532"
)

// ociDigestPattern requires a digest-pinned image reference. The adapter never
// launches a floating tag, so a component can only run the exact artifact its
// verified manifest declared.
var ociDigestPattern = regexp.MustCompile(`@sha256:[A-Fa-f0-9]{64}$`)

// ociExec runs a container-runtime command with bounded stdio. It is a seam so
// argv construction and stream bounding can be unit-tested without a real
// container runtime.
type ociExec func(ctx context.Context, name string, args []string, stdin []byte, stdout, stderr io.Writer) error

// OCIRunner launches a digest-pinned OCI artifact as a collector SDK process
// over JSON stdin/stdout. It speaks the same SDK contract as ProcessRunner but
// runs the extension inside an isolated container: no network by default, a
// read-only root filesystem, a non-root user, all capabilities dropped, and no
// new privileges. The container receives no Eshu Postgres, graph, reducer, API,
// MCP, or workflow-control handles — only the bounded request on stdin.
type OCIRunner struct {
	// Runtime is the container runtime CLI (docker or podman); empty defaults
	// to docker.
	Runtime string
	// ImageRef is the digest-pinned artifact (repo@sha256:<64 hex>). It must
	// come from the component's verified manifest artifact, not operator config.
	ImageRef string
	// Network names the container network; empty defaults to "none".
	Network string
	// User is the container uid:gid; empty defaults to a non-root user.
	User string
	// Env lists "KEY=value" pairs passed to the container via --env. The host
	// environment is never forwarded wholesale.
	Env []string
	// ExtraArgs are additional runtime flags inserted before the image
	// reference (for operator-approved mounts or limits).
	ExtraArgs        []string
	StdoutLimitBytes int64
	StderrLimitBytes int64
	// exec is the command seam; nil uses the real container runtime.
	exec ociExec
}

// RunCollector launches the digest-pinned artifact and decodes one SDK result.
func (r OCIRunner) RunCollector(ctx context.Context, request Request) (sdkcollector.Result, error) {
	args, err := r.commandArgs()
	if err != nil {
		return sdkcollector.Result{}, err
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return sdkcollector.Result{}, fmt.Errorf("encode extension request: %w", err)
	}

	stdout := newLimitedBuffer(effectiveLimit(r.StdoutLimitBytes, defaultStdoutLimitBytes))
	stderr := newLimitedBuffer(effectiveLimit(r.StderrLimitBytes, defaultStderrLimitBytes))

	run := r.exec
	if run == nil {
		run = defaultOCIExec
	}
	if err := run(ctx, r.runtime(), args, payload, stdout, stderr); err != nil {
		if ctx.Err() != nil {
			return sdkcollector.Result{}, ctx.Err()
		}
		return sdkcollector.Result{}, fmt.Errorf(
			"extension oci adapter failed: %w (stderr_bytes=%d stderr_truncated=%t)",
			err,
			stderr.Len(),
			stderr.Truncated(),
		)
	}
	if stdout.Truncated() {
		return sdkcollector.Result{}, fmt.Errorf(
			"extension stdout limit exceeded: limit_bytes=%d",
			stdout.Limit(),
		)
	}
	return decodeSDKResult(stdout.Bytes())
}

func (r OCIRunner) runtime() string {
	if runtime := strings.TrimSpace(r.Runtime); runtime != "" {
		return runtime
	}
	return defaultOCIRuntime
}

// commandArgs builds the digest-pinned, isolated `run` argument vector. It
// rejects any image reference that is not digest-pinned so a component can
// never be launched from a mutable tag.
func (r OCIRunner) commandArgs() ([]string, error) {
	image := strings.TrimSpace(r.ImageRef)
	if image == "" {
		return nil, errors.New("oci image reference is required")
	}
	if !ociDigestPattern.MatchString(image) {
		return nil, fmt.Errorf(
			"oci image reference must be digest-pinned (repo@sha256:<64 hex>): %q",
			image,
		)
	}
	network := strings.TrimSpace(r.Network)
	if network == "" {
		network = defaultOCINetwork
	}
	user := strings.TrimSpace(r.User)
	if user == "" {
		user = defaultOCIUser
	}
	args := []string{
		"run", "--rm", "--interactive",
		"--network", network,
		"--read-only",
		"--user", user,
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges",
	}
	for _, env := range r.Env {
		env = strings.TrimSpace(env)
		if env != "" {
			args = append(args, "--env", env)
		}
	}
	args = append(args, r.ExtraArgs...)
	args = append(args, image)
	return args, nil
}

func defaultOCIExec(ctx context.Context, name string, args []string, stdin []byte, stdout, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = bytes.NewReader(stdin)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

// decodeSDKResult decodes exactly one SDK result from a bounded stdout buffer,
// rejecting unknown fields and trailing JSON so the host never accepts a
// partial or padded extension response.
func decodeSDKResult(payload []byte) (sdkcollector.Result, error) {
	var result sdkcollector.Result
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return sdkcollector.Result{}, fmt.Errorf("decode extension result: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return sdkcollector.Result{}, errors.New("decode extension result: trailing JSON value")
		}
		return sdkcollector.Result{}, fmt.Errorf("decode extension result trailer: %w", err)
	}
	return result, nil
}
