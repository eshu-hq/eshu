// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryplan

import "fmt"

type pilotCursor struct {
	tokens []string
	pos    int
	kind   string
}

func (p *pilotCursor) peek() string {
	if p.pos >= len(p.tokens) {
		return ""
	}
	return p.tokens[p.pos]
}

func (p *pilotCursor) take(token string) bool {
	if p.peek() != token {
		return false
	}
	p.pos++
	return true
}

func (p *pilotCursor) need(token string) error {
	if p.take(token) {
		return nil
	}
	return fmt.Errorf("expected %s at token %d, got %s", token, p.pos, p.peek())
}

func (p *pilotCursor) name() error {
	if !pilotValidName(p.peek()) {
		return fmt.Errorf("expected identifier at token %d, got %s", p.pos, p.peek())
	}
	p.pos++
	return nil
}

func pilotStatementSyntax(tokens []string, kind string) error {
	p := &pilotCursor{tokens: tokens, kind: kind}
	var err error
	switch kind {
	case queryKindSQLReadModel:
		err = p.sqlSelect()
	case queryKindCypher:
		err = p.cypherMatch()
	default:
		return fmt.Errorf("unsupported pilot query kind %q", kind)
	}
	if err != nil {
		return err
	}
	if p.pos != len(tokens) {
		return fmt.Errorf("unsupported trailing pilot syntax at token %d: %s", p.pos, p.peek())
	}
	return nil
}

func (p *pilotCursor) sqlSelect() error {
	if err := p.need("SELECT"); err != nil {
		return err
	}
	if err := p.expressions(true); err != nil {
		return err
	}
	if err := p.need("FROM"); err != nil {
		return err
	}
	if err := p.tableAlias(); err != nil {
		return err
	}
	for p.take("JOIN") {
		if err := p.tableAlias(); err != nil {
			return err
		}
		if err := p.need("ON"); err != nil {
			return err
		}
		if err := p.expression(0); err != nil {
			return err
		}
	}
	if p.take("WHERE") {
		if err := p.expression(0); err != nil {
			return err
		}
	}
	if p.take("ORDER") {
		if err := p.need("BY"); err != nil {
			return err
		}
		if err := p.expressions(false); err != nil {
			return err
		}
	}
	if p.take("LIMIT") {
		if err := p.pageBound(); err != nil {
			return err
		}
	}
	return nil
}

func (p *pilotCursor) tableAlias() error {
	if err := p.name(); err != nil {
		return err
	}
	if p.take(".") {
		if err := p.name(); err != nil {
			return err
		}
	}
	if p.take("AS") {
		return p.name()
	}
	return nil
}

func (p *pilotCursor) cypherMatch() error {
	if err := p.need("MATCH"); err != nil {
		return err
	}
	if err := p.cypherNode(); err != nil {
		return err
	}
	for p.peek() == "-" || p.peek() == "<-" {
		if err := p.cypherEdge(); err != nil {
			return err
		}
		if err := p.cypherNode(); err != nil {
			return err
		}
	}
	if p.take("WHERE") {
		if err := p.expression(0); err != nil {
			return err
		}
	}
	if err := p.need("RETURN"); err != nil {
		return err
	}
	p.take("DISTINCT")
	if err := p.expressions(true); err != nil {
		return err
	}
	if p.take("ORDER") {
		if err := p.need("BY"); err != nil {
			return err
		}
		if err := p.expressions(false); err != nil {
			return err
		}
	}
	if p.take("SKIP") {
		if err := p.pageBound(); err != nil {
			return err
		}
	}
	if p.take("LIMIT") {
		if err := p.pageBound(); err != nil {
			return err
		}
	}
	return nil
}

func (p *pilotCursor) pageBound() error {
	token := p.peek()
	if token == "" || token[0] != '$' && !pilotDigits(token) {
		return fmt.Errorf("expected page-bound parameter or integer at token %d", p.pos)
	}
	p.pos++
	return nil
}

func (p *pilotCursor) cypherNode() error {
	if err := p.need("("); err != nil {
		return err
	}
	if err := p.name(); err != nil {
		return err
	}
	if err := p.need(":"); err != nil {
		return err
	}
	if err := p.name(); err != nil {
		return err
	}
	if p.take("{") {
		for {
			if err := p.name(); err != nil {
				return err
			}
			if err := p.need(":"); err != nil {
				return err
			}
			if err := p.expression(0); err != nil {
				return err
			}
			if !p.take(",") {
				break
			}
		}
		if err := p.need("}"); err != nil {
			return err
		}
	}
	return p.need(")")
}

func (p *pilotCursor) cypherEdge() error {
	first := p.peek()
	p.pos++
	if err := p.need("["); err != nil {
		return err
	}
	if p.peek() != ":" {
		if err := p.name(); err != nil {
			return err
		}
	}
	if err := p.need(":"); err != nil {
		return err
	}
	if err := p.name(); err != nil {
		return err
	}
	if p.take("*") {
		if !pilotDigits(p.peek()) {
			return fmt.Errorf("unbounded pilot relationship at token %d", p.pos)
		}
		p.pos++
		if p.take("..") {
			if !pilotDigits(p.peek()) {
				return fmt.Errorf("unbounded pilot relationship at token %d", p.pos)
			}
			p.pos++
		}
	}
	if err := p.need("]"); err != nil {
		return err
	}
	if first == "<-" {
		return p.need("-")
	}
	return p.need("->")
}
