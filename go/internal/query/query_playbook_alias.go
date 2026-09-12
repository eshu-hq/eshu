// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query //nolint:dirgate // B7 root alias shim for #6642: type aliases and thin forwarders for the moved query-playbook family must live in package query so handler wiring, cmd constructors, and the staying investigation-workflow family compile unchanged.

import (
	"github.com/eshu-hq/eshu/go/internal/query/playbook"
)

// query_playbook_alias.go is the root alias shim for the query-playbook
// handler family (#6642, modelled on freshness_alias.go and
// work_item_alias.go). Its catalog, resolver, validation, and handler files
// moved to playbook/. The QueryPlaybookHandler type alias keeps handler.go's
// `Playbooks *QueryPlaybookHandler` field, cmd/api's and cmd/mcp-server's
// wiring_router.go, root doc.go, and the apirecording/mcpreplay replay
// tests compiling unchanged; the PlaybookCatalog/PlaybookCatalogVersions/
// PlaybookToolNames/LookupPlaybook func forwarders have their own real
// callers outside the family -- internal/answerquality/report_score.go and
// internal/serviceintel/suggestions.go (LookupPlaybook),
// internal/cli/hosted/onboard.go (PlaybookCatalog) -- plus
// internal/demospec/manifest_test.go (PlaybookCatalog),
// internal/mcp/query_playbook_registry_test.go (PlaybookToolNames) and
// go/internal/mcp/{answer_parity_gen2_test.go,demo_playbook_parity_test.go}
// in test code. This move touches no caller outside the family. The staying
// investigation_workflow.go and
// investigation_workflow_catalog.go (#6642 keeps that family in root for a
// later lane) reach the playbook helpers they use through this file's type
// aliases and unexported forwarders, and root's envelope_aliases.go keeps its own
// CapabilityQueryPlaybooks = "query.playbooks" literal unchanged rather than
// forwarding here -- see README.md's Move evidence for why.
//
// One home per symbol: nothing here implements behavior, it only aliases or
// forwards to the canonical home. New code must import playbook directly.

// QueryPlaybookHandler is the query-playbook handler family type. Its home
// is playbook/; this alias keeps handler.go's `Playbooks *QueryPlaybookHandler`
// field, cmd/api's and cmd/mcp-server's wiring, and
// go/internal/mcp/demo_playbook_parity_test.go spelling
// query.QueryPlaybookHandler unchanged.
type QueryPlaybookHandler = playbook.Handler

// QueryPlaybook is a deterministic, bounded, versioned description of a
// common starter-prompt or cookbook workflow. Its home is playbook/
// (playbook.Definition); go/internal/mcp/answer_parity_gen2_test.go keeps
// spelling query.QueryPlaybook unchanged.
type QueryPlaybook = playbook.Definition

// PlaybookInputType enumerates the value kinds a playbook input accepts. Its
// home is playbook/ (playbook.InputType); no caller outside the family
// spells this bare type today, kept so root's pre-move exported surface
// stays whole.
type PlaybookInputType = playbook.InputType

// PlaybookInputString is a free-form string playbook input. Its home is
// playbook/ (playbook.InputString); investigation_workflow.go and
// investigation_workflow_catalog.go keep spelling query.PlaybookInputString
// unchanged.
const PlaybookInputString = playbook.InputString

// PlaybookInputIdentifier is a canonical identifier playbook input. Its home
// is playbook/ (playbook.InputIdentifier); investigation_workflow.go and
// investigation_workflow_catalog.go keep spelling
// query.PlaybookInputIdentifier unchanged.
const PlaybookInputIdentifier = playbook.InputIdentifier

// PlaybookInput declares one input a playbook requires or accepts. Its home
// is playbook/ (playbook.Input); investigation_workflow.go and
// investigation_workflow_catalog.go keep spelling query.PlaybookInput
// unchanged.
type PlaybookInput = playbook.Input

// PlaybookParamSource enumerates where a resolved parameter value comes
// from. Its home is playbook/ (playbook.ParamSource); no caller outside the
// family spells this bare type today, kept so root's pre-move exported
// surface stays whole.
type PlaybookParamSource = playbook.ParamSource

// Playbook param-source aliases preserve every pre-move root spelling. None
// has a caller outside the family today; they are kept so the alias stanza
// carries the whole enumeration, not a subset a future caller would find
// half-missing.
const (
	PlaybookParamFromInput   = playbook.ParamFromInput
	PlaybookParamConstString = playbook.ParamConstString
	PlaybookParamConstInt    = playbook.ParamConstInt
	PlaybookParamConstBool   = playbook.ParamConstBool
)

// PlaybookParam declares one bounded argument for a step. Its home is
// playbook/ (playbook.Param, exporting ValidateSingleSource at its
// declaration because investigation_workflow.go is the caller that needs
// it); investigation_workflow.go and investigation_workflow_catalog.go keep
// spelling query.PlaybookParam unchanged.
type PlaybookParam = playbook.Param

// PlaybookDrilldown declares an optional follow-up call surfaced after a
// step. Its home is playbook/ (playbook.Drilldown); no caller outside the
// family spells this bare type today, kept so root's pre-move exported
// surface stays whole.
type PlaybookDrilldown = playbook.Drilldown

// PlaybookStep is one ordered, bounded call in a playbook. Its home is
// playbook/ (playbook.Step); investigation_workflow.go keeps spelling
// query.PlaybookStep unchanged.
type PlaybookStep = playbook.Step

// PlaybookFailureMode declares a truth or error condition a step family can
// hit. Its home is playbook/ (playbook.FailureMode); no caller outside the
// family spells this bare type today, kept so root's pre-move exported
// surface stays whole.
type PlaybookFailureMode = playbook.FailureMode

// PlaybookVersionRef is a compact (ID, Version) pair used for catalog
// stability assertions. Its home is playbook/ (playbook.VersionRef); no
// caller outside the family spells this bare type today, kept so root's
// pre-move exported surface stays whole.
type PlaybookVersionRef = playbook.VersionRef

// ResolvedCall is one fully specified, bounded call produced by resolving a
// playbook step. Its home is playbook/ (unchanged spelling); no caller
// outside the family spells this bare type today, kept so root's pre-move
// exported surface stays whole.
type ResolvedCall = playbook.ResolvedCall

// ResolvedPlaybook is the deterministic output of resolving a playbook
// against concrete inputs. Its home is playbook/ (unchanged spelling);
// go/internal/mcp/answer_parity_gen2_test.go keeps spelling
// query.ResolvedPlaybook unchanged.
type ResolvedPlaybook = playbook.ResolvedPlaybook

// PlaybookCatalog returns the versioned, deterministic catalog of query
// playbooks. Its home is playbook/ (playbook.Catalog);
// internal/cli/hosted/onboard.go is its production caller, and
// internal/demospec/manifest_test.go and
// go/internal/mcp/answer_parity_gen2_test.go keep spelling
// query.PlaybookCatalog unchanged in test code.
func PlaybookCatalog() []QueryPlaybook {
	return playbook.Catalog()
}

// PlaybookCatalogVersions returns the catalog identity as an ordered slice
// of (ID, Version) pairs. Its home is playbook/ (playbook.CatalogVersions);
// no caller outside the family spells this today, kept so root's pre-move
// exported surface stays whole.
func PlaybookCatalogVersions() []PlaybookVersionRef {
	return playbook.CatalogVersions()
}

// PlaybookToolNames returns the sorted, de-duplicated set of first-class
// tool names referenced by any catalog step or drilldown. Its home is
// playbook/ (playbook.ToolNames); go/internal/mcp/answer_parity_gen2_test.go
// keeps spelling query.PlaybookToolNames unchanged.
func PlaybookToolNames() []string {
	return playbook.ToolNames()
}

// LookupPlaybook returns the catalog playbook with the given ID and whether
// it was found. Its home is playbook/ (playbook.Lookup);
// internal/answerquality/report_score.go and
// internal/serviceintel/suggestions.go are its production callers, and
// go/internal/mcp/{answer_parity_gen2_test.go,demo_playbook_parity_test.go}
// keep spelling query.LookupPlaybook unchanged in test code.
func LookupPlaybook(id string) (QueryPlaybook, bool) {
	return playbook.Lookup(id)
}

// boolParam declares a constant boolean flag argument. Its home is
// playbook/, exported there as BoolParam because
// investigation_workflow_catalog.go is the caller that needs this
// unexported root spelling.
func boolParam(name string, value bool) PlaybookParam {
	return playbook.BoolParam(name, value)
}

// constStringParam declares a constant string argument. Its home is
// playbook/, exported there as ConstStringParam because
// investigation_workflow_catalog.go is the caller that needs this
// unexported root spelling.
func constStringParam(name, value string) PlaybookParam {
	return playbook.ConstStringParam(name, value)
}

// inputParam binds a tool argument to a declared playbook input. Its home is
// playbook/, exported there as InputParam because investigation_workflow.go
// and investigation_workflow_catalog.go are the callers that need this
// unexported root spelling.
func inputParam(name, fromInput string) PlaybookParam {
	return playbook.InputParam(name, fromInput)
}

// limitParam declares a default bounded limit for a tool argument. Its home
// is playbook/, exported there as LimitParam because
// investigation_workflow_catalog.go is the caller that needs this
// unexported root spelling.
func limitParam(name string, value int) PlaybookParam {
	return playbook.LimitParam(name, value)
}

// resolveParams binds a step's declared params to concrete argument values.
// Its home is playbook/, exported there as ResolveParams because
// investigation_workflow.go is the caller that needs this unexported root
// spelling.
func resolveParams(playbookID string, step PlaybookStep, inputs map[string]string) (map[string]any, error) {
	return playbook.ResolveParams(playbookID, step, inputs)
}

// isRawCypherTool reports whether tool is a raw graph query tool a playbook
// step may not reference. Its home is playbook/, exported there as
// IsRawCypherTool because investigation_workflow.go is the caller that
// needs this unexported root spelling.
func isRawCypherTool(tool string) bool {
	return playbook.IsRawCypherTool(tool)
}
