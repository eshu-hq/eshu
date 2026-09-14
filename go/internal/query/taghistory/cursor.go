// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package taghistory

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// CursorVersion is the plaintext version inside a SEALED tag-history cursor,
// and the only one this build seals or opens. A cursor carrying any other
// version is rejected rather than partially trusted, the rule
// RepositoryRefPageCursorVersion already applies in
// go/internal/query/repositoryreadmodel/repository_refs_page.go. Restarting
// paging is safe: the route has no side effects.
//
// Version 1 was a raw row offset. Version 2 was an unsealed keyset key, which
// a caller could mint at any start of its choosing (#6564 round-3 finding 1);
// it survives ONLY as UnsealedCursorVersion below. A client holding either
// restarts from page one, the same deploy-boundary rule a
// RepositoryRefPageCursorVersion bump carries.
const CursorVersion = 3

// UnsealedCursorVersion is the plaintext version of the unsealed token, which
// this build emits and accepts ONLY where no sealing key is configured and the
// caller's page is not grant-filtered (see EncodeCursor). Nothing is withheld
// from such a caller, so an aimed start buys it nothing it cannot already ask
// for with the offset parameter. A grant-filtered caller never reaches this
// path: with no sealing key its paging fails closed instead.
const UnsealedCursorVersion = 2

// Sealer seals and opens the cursor envelope. It is the AEAD half of
// *secretcrypto.Keyring, named here as an interface so this package does not
// import secretcrypto and so a test can supply its own keyring.
//
// A nil Sealer means "this deployment holds no DEK", not "seal with nothing":
// EncodeCursor and DecodeCursor fall back to the unsealed token, and the
// handler refuses that fallback for a grant-filtered caller.
//
// Beware the nil-interface trap when wiring one: assigning a nil
// *secretcrypto.Keyring to a Sealer field yields a NON-nil interface holding a
// nil pointer, which would panic on the first Seal. Wire it behind an explicit
// `if keyring != nil` guard (cmd/api/wiring.go and cmd/mcp-server/wiring.go
// both do).
type Sealer interface {
	Seal(plaintext, aad []byte) (string, error)
	Open(envelope string, aad []byte) ([]byte, error)
}

// cursorAADPrefix binds a sealed cursor to this route and this cursor version.
// It is concatenated with the image_ref and the caller's Audience, so an
// envelope sealed for another feature (a provider secret, a TOTP secret, the
// bootstrap credential), for another image_ref, or for another authorization
// audience fails Open rather than a post-decrypt string compare, and a version
// bump cannot be replayed across.
const cursorAADPrefix = "eshu/query/tag-history/cursor/v3\x00"

// unscopedAudience is the Audience of every caller that sees the whole history:
// unauthenticated, all-scopes, and legacy shared-token mode. It is a literal
// rather than a digest because there is nothing to distinguish -- such callers
// are interchangeable by construction, they all see every row.
//
// Its real work is separating them from scoped callers, and that is the half of
// the audience binding that closes the actual exposure. Unscoped callers keep
// the offset parameter, so an unscoped caller can mint a cursor at ESSENTIALLY
// ANY raw position by asking for offset=N and reading the next_cursor back.
// Handing such a token to a scoped caller would have restored exactly the
// caller-chosen start the seal exists to remove.
const unscopedAudience = "all-scopes"

// Audience is the authorization context a sealed cursor is bound to. It enters
// the AEAD as additional data, never the wire token, so it is not a value a
// caller can read, edit or present.
//
// What it binds to is the caller's GRANT SET, deliberately not its credential
// and not its principal identity, and each exclusion is load-bearing:
//
//   - Not the credential. A scoped token rotated for the same user carries the
//     same grants, and invalidating an in-flight page on a rotation would break
//     legitimate paging for no security gain.
//   - Not the principal. cmd/api and the standalone cmd/mcp-server can resolve
//     one human through different credential paths, and this design already
//     promises that a cursor the API issued opens on any replica holding the
//     same DEK. A principal-bound cursor would silently break that.
//   - The grant set, because it is what actually decides which rows the caller
//     sees. Two callers with the same grants see the same page, so a token
//     passing between them discloses nothing.
//
// The cost is a grant change mid-walk: the in-flight cursor stops opening and
// the client restarts from page one. That is correct rather than regrettable --
// the filter's answer changed underneath the walk, so continuing it would splice
// two different visibilities into one result -- and it is the same deploy-
// boundary contract key rotation and a version bump already carry, over a walk
// that lasts seconds to minutes.
//
// The derivation is a pure function of the token's own grant lists (see
// RepositoryAccessFilterFromContext and WithCanonicalScopeRepositories, which
// is string rewriting with no graph read), so it cannot drift under ingestion
// and it evaluates identically in every process holding the same DEK.
type Audience string

// AudienceOf derives the cursor audience for one caller's access filter.
//
// The encoding is canonical, so two requests carrying the same grants in a
// different order, or with duplicates, produce the same audience and page
// together: ids are tagged by kind, sorted, deduplicated, and length-prefixed
// before hashing, so no concatenation of one grant set can collide with
// another. The digest is over grant ids only and never leaves the process.
func AudienceOf(access querycontract.RepositoryAccessFilter) Audience {
	if !access.Scoped() {
		return unscopedAudience
	}
	ids := make([]string, 0, len(access.AllowedRepositoryIDs)+len(access.AllowedScopeIDs))
	for _, id := range access.AllowedRepositoryIDs {
		ids = append(ids, "r\x00"+id)
	}
	for _, id := range access.AllowedScopeIDs {
		ids = append(ids, "s\x00"+id)
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	digest := sha256.New()
	for _, id := range ids {
		_, _ = digest.Write([]byte(strconv.Itoa(len(id)) + "\x00" + id))
	}
	return Audience(hex.EncodeToString(digest.Sum(nil)))
}

// ErrCursorRequiresUID and the other plaintext errors below are the shape
// checks DecodeCursor applies AFTER the envelope opens. On a sealed token they
// can only fire on a payload this server itself sealed, so they guard against a
// server bug, not against a forger -- the AEAD tag does that.
var ErrCursorRequiresUID = errors.New("cursor is missing its uid key")

// ErrCursorSealingUnavailable reports that a cursor was presented to, or owed
// to, a grant-filtered caller on a deployment that holds no sealing key. It is
// a configuration gap, never caller error: the handler answers 503 naming the
// variable rather than serving an unsealed token that would reopen the oracle.
var ErrCursorSealingUnavailable = errors.New(
	"tag-history cursor sealing key is not configured; set ESHU_AUTH_SECRET_ENC_KEY or ESHU_AUTH_SECRET_ENC_KEY_FILE",
)

// Cursor is the continuation token GET /api/v0/images/tag-history returns.
//
// It is a SEALED KEYSET cursor: the plaintext names the (first_observed_at,
// uid) position of one row in the statement's total order, and the wire token
// is that plaintext under AES-256-GCM with the deployment DEK
// (secretcrypto.Keyring, ESHU_AUTH_SECRET_ENC_KEY(_FILE)) and a route-specific
// AAD. The next page is "your visible rows after this position".
//
// Both halves are load-bearing, and the SEAL is the half that closes the
// disclosure:
//
//   - A key rather than an offset (#6564 re-review finding 1). An offset was a
//     position in the PRE-FILTER history, so a scoped caller that base64-decoded
//     its own token read the frontier the grant filter had advanced past.
//   - Sealed rather than plaintext (#6564 round-3 finding 1). A key alone still
//     let the caller CHOOSE where the scanned span starts, and that is the
//     oracle: a fully-withheld capped page answers with the key of the 800th raw
//     row after the chosen start, so the answer is a monotone step function of
//     the start, and binary search on the timestamp recovers every withheld
//     row's (first_observed_at, uid) in about forty requests each. Refusing to
//     advance would not have closed it either -- with a caller-chosen start the
//     empty/non-empty distinction is itself that step function.
//   - Bound to its AUDIENCE, not only to the route and the image_ref (#6564
//     review finding, cursor.go). Sealing alone made the reachable start set
//     {page one} U {tokens THIS SERVER issued to ANY caller}, and the second
//     half of that was still too wide: a token minted in one authorization
//     context opened cleanly in another, so a scoped caller handed one started
//     mid-history at a position it had not paged to. It mattered most for
//     unscoped callers, who keep the offset parameter and can therefore mint a
//     token at an arbitrary raw position -- handing one over restored the
//     caller-chosen start outright. With the grant set in the AAD (Audience)
//     the reachable set is {page one} U {tokens issued to a caller holding
//     these same grants}, and any such caller sees the same rows this one does,
//     so nothing in that set discloses anything. THAT is what makes the
//     "cannot choose where such a span starts" claim below true rather than
//     merely likely.
//
// Three properties follow from the key rather than from a check:
//
//   - Forward-only and duplicate-free. An observation inserted behind the key
//     between two requests does not shift the walk, so no row is returned
//     twice; it is simply not seen, the same contract RepositoryRefPageCursor
//     documents. An offset shifted under exactly that insert.
//   - Valid at any page size. There is no Limit binding: it existed only to
//     stop an offset token being re-aimed at limit=1, and a key has no position
//     to re-aim. A caller may change limit mid-walk.
//   - Stable across restarts and replicas that hold the same DEK. The key is a
//     property tuple any replica can evaluate; nothing is stored server-side.
//     A replica or a standalone MCP server WITHOUT the DEK cannot open a cursor
//     the API issued -- that is the deployment obligation this design adds, and
//     it is documented on the route, in the Helm README and in compose.
//
// Rotation is a deploy boundary, not an engineered path. Open resolves the key
// by the envelope's key_id, so a multi-key keyring opens old cursors; a
// KeyringFromEnv keyring holds one primary, so rotating it makes in-flight
// cursors 400 and the client restarts from page one. Paging is seconds to
// minutes long, so that is the same boundary the v1->v2->v3 bumps already
// carry. A grant change mid-walk lands on that same boundary, by the same
// mechanism, for the reason Audience documents. There is no iat or expiry: the
// key carries no state, and a stale key simply reads the rows after it.
//
// NullAt is needed because the store holds THREE timestamp states with three
// sort positions: a stored empty string (a zero ObservedAt, written by
// ociTagObservedAtValue in go/internal/storage/cypher), a real millisecond
// string, and no property at all (nodes created before #5459 shipped
// first_observed_at). The empty string needs no special case, because
// t.first_observed_at compares greater than it for every non-empty string. "No
// timestamp" cannot be expressed as a string value at all, and those rows sort
// LAST (measured on the pinned build, see
// docs/internal/evidence/6564-tag-history-keyset-pagination.md), so NullAt
// selects the null-tail statement instead of a string comparison.
//
// Residue this does NOT close, disclosed on every caller-facing surface --
// COUNTS, never identities: on a page that reached the read cap holding k rows
// below the requested limit, a scoped caller learns that 800-k of the 800 raw
// observations after its own last-shown row (or after the sealed frontier of
// its previous capped page) are ones it may not see. It cannot choose where
// such a span starts -- the seal bounds the reachable starts to tokens this
// server issued, and the Audience binding narrows those to tokens issued
// against this same grant set, whose holder sees exactly these rows -- cannot
// read the frontier, and never learns a withheld observation's
// first_observed_at, uid or digest. That is a strict subset of the honest
// count:0 signal already disclosed, so sealing concedes nothing new.
type Cursor struct {
	Version  int    `json:"v"`
	ImageRef string `json:"ref"`
	At       string `json:"at,omitempty"`
	NullAt   bool   `json:"nt,omitempty"`
	UID      string `json:"uid"`
}

// EncodeCursor renders key as the token next_cursor carries for imageRef.
//
// With a sealer it returns the AES-256-GCM envelope
// (ESK1.<key_id>.<nonce>.<ciphertext>), bound to imageRef through the AAD. The
// error is a real sealing failure -- the handler answers 500, never a cleartext
// fallback.
//
// With a nil sealer it returns the unsealed UnsealedCursorVersion token. That
// is legal only for a caller whose page is not grant-filtered; the handler
// checks that before calling (see tagHistoryCursorUnavailable in
// go/internal/query/tag_history.go).
func EncodeCursor(sealer Sealer, imageRef string, audience Audience, key Key) (string, error) {
	if sealer == nil {
		return base64.RawURLEncoding.EncodeToString(marshalCursor(UnsealedCursorVersion, imageRef, key)), nil
	}
	sealed, err := sealer.Seal(marshalCursor(CursorVersion, imageRef, key), cursorAAD(imageRef, audience))
	if err != nil {
		return "", fmt.Errorf("seal cursor: %w", err)
	}
	return sealed, nil
}

// DecodeCursor opens a token produced by EncodeCursor and returns the key it
// names. Callers MUST turn a non-nil error into a 400 -- except
// ErrCursorSealingUnavailable, which the handler raises itself before calling
// here and answers 503.
//
// With a sealer, an envelope that fails to open -- forged, truncated, sealed
// under a retired key, sealed for another image_ref, or sealed by another
// feature under a different AAD -- returns secretcrypto's single opaque error,
// so the refusal never tells a caller which part was wrong. The plaintext
// checks that follow (version, uid, the nt/at conflict) can then only fire on a
// payload this server sealed.
//
// With a nil sealer it accepts only the unsealed token, which this build issues
// only to a caller with nothing withheld from it.
//
// It does NOT error on a key that matches no current row. A retracted or
// re-projected observation is not a paging failure: the predicate is on values,
// not on that row still existing, so the next page is simply the rows after
// that position. A key beyond the end of the history yields an empty,
// untruncated page.
func DecodeCursor(sealer Sealer, raw, imageRef string, audience Audience) (Key, error) {
	plaintext, wantVersion, err := openCursorPayload(sealer, raw, imageRef, audience)
	if err != nil {
		return Key{}, err
	}
	var cursor Cursor
	if err := json.Unmarshal(plaintext, &cursor); err != nil {
		return Key{}, fmt.Errorf("invalid cursor payload: %w", err)
	}
	if cursor.Version != wantVersion {
		return Key{}, fmt.Errorf("unsupported cursor version %d", cursor.Version)
	}
	if cursor.ImageRef != imageRef {
		return Key{}, errors.New("cursor was issued for another image_ref")
	}
	if cursor.UID == "" {
		return Key{}, ErrCursorRequiresUID
	}
	if cursor.NullAt && cursor.At != "" {
		return Key{}, errors.New("cursor claims no first_observed_at but carries one")
	}
	return Key{At: cursor.At, NullAt: cursor.NullAt, UID: cursor.UID}, nil
}

// cursorAAD binds a sealed cursor to this route, this cursor version, this
// image_ref and this authorization audience. It is additional data, not
// ciphertext: Open must be handed the identical bytes, which is what makes a
// cursor for another image_ref or another audience fail the AEAD tag rather
// than a string compare after decryption.
//
// The image_ref is length-prefixed so the two variable-width components cannot
// be shifted against each other. A caller controls image_ref through
// repository_id and tag, and a query value may carry any byte including NUL, so
// a bare separator would let one (image_ref, audience) pair be re-read as
// another.
func cursorAAD(imageRef string, audience Audience) []byte {
	return []byte(cursorAADPrefix + strconv.Itoa(len(imageRef)) + "\x00" + imageRef + string(audience))
}

// openCursorPayload returns one token's plaintext and the plaintext version
// that token shape must carry. The two shapes are never interchangeable: a
// sealed envelope handed to an unsealed-mode server fails base64 on the
// envelope's dots, and an unsealed token handed to a sealing server fails the
// AEAD tag.
func openCursorPayload(sealer Sealer, raw, imageRef string, audience Audience) ([]byte, int, error) {
	if sealer == nil {
		plaintext, err := base64.RawURLEncoding.DecodeString(raw)
		if err != nil {
			return nil, 0, fmt.Errorf("invalid cursor encoding: %w", err)
		}
		return plaintext, UnsealedCursorVersion, nil
	}
	plaintext, err := sealer.Open(raw, cursorAAD(imageRef, audience))
	if err != nil {
		return nil, 0, fmt.Errorf("cursor did not open: %w", err)
	}
	return plaintext, CursorVersion, nil
}

// marshalCursor renders the plaintext of one cursor as JSON.
func marshalCursor(version int, imageRef string, key Key) []byte {
	// The payload is a bounded struct of three strings, a bool and an int;
	// Marshal cannot fail.
	raw, _ := json.Marshal(Cursor{
		Version:  version,
		ImageRef: imageRef,
		At:       key.At,
		NullAt:   key.NullAt,
		UID:      key.UID,
	})
	return raw
}

// ScopedTruthReason is the truth-envelope reason a grant-filtered caller
// receives. It discloses the BUILT_FROM coverage cost, the mutated-flag residue
// of a blanked previous_digest, the refilled cursor-only paging, and the
// counts-not-identities residue of a cap-reached page. It lives beside the
// cursor and the refill loop it describes so a change to either lands next to
// the sentence that publishes it (the API handler only selects it).
const ScopedTruthReason = "resolved from bounded container image tag-observation history anchored on image_ref, " +
	"bound to the caller's repository grant through ContainerImage-[:BUILT_FROM]->Repository: a row is kept only when " +
	"the image at its resolved_digest is BUILT_FROM a granted repository, previous_digest is blanked unless that image is " +
	"also BUILT_FROM a granted repository, and observations whose image has no BUILT_FROM edge are withheld; mutated is " +
	"left as observed, so a row with mutated true and no previous_digest still tells you some prior digest existed; the " +
	"page is refilled across further fixed-size reads until it holds limit visible rows, the history ends, or the " +
	"per-request read cap is reached, so on a filled page count below limit does not measure withheld rows; continue with " +
	"next_cursor, an encrypted token naming one row's first_observed_at and uid rather than a row position, so paging is " +
	"forward-only and an observation inserted behind that point on a later page is not returned; replay it exactly as " +
	"issued and keep following it until truncated is false, because a token this server did not issue cannot be opened " +
	"and is refused, and neither can one issued under repository grants other than the ones you hold now -- if your " +
	"grants change mid-walk the token stops opening and you restart from page one; what a cap-reached page discloses, stated here rather than hidden, is a count and never an identity: " +
	"with count below limit and truncated true, the remainder of the 800 raw observations after the row you were last " +
	"shown, or after the frontier of your previous capped page, are ones you may not see -- you cannot choose where that " +
	"span starts, cannot read the frontier, and never learn a withheld observation's first_observed_at, uid or digest"

// CursorUnavailableReason is appended to the scoped truth reason when
// this deployment holds no cursor sealing key. Paging fails closed rather than
// downgrading to an unsealed token, so the caller is told why its page stops
// and the operator is told which variable to set.
const CursorUnavailableReason = "; continuation unavailable: the cursor sealing key is not configured " +
	"(ESHU_AUTH_SECRET_ENC_KEY or ESHU_AUTH_SECRET_ENC_KEY_FILE), so next_cursor is omitted and this page cannot be continued"
