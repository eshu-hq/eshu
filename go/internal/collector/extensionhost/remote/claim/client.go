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
	// Clock supplies now for grant-expiry evaluation. Nil means UTC now.
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
		clock = time.Now().UTC
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

// CallError is one failed protocol call. Code is the stable contract
// (a workflowv1 code, or CodeResponseTooLarge for a client-side bound);
// Message is bounded operator detail that never carries credentials.
type CallError struct {
	Op         workflowv1.Operation
	Code       string
	Message    string
	RetryAfter time.Duration
}

func (e *CallError) Error() string {
	return fmt.Sprintf("%s %s: %s", e.Op, e.Code, e.Message)
}

// Stale reports a 409-class conflict: the held epoch is no longer honored.
// The caller must re-claim, never retry the same mutation.
func (e *CallError) Stale() bool {
	return e.Code == workflowv1.CodeStaleFence ||
		e.Code == workflowv1.CodeLeaseExpired ||
		e.Code == workflowv1.CodeClaimConflict
}

// Paced reports flow control: no claim was issued; back off for RetryAfter.
func (e *CallError) Paced() bool { return e.Code == workflowv1.CodePaced }

// NotFound reports a missing row.
func (e *CallError) NotFound() bool { return e.Code == workflowv1.CodeNotFound }

// VersionMismatch reports a protocol-major disagreement.
func (e *CallError) VersionMismatch() bool { return e.Code == workflowv1.CodeVersionMismatch }

// PacedError is a paced ClaimNext: no claim was issued. It mirrors the
// in-tree pacing-deferral-as-error shape so the runner backs off on the
// existing retry-after path. RetryAfter is always the grant delay to honor.
type PacedError struct {
	// Deferral is the server's pacing verdict on the issued=false path.
	// It is zero on the 429 path, where the envelope carries only delay.
	Deferral   workflowv1.ClaimPacingDeferral
	RetryAfter time.Duration
}

func (e *PacedError) Error() string {
	if e.Deferral.RetryAfter > 0 {
		return e.Deferral.Error()
	}
	return fmt.Sprintf("claim pacing engaged: grant delayed %s", e.RetryAfter)
}

// RetryAfterDelay exposes the grant delay on the retry-after path.
func (e *PacedError) RetryAfterDelay() time.Duration { return e.RetryAfter }

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
		return workflowv1.ClaimedWorkItem{}, false, c.callError(workflowv1.OpClaimNext, status, envelope, body)
	}
	if err := decodeStrict(body, &resp); err != nil {
		return workflowv1.ClaimedWorkItem{}, false, c.callErrorText(workflowv1.OpClaimNext, "invalid", err.Error())
	}
	if err := resp.Validate(); err != nil {
		return workflowv1.ClaimedWorkItem{}, false, c.callErrorText(workflowv1.OpClaimNext, "invalid", err.Error())
	}
	if !resp.Issued {
		return workflowv1.ClaimedWorkItem{}, false, &PacedError{
			Deferral:   resp.Deferral,
			RetryAfter: resp.Deferral.RetryAfter,
		}
	}
	return resp.Grant, true, nil
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

// mutate sends one fenced lifecycle mutation. The merged table declares no
// success body for these operations, so a 2xx must carry an empty body:
// bytes on that path fail closed rather than being silently accepted.
func (c *Client) mutate(ctx context.Context, op workflowv1.Operation, mutation workflowv1.ClaimMutation) error {
	if err := mutation.Validate(); err != nil {
		return fmt.Errorf("claim %s: %w", op, err)
	}
	status, envelope, body, err := c.roundTrip(ctx, op, mutation)
	if err != nil {
		return err
	}
	if status < 200 || status > 299 {
		return c.callError(op, status, envelope, body)
	}
	if len(bytes.TrimSpace(body)) != 0 {
		return c.callErrorText(op, "invalid", "unexpected body on an operation with no declared success body")
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
		return workflowv1.CommitPinResponse{}, c.callError(workflowv1.OpValidateAndPinClaimForCommit, status, envelope, body)
	}
	if err := decodeStrict(body, &resp); err != nil {
		return workflowv1.CommitPinResponse{}, c.callErrorText(workflowv1.OpValidateAndPinClaimForCommit, "invalid", err.Error())
	}
	if err := resp.Validate(); err != nil {
		return workflowv1.CommitPinResponse{}, &CallError{
			Op:      workflowv1.OpValidateAndPinClaimForCommit,
			Code:    workflowv1.CodeStaleFence,
			Message: c.scrub("commit pin refused: " + err.Error()),
		}
	}
	return resp, nil
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

// pacedError builds the 429-path paced error from the envelope delay.
func (c *Client) pacedError(envelope workflowv1.ErrorEnvelope) error {
	var retryAfter time.Duration
	if envelope.RetryAfterSeconds > 0 {
		retryAfter = time.Duration(envelope.RetryAfterSeconds) * time.Second
	}
	return &PacedError{RetryAfter: retryAfter}
}

// callError renders a protocol failure. The message is the server's bounded
// detail, scrubbed of credentials; an unparseable body degrades to the
// status text rather than failing to produce an error.
func (c *Client) callError(
	op workflowv1.Operation,
	status int,
	envelope workflowv1.ErrorEnvelope,
	body []byte,
) error {
	code := envelope.Code
	message := envelope.Message
	if strings.TrimSpace(code) == "" {
		code = "unexpected_status"
		message = http.StatusText(status)
		if strings.TrimSpace(message) == "" {
			message = fmt.Sprintf("unexpected status %d", status)
		}
		_ = body
	}
	callErr := &CallError{Op: op, Code: code, Message: c.scrub(boundMessage(message))}
	if envelope.RetryAfterSeconds > 0 {
		callErr.RetryAfter = time.Duration(envelope.RetryAfterSeconds) * time.Second
	}
	return callErr
}

// callErrorText renders a client-side failure (decode, bound, transport).
func (c *Client) callErrorText(op workflowv1.Operation, code, message string) error {
	return &CallError{Op: op, Code: code, Message: c.scrub(boundMessage(message))}
}

// scrub removes the configured bearer from surfaced text: a hostile server
// that echoes the Authorization value in an error message must not have it
// relayed to operators.
func (c *Client) scrub(message string) string {
	if c.token == "" {
		return message
	}
	return strings.ReplaceAll(message, c.token, "[redacted]")
}

// boundMessage truncates surfaced detail to the operator bound.
func boundMessage(message string) string {
	trimmed := strings.TrimSpace(message)
	if len(trimmed) > maxErrorMessage {
		return trimmed[:maxErrorMessage] + "…"
	}
	return trimmed
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
