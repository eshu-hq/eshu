// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iampolicy

// StatementShape is the decoded, trust-relevant shape of one permission
// statement: everything a grant fold needs to decide whether the statement can
// be conservatively trusted, with none of the decisions made yet.
//
// The two folds that consume it — the reducer root's privilege-escalation
// builder and the iamcan family's CAN_PERFORM builder — count into different
// tallies and check against different catalogs, so the decisions stay at each
// caller. The extraction is identical in both, so it lives here once.
type StatementShape struct {
	// Effect is the raw statement effect, "Allow" or "Deny". Anything else is
	// neither, and a fold must ignore the statement rather than guess.
	Effect string
	// Actions is the statement's lowercase action list.
	Actions []string
	// PolicySource names where the statement came from (inline, attached
	// managed, trust, resource policy). The CAN_PERFORM fold reads only
	// identity-policy sources.
	PolicySource string
	// HasConditions reports whether the statement carries condition keys. A
	// conditioned Allow is never conservatively trusted: conditions carry key
	// names only, never values, so the reducer cannot evaluate them.
	HasConditions bool
	// HasNotActions reports whether the statement uses NotAction. Its
	// complement semantics are not conservatively resolvable.
	HasNotActions bool
	// HasNotResources reports whether the statement uses NotResource, for the
	// same reason.
	HasNotResources bool
}

// Trustable reports whether the statement is an Allow that carries no
// condition, NotAction, or NotResource — the only shape either fold folds into
// a principal's trusted action set. A false here is never silent: each caller
// counts the refusal under its own skip reason.
func (s StatementShape) Trustable() bool {
	return s.Effect == EffectAllow && !s.HasConditions && !s.HasNotActions && !s.HasNotResources
}

// Statement effect tokens as AWS emits them. They are compared verbatim: a
// statement whose effect is neither contributes nothing, rather than being
// guessed into one.
const (
	// EffectAllow is the grant effect.
	EffectAllow = "Allow"
	// EffectDeny is the refusal effect, which always wins over an Allow.
	EffectDeny = "Deny"
)

// Classify extracts a statement's trust-relevant shape. It is the single read
// site for the wrapped iamv1.Permission's trust fields, which is what keeps the
// payloadusage wrapper attribution (#4668) able to see them: the wrapper struct
// and the reads through it stay in one package.
func Classify(statement Statement) StatementShape {
	return StatementShape{
		Effect:          statement.Permission.Effect,
		Actions:         statement.Permission.Actions,
		PolicySource:    statement.Permission.PolicySource,
		HasConditions:   derefBool(statement.Permission.HasConditions),
		HasNotActions:   len(statement.Permission.NotActions) > 0,
		HasNotResources: len(statement.Permission.NotResources) > 0,
	}
}

// derefBool returns the pointed-to bool, or false when the pointer is nil,
// matching the pre-typing payload default so a flag that was absent reads as
// false exactly as it did before typing. It is local rather than a payloadcore
// call so this package keeps its standard-library-plus-SDK import set.
func derefBool(value *bool) bool {
	return value != nil && *value
}
