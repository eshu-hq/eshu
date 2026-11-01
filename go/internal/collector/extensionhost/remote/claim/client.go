// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package claim

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	workflowv1 "github.com/eshu-hq/eshu-workflow/workflowv1"
)

// DefaultMaxResponseBytes bounds one protocol response body. Claim grants
// are small JSON documents; anything past the bound fails closed instead of
// buffering an untrusted server's output without limit.
const DefaultMaxResponseBytes = 1 << 20

// CodeResponseTooLarge is the client-side code for a response past
// MaxResponseBytes. It is not a protocol code: the server never sends it.
const CodeResponseTooLarge = "response_too_large"

// maxErrorMessage bounds one surfaced server message. Error text carries
// identifiers and short reasons only, never bodies or credentials.
const maxErrorMessage = 512

// Config builds a Client. BaseURL, ProducerID, ProducerVersion, Scopes, and
// ReadGrants are required; everything else has a safe default. The producer
// identity is fixed here at construction and never taken from the wire, so
// a tampered response cannot reassign which holder this client proves.
type Config struct {
	// BaseURL is the workflow service origin, for example
	// "https://workflow.internal". Only http and https are accepted.
	BaseURL string
	// HTTPClient carries the transport. Nil uses a default client with a
	// 30-second timeout; callers needing mTLS or proxies supply their own.
	HTTPClient *http.Client
	// AuthToken is the optional bearer for the workflow service. It travels
	// as an Authorization header only, is scrubbed from every surfaced
	// error, and never appears in a request body or log.
	AuthToken string
	// ProducerID and ProducerVersion name the granted component
	// (manifest metadata.id and metadata.version). Every grant decision
	// evaluates under exactly this identity.
	ProducerID      string
	ProducerVersion string
	// Scopes are the manifest-declared collector kinds. A grant whose scope
	// is not declared here never authorizes this holder.
	Scopes []string
	// ReadGrants re-reads the live producer-grant set. The prototype backs
	// it with RegistryGrantReader; the cutover binds the core grant plane
	// when the protocol declares one (no grant plane exists in the merged
	// table, so no interim endpoint is invented here).
	ReadGrants GrantReader
	// Observer receives one grant decision per evaluated core-owned kind.
	// Nil disables observation; observation never changes an outcome.
	Observer Observer
	// Clock supplies now for grant-expiry evaluation. Nil reads the wall
	// clock (UTC) on every call.
	Clock func() time.Time
	// MaxResponseBytes bounds one response body. Values <= 0 select
	// DefaultMaxResponseBytes.
	MaxResponseBytes int64
}

// Client is the remote claimed-service seam: claim, commit, and live-grant
// re-read over the merged workflow.v1 table, holding no core implementation
// handle. A Client is safe for concurrent use.
type Client struct {
	baseURL    string
	http       *http.Client
	token      string
	producerID string
	version    string
	scopes     []string
	grants     GrantReader
	observer   Observer
	clock      func() time.Time
	maxBytes   int64
}

// NewClient validates the seam configuration fail-closed: a client that
// cannot name its holder, its scopes, or its grant source is never built.
func NewClient(cfg Config) (*Client, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	endpoint, err := url.ParseRequestURI(base)
	if err != nil {
		return nil, fmt.Errorf("claim: base URL is required: %w", err)
	}
	if endpoint.Scheme != "http" && endpoint.Scheme != "https" {
		return nil, fmt.Errorf("claim: base URL scheme %q must be http or https", endpoint.Scheme)
	}
	if strings.TrimSpace(cfg.ProducerID) == "" {
		return nil, errors.New("claim: producer id is required")
	}
	if strings.TrimSpace(cfg.ProducerVersion) == "" {
		return nil, errors.New("claim: producer version is required")
	}
	if len(cfg.Scopes) == 0 {
		return nil, errors.New("claim: at least one declared scope is required")
	}
	if cfg.ReadGrants == nil {
		return nil, errors.New("claim: grant reader is required")
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	clock := cfg.Clock
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	maxBytes := cfg.MaxResponseBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxResponseBytes
	}
	return &Client{
		baseURL:    base,
		http:       httpClient,
		token:      cfg.AuthToken,
		producerID: strings.TrimSpace(cfg.ProducerID),
		version:    strings.TrimSpace(cfg.ProducerVersion),
		scopes:     append([]string(nil), cfg.Scopes...),
		grants:     cfg.ReadGrants,
		observer:   cfg.Observer,
		clock:      clock,
		maxBytes:   maxBytes,
	}, nil
}

// ClaimNext asks the service for the next eligible work item. It returns
// found=false with a nil error on the empty-queue signal (404 not_found:
// the merged table defines no other empty response), the issued grant
// otherwise, or a *PacedError when the grant is paced. The request is
// validated locally first: malformed input never touches the wire.
func (c *Client) ClaimNext(
	ctx context.Context,
	selector workflowv1.ClaimSelector,
	observedAt time.Time,
	leaseTTL time.Duration,
) (workflowv1.ClaimedWorkItem, bool, error) {
	req := workflowv1.ClaimNextRequest{Selector: selector, ObservedAt: observedAt, LeaseTTL: leaseTTL}
	if err := req.Validate(); err != nil {
		return workflowv1.ClaimedWorkItem{}, false, fmt.Errorf("claim %s: %w", workflowv1.OpClaimNext, err)
	}
	var resp workflowv1.ClaimNextResponse
	status, envelope, body, err := c.roundTrip(ctx, workflowv1.OpClaimNext, req)
	if err != nil {
		return workflowv1.ClaimedWorkItem{}, false, err
	}
	switch {
	case status == http.StatusNotFound && envelopeCode(envelope) == workflowv1.CodeNotFound:
		return workflowv1.ClaimedWorkItem{}, false, nil
	case status == http.StatusTooManyRequests || envelopeCode(envelope) == workflowv1.CodePaced:
		return workflowv1.ClaimedWorkItem{}, false, c.pacedError(envelope)
	case status < 200 || status > 299:
		return workflowv1.ClaimedWorkItem{}, false, c.callError(workflowv1.OpClaimNext, status, envelope)
	}
	if err := decodeStrict(body, &resp); err != nil {
		return workflowv1.ClaimedWorkItem{}, false, c.callErrorText(workflowv1.OpClaimNext, "invalid", err.Error())
	}
	if err := resp.Validate(); err != nil {
		return workflowv1.ClaimedWorkItem{}, false, c.callErrorText(workflowv1.OpClaimNext, "invalid", err.Error())
	}
	if !resp.Issued {
		return workflowv1.ClaimedWorkItem{}, false, c.deferralError(resp.Deferral)
	}
	if err := checkGrantSelector(selector, resp.Grant); err != nil {
		return workflowv1.ClaimedWorkItem{}, false, c.callErrorText(workflowv1.OpClaimNext, "invalid", err.Error())
	}
	return resp.Grant, true, nil
}

// checkGrantSelector fails closed when an issued grant is not the claim this
// client asked for: another claim id, owner, collector kind, or instance.
// Acting on someone else's grant would heartbeat or commit work this holder
// never claimed. The error names the field only, never a value.
func checkGrantSelector(selector workflowv1.ClaimSelector, grant workflowv1.ClaimedWorkItem) error {
	switch {
	case grant.Claim.ClaimID != selector.ClaimID:
		return errors.New("granted claim does not match the requested selector: claim_id")
	case grant.Claim.OwnerID != selector.OwnerID:
		return errors.New("granted claim does not match the requested selector: owner_id")
	case grant.WorkItem.CollectorKind != selector.CollectorKind:
		return errors.New("granted claim does not match the requested selector: collector_kind")
	case grant.WorkItem.CollectorInstanceID != selector.CollectorInstanceID:
		return errors.New("granted claim does not match the requested selector: collector_instance_id")
	}
	return nil
}

// Heartbeat renews the lease on a held claim epoch.
func (c *Client) Heartbeat(ctx context.Context, mutation workflowv1.ClaimMutation) error {
	return c.mutate(ctx, workflowv1.OpHeartbeatClaim, mutation)
}

// Complete finishes a held claim after its generation committed.
func (c *Client) Complete(ctx context.Context, mutation workflowv1.ClaimMutation) error {
	return c.mutate(ctx, workflowv1.OpCompleteClaim, mutation)
}

// Release returns a held claim unworked.
func (c *Client) Release(ctx context.Context, mutation workflowv1.ClaimMutation) error {
	return c.mutate(ctx, workflowv1.OpReleaseClaim, mutation)
}

// FailRetryable records a retryable claim failure.
func (c *Client) FailRetryable(ctx context.Context, mutation workflowv1.ClaimMutation) error {
	return c.mutate(ctx, workflowv1.OpFailClaimRetryable, mutation)
}

// FailTerminal records a terminal claim failure.
func (c *Client) FailTerminal(ctx context.Context, mutation workflowv1.ClaimMutation) error {
	return c.mutate(ctx, workflowv1.OpFailClaimTerminal, mutation)
}

// ackResponse is the acknowledgement the pinned eshu-workflow server writes
// for every lifecycle operation (server.serveMutation answers ack{OK: true},
// encoded as {"ok":true} plus a newline).
type ackResponse struct {
	OK bool `json:"ok"`
}

// mutate sends one fenced lifecycle mutation. A 2xx counts as success only
// when the body is exactly the {"ok":true} acknowledgement: an empty body,
// ok=false, an unknown field, or trailing data fails closed, because the
// server may or may not have applied the mutation and the caller must not
// proceed on an unverified answer.
func (c *Client) mutate(ctx context.Context, op workflowv1.Operation, mutation workflowv1.ClaimMutation) error {
	if err := mutation.Validate(); err != nil {
		return fmt.Errorf("claim %s: %w", op, err)
	}
	status, envelope, body, err := c.roundTrip(ctx, op, mutation)
	if err != nil {
		return err
	}
	if status < 200 || status > 299 {
		return c.callError(op, status, envelope)
	}
	var ack ackResponse
	if err := decodeStrict(body, &ack); err != nil {
		return c.callErrorText(op, "invalid", err.Error())
	}
	if !ack.OK {
		return c.callErrorText(op, "invalid", "acknowledgement did not report ok")
	}
	return nil
}

// PinForCommit asks core to pin a claim server-side for a fact commit: the
// protocol replacement for the in-transaction heartbeat the in-tree path
// relied on. An unpinned pin surfaces as a stale-fence error, never a
// commit: the caller must re-claim.
func (c *Client) PinForCommit(
	ctx context.Context,
	req workflowv1.CommitPinRequest,
) (workflowv1.CommitPinResponse, error) {
	if err := req.Validate(); err != nil {
		return workflowv1.CommitPinResponse{}, fmt.Errorf("claim %s: %w", workflowv1.OpValidateAndPinClaimForCommit, err)
	}
	var resp workflowv1.CommitPinResponse
	status, envelope, body, err := c.roundTrip(ctx, workflowv1.OpValidateAndPinClaimForCommit, req)
	if err != nil {
		return workflowv1.CommitPinResponse{}, err
	}
	if status < 200 || status > 299 {
		return workflowv1.CommitPinResponse{}, c.callError(workflowv1.OpValidateAndPinClaimForCommit, status, envelope)
	}
	if err := decodeStrict(body, &resp); err != nil {
		return workflowv1.CommitPinResponse{}, c.callErrorText(workflowv1.OpValidateAndPinClaimForCommit, "invalid", err.Error())
	}
	if err := resp.Validate(); err != nil {
		return workflowv1.CommitPinResponse{}, c.pinRefusedError(err)
	}
	return resp, nil
}

// pinRefusedError renders a response that decoded but failed validation as
// the stale-fence signal: the caller must re-claim, never commit.
func (c *Client) pinRefusedError(cause error) *CallError {
	return &CallError{
		Op:      workflowv1.OpValidateAndPinClaimForCommit,
		Code:    workflowv1.CodeStaleFence,
		Message: boundMessage(c.scrub("commit pin refused: " + cause.Error())),
	}
}

// roundTrip sends one protocol call and returns the status, the error
// envelope (present on failures), and the bounded body.
func (c *Client) roundTrip(
	ctx context.Context,
	op workflowv1.Operation,
	request any,
) (int, workflowv1.ErrorEnvelope, []byte, error) {
	if err := ctx.Err(); err != nil {
		return 0, workflowv1.ErrorEnvelope{}, nil, fmt.Errorf("claim %s: %w", op, err)
	}
	route, ok := workflowv1.RouteFor(op)
	if !ok {
		return 0, workflowv1.ErrorEnvelope{}, nil, fmt.Errorf("claim %s: unknown protocol operation", op)
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return 0, workflowv1.ErrorEnvelope{}, nil, fmt.Errorf("claim %s: %w", op, err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, route.Method, c.baseURL+route.Path, bytes.NewReader(raw))
	if err != nil {
		return 0, workflowv1.ErrorEnvelope{}, nil, fmt.Errorf("claim %s: %w", op, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set(workflowv1.VersionHeader, strconv.Itoa(workflowv1.ProtocolVersion))
	if c.token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.token)
	}
	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return 0, workflowv1.ErrorEnvelope{}, nil, fmt.Errorf("claim %s: %w", op, ctxErr)
		}
		return 0, workflowv1.ErrorEnvelope{}, nil, c.callErrorText(op, "transport", err.Error())
	}
	defer func() { _ = httpResp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(httpResp.Body, c.maxBytes+1))
	if err != nil {
		return 0, workflowv1.ErrorEnvelope{}, nil, c.callErrorText(op, "transport", err.Error())
	}
	if int64(len(body)) > c.maxBytes {
		return 0, workflowv1.ErrorEnvelope{}, nil, &CallError{
			Op:      op,
			Code:    CodeResponseTooLarge,
			Message: fmt.Sprintf("response exceeds the %d-byte bound", c.maxBytes),
		}
	}
	if httpResp.StatusCode < 200 || httpResp.StatusCode > 299 {
		var envelope workflowv1.ErrorEnvelope
		_ = json.Unmarshal(body, &envelope)
		return httpResp.StatusCode, envelope, body, nil
	}
	return httpResp.StatusCode, workflowv1.ErrorEnvelope{}, body, nil
}

// envelopeCode reads the protocol code off a failure envelope.
func envelopeCode(envelope workflowv1.ErrorEnvelope) string {
	return envelope.Code
}

// decodeStrict decodes one JSON document the way the extension boundary
// decodes SDK results: unknown fields rejected, trailing data rejected.
// Anything the merged table does not declare fails closed instead of being
// acted on.
func decodeStrict(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("strict decode: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("strict decode: trailing data after JSON document")
	}
	return nil
}
