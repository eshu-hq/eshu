// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package nuget

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"strings"
)

// propDecl is one literal-or-not property declaration kept as a row.
type propDecl struct {
	name      string
	value     string
	condition string
	groupCond string
	oversized bool
	stateKey  string
}

// propState folds every declaration of one property name (case-insensitive,
// like MSBuild). A reference resolves only while the state is clean.
type propState struct {
	value   string
	seen    bool
	differ  bool
	anyCond bool
	bad     bool
}

// itemDecl is one validated package item.
type itemDecl struct {
	kind      string
	name      string
	version   string
	override  string
	private   string
	include   string
	exclude   string
	condition string
	groupCond string
	reasons   itemReason
}

// propsScan streams one props file's tokens. Allocation is bounded by the
// caps in props.go and the input size, never by nesting or entry count.
type propsScan struct {
	dec    *xml.Decoder
	depth  int
	limits *propsLimits
	props  []propDecl
	states map[string]*propState
	items  []itemDecl
	// tracker sees Update, Remove, duplicate, and unevaluated constructs
	// that demote an otherwise resolved item row.
	tracker itemTracker
}

func newPropsScan(source []byte, limits *propsLimits) *propsScan {
	dec := xml.NewDecoder(bytes.NewReader(source))
	dec.Strict = true
	return &propsScan{
		dec: dec, limits: limits, states: make(map[string]*propState),
		tracker: newItemTracker(),
	}
}

// token returns the next token and tracks element depth, failing with
// errPropsTooDeep before the decoder's own element stack can grow past the cap.
func (s *propsScan) token() (xml.Token, error) {
	tok, err := s.dec.Token()
	if err != nil {
		return nil, err
	}
	switch tok.(type) {
	case xml.StartElement:
		s.depth++
		if s.depth > MaxPropsDepth {
			return nil, errPropsTooDeep
		}
	case xml.EndElement:
		s.depth--
	}
	return tok, nil
}

func (s *propsScan) run() error {
	rootSeen := false
	for {
		tok, err := s.token()
		if errors.Is(err, io.EOF) {
			if !rootSeen {
				return errors.New("no Project root element")
			}
			return nil
		}
		if err != nil {
			return err
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if rootSeen {
			return errors.New("more than one root element")
		}
		if start.Name.Local != "Project" {
			return errors.New("root element is not Project")
		}
		rootSeen = true
		for _, attr := range start.Attr {
			if attr.Name.Space == "" && strings.EqualFold(attr.Name.Local, "Sdk") {
				// An SDK project imports props and targets this scanner never reads.
				s.tracker.unevaluated = true
			}
		}
		if err := s.project(); err != nil {
			return err
		}
	}
}

func (s *propsScan) project() error {
	for {
		tok, err := s.token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.EndElement:
			return nil
		case xml.StartElement:
			var herr error
			switch t.Name.Local {
			case "PropertyGroup":
				herr = s.propertyGroup(t)
			case "ItemGroup":
				herr = s.itemGroup(t)
			default:
				// Import, Choose, ItemDefinitionGroup, and the rest can add,
				// change, or default items, so every item row is demoted.
				s.limits.skippedElements++
				s.tracker.unevaluated = true
				herr = s.skipElement()
			}
			if herr != nil {
				return herr
			}
		}
	}
}

// skipElement consumes tokens through the End of the element whose Start the
// caller already read.
func (s *propsScan) skipElement() error {
	for level := 1; level > 0; {
		tok, err := s.token()
		if err != nil {
			return err
		}
		switch tok.(type) {
		case xml.StartElement:
			level++
		case xml.EndElement:
			level--
		}
	}
	return nil
}

// readText collects the character data of the element whose Start the caller
// already read, up to limit bytes. Child elements are skipped and reported.
func (s *propsScan) readText(limit int) (text string, over bool, nested bool, err error) {
	var buf []byte
	for {
		tok, terr := s.token()
		if terr != nil {
			return "", false, false, terr
		}
		switch t := tok.(type) {
		case xml.CharData:
			if over {
				continue
			}
			room := limit + 1 - len(buf)
			if len(t) > room {
				buf = append(buf, t[:room]...)
				over = true
			} else {
				buf = append(buf, t...)
			}
		case xml.StartElement:
			nested = true
			if serr := s.skipElement(); serr != nil {
				return "", false, false, serr
			}
		case xml.EndElement:
			text = strings.TrimSpace(string(buf))
			if len(text) > limit {
				over = true
			}
			if over {
				text = ""
			}
			return text, over, nested, nil
		}
	}
}

func (s *propsScan) propertyGroup(start xml.StartElement) error {
	groupCond, _ := attrOnce(start.Attr, "Condition")
	for {
		tok, err := s.token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.EndElement:
			return nil
		case xml.StartElement:
			if err := s.property(t, groupCond); err != nil {
				return err
			}
		}
	}
}

func (s *propsScan) property(start xml.StartElement, groupCond string) error {
	cond, _ := attrOnce(start.Attr, "Condition")
	value, over, nested, err := s.readText(MaxPropsValueBytes)
	if err != nil {
		return err
	}
	name := start.Name.Local
	if len(name) > MaxPropsNameBytes {
		s.limits.oversizedFields++
		return nil
	}
	if len(cond) > MaxPropsConditionBytes || len(groupCond) > MaxPropsConditionBytes {
		s.limits.oversizedFields++
		cond, groupCond = "", ""
		over = true
	}
	key := strings.ToLower(name)
	state := s.states[key]
	if state == nil {
		if len(s.states) >= MaxPropsPropertyRows {
			s.limits.droppedPropertyRows++
			return nil
		}
		state = &propState{}
		s.states[key] = state
	}
	if nested {
		s.limits.skippedItems++
		state.bad = true
		return nil
	}
	if over {
		s.limits.oversizedFields++
		state.bad = true
	}
	if state.seen && state.value != value {
		state.differ = true
	}
	if !state.seen {
		state.value, state.seen = value, true
	}
	if cond != "" || groupCond != "" {
		state.anyCond = true
	}
	if len(s.props) >= MaxPropsPropertyRows {
		s.limits.droppedPropertyRows++
		return nil
	}
	s.props = append(s.props, propDecl{
		name: name, value: value, condition: cond, groupCond: groupCond,
		oversized: over, stateKey: key,
	})
	return nil
}

func (s *propsScan) itemGroup(start xml.StartElement) error {
	groupCond, _ := attrOnce(start.Attr, "Condition")
	for {
		tok, err := s.token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.EndElement:
			return nil
		case xml.StartElement:
			if err := s.item(t, groupCond); err != nil {
				return err
			}
		}
	}
}

// itemKinds are the item types the grammar turns into dependency rows.
var itemKinds = [...]string{"PackageVersion", "GlobalPackageReference", "PackageReference"}

func (s *propsScan) item(start xml.StartElement, groupCond string) error {
	local := start.Name.Local
	known := false
	for _, kind := range itemKinds {
		if local == kind {
			known = true
		}
	}
	if !known {
		for i, kind := range itemKinds {
			if strings.EqualFold(local, kind) {
				// MSBuild item types are case-insensitive, so this item is
				// real but unread: every row of its type is demoted.
				s.limits.skippedItems++
				s.tracker.unresolved[i] = true
			}
		}
		return s.skipElement()
	}

	conflict := hasCaseVariantAttribute(start.Attr)
	decl := itemDecl{kind: local, groupCond: groupCond}
	take := func(dst *string, name string) {
		value, dup := attrOnce(start.Attr, name)
		*dst = strings.TrimSpace(value)
		conflict = conflict || dup
	}
	var include, update, remove, exclude string
	take(&include, "Include")
	take(&update, "Update")
	take(&remove, "Remove")
	take(&exclude, "Exclude")
	if exclude != "" {
		decl.reasons |= reasonExclude
	}
	take(&decl.version, "Version")
	take(&decl.override, "VersionOverride")
	take(&decl.private, "PrivateAssets")
	take(&decl.include, "IncludeAssets")
	take(&decl.exclude, "ExcludeAssets")
	take(&decl.condition, "Condition")

	fields := map[string]*string{
		"version": &decl.version, "versionoverride": &decl.override,
		"privateassets": &decl.private, "includeassets": &decl.include,
		"excludeassets": &decl.exclude,
	}
	oversize := false
	for {
		tok, err := s.token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			dst, ok := fields[strings.ToLower(t.Name.Local)]
			if !ok {
				if err := s.skipElement(); err != nil {
					return err
				}
				continue
			}
			for _, attr := range t.Attr {
				if attr.Name.Space == "" && strings.EqualFold(attr.Name.Local, "Condition") {
					decl.reasons |= reasonConditionalMetadata
				}
			}
			text, over, nested, err := s.readText(MaxPropsValueBytes)
			if err != nil {
				return err
			}
			oversize = oversize || over
			// MSBuild applies child metadata after the attribute, so a child
			// that differs, including an empty one, makes the value unproven.
			conflict = conflict || nested || (*dst != "" && *dst != text)
			if text != "" {
				*dst = text
			}
		case xml.EndElement:
			return s.admitItem(decl, itemSpecs{include, update, remove}, conflict, oversize)
		}
	}
}

// itemSpecs holds an item's Include, Update, and Remove attribute values.
type itemSpecs struct {
	include, update, remove string
}

// admitItem records the item's operations for the tracker, then applies the
// name, conflict, and size rules and the row cap.
func (s *propsScan) admitItem(decl itemDecl, specs itemSpecs, conflict, oversize bool) error {
	include := specs.include
	if specs.update != "" {
		s.tracker.modify(decl.kind, specs.update, false, len(s.items))
	}
	if specs.remove != "" {
		s.tracker.modify(decl.kind, specs.remove, true, len(s.items))
	}
	if include != "" {
		s.tracker.include(decl.kind, include)
	}
	if oversize || len(include) > MaxPropsNameBytes || len(decl.version) > MaxPropsVersionBytes ||
		len(decl.override) > MaxPropsVersionBytes || len(decl.condition) > MaxPropsConditionBytes ||
		len(decl.groupCond) > MaxPropsConditionBytes || len(decl.private) > MaxPropsValueBytes ||
		len(decl.include) > MaxPropsValueBytes || len(decl.exclude) > MaxPropsValueBytes {
		s.limits.oversizedFields++
		return nil
	}
	if conflict || include == "" || specs.update != "" || specs.remove != "" || !literalPackageName(include) ||
		(decl.version != "" && decl.override != "") {
		s.limits.skippedItems++
		return nil
	}
	decl.name = include
	if len(s.items) >= MaxPropsItemRows {
		s.limits.droppedItemRows++
		return nil
	}
	s.items = append(s.items, decl)
	return nil
}

// literalPackageName rejects item lists, wildcards, and MSBuild expressions:
// none of them names exactly one package.
func literalPackageName(name string) bool {
	if strings.ContainsAny(name, ";*?") {
		return false
	}
	return !strings.Contains(name, "$(") && !strings.Contains(name, "@(") && !strings.Contains(name, "%(")
}

// attrOnce returns the unprefixed attribute name and whether it appears more
// than once, which MSBuild rejects and this grammar treats as a conflict.
func attrOnce(attrs []xml.Attr, name string) (string, bool) {
	value, count := "", 0
	for _, attr := range attrs {
		if attr.Name.Space == "" && attr.Name.Local == name {
			if count == 0 {
				value = attr.Value
			}
			count++
		}
	}
	return value, count > 1
}
