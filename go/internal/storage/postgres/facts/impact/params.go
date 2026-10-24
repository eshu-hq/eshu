// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factimpact

import (
	"strconv"
	"strings"
)

// Param names one value list a branch statement binds. The loader maps each
// Param to the matching SupplyChainImpactFactFilter field (or a derived
// list) and binds them in Branch.Params order.
type Param int

// Bound filter lists. The Lower* lists are the lower-cased, cleaned twins
// the suppression scope arms compare against; the *ScopeIDs lists are the
// scope IDs the scope resolution statement returns for RepositoryIDs and
// FileRepositoryIDs, with their active generation IDs in the matching
// *GenerationIDs lists; SuppressionLimit is the suppression cap plus one.
const (
	PackageIDs Param = iota + 1
	PURLs
	CVEIDs
	AdvisoryIDs
	SubjectDigests
	ProductCriteria
	DocumentIDs
	RepositoryIDs
	ImageRefs
	FileRepositoryIDs
	LowerPackageIDs
	LowerPURLs
	LowerCVEIDs
	LowerSubjectDigests
	LowerRepositoryIDs
	LowerAdvisoryIDs
	RepositoryScopeIDs
	RepositoryGenerationIDs
	FileRepositoryScopeIDs
	FileRepositoryGenerationIDs
	SuppressionLimit
)

// LowerTwin returns the normalized twin of a base list and whether one
// exists. A normalized arm can only match when its base list is non-empty.
func (p Param) LowerTwin() (Param, bool) {
	switch p {
	case PackageIDs:
		return LowerPackageIDs, true
	case PURLs:
		return LowerPURLs, true
	case CVEIDs:
		return LowerCVEIDs, true
	case SubjectDigests:
		return LowerSubjectDigests, true
	case RepositoryIDs:
		return LowerRepositoryIDs, true
	case AdvisoryIDs:
		return LowerAdvisoryIDs, true
	default:
		return 0, false
	}
}

// binder assigns $n placeholders in first-use order so each statement binds
// exactly the lists it reads and no unreferenced placeholder reaches the
// server (an unreferenced $n has no inferable type).
type binder struct {
	order []Param
	index map[Param]int
}

func newBinder() *binder { return &binder{index: map[Param]int{}} }

func (b *binder) ref(p Param) string {
	position, ok := b.index[p]
	if !ok {
		b.order = append(b.order, p)
		position = len(b.order)
		b.index[p] = position
	}
	return "$" + strconv.Itoa(position)
}

// list renders `$n::text[]` for a text-array list.
func (b *binder) list(p Param) string { return b.ref(p) + "::text[]" }

// quoteKinds renders a fact-kind literal list for an IN (...) predicate.
func quoteKinds(kinds []string) string {
	quoted := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		quoted = append(quoted, "'"+kind+"'")
	}
	return strings.Join(quoted, ", ")
}

// kindPredicate renders fact.fact_kind = 'K' for one kind and
// fact.fact_kind IN (...) for several, the forms the partial index
// predicates are written in.
func kindPredicate(column string, kinds []string) string {
	if len(kinds) == 1 {
		return column + " = '" + kinds[0] + "'"
	}
	return column + " IN (" + quoteKinds(kinds) + ")"
}
