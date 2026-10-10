// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryplan

import "fmt"

func (p *pilotCursor) expressions(aliases bool) error {
	for {
		if err := p.expression(0); err != nil {
			return err
		}
		if aliases && p.take("AS") {
			if err := p.name(); err != nil {
				return err
			}
		}
		if !p.take(",") {
			return nil
		}
	}
}

// expression consumes the bounded SQL/Cypher expression grammar used by the
// pilots. An unrecognized operator or clause remains unread and fails the
// enclosing statement's exact end check.
func (p *pilotCursor) expression(minPrecedence int) error {
	if p.take("NOT") || p.take("+") || p.take("-") {
		if err := p.expression(5); err != nil {
			return err
		}
	} else if err := p.primary(); err != nil {
		return err
	}
	for {
		if p.take(".") || p.kind == queryKindSQLReadModel && p.take("->>") {
			if err := p.nameOrLiteral(); err != nil {
				return err
			}
			continue
		}
		if p.kind == queryKindSQLReadModel && p.take("::") {
			if err := p.name(); err != nil {
				return err
			}
			if p.take("[") {
				if err := p.need("]"); err != nil {
					return err
				}
			}
			continue
		}
		precedence := pilotOperatorPrecedence(p.peek())
		if precedence == 0 || precedence < minPrecedence {
			return nil
		}
		operator := p.peek()
		p.pos++
		if operator == "IS" {
			p.take("NOT")
			if err := p.need("NULL"); err != nil {
				return err
			}
			continue
		}
		if err := p.expression(precedence + 1); err != nil {
			return err
		}
	}
}

func pilotOperatorPrecedence(token string) int {
	switch token {
	case "OR":
		return 1
	case "AND":
		return 2
	case "=", "<", ">", "<=", ">=", "<>", "!=", "IN", "IS":
		return 3
	case "+", "-":
		return 4
	default:
		return 0
	}
}

func (p *pilotCursor) primary() error {
	if p.take("(") {
		if p.peek() == "SELECT" {
			if err := p.sqlSelect(); err != nil {
				return err
			}
		} else {
			if err := p.expression(0); err != nil {
				return err
			}
			for p.kind == queryKindSQLReadModel && p.take(",") {
				if err := p.expression(0); err != nil {
					return err
				}
			}
		}
		return p.need(")")
	}
	start := p.pos
	if err := p.nameOrLiteral(); err != nil {
		return err
	}
	if p.take("(") {
		if p.tokens[start] != "COALESCE" && (p.kind != queryKindSQLReadModel || p.tokens[start] != "ANY") {
			return fmt.Errorf("unsupported pilot function %s", p.tokens[start])
		}
		if !p.take(")") {
			for {
				if err := p.expression(0); err != nil {
					return err
				}
				if !p.take(",") {
					break
				}
			}
			return p.need(")")
		}
	}
	return nil
}

func (p *pilotCursor) nameOrLiteral() error {
	token := p.peek()
	if token == "" || token == ")" || token == "]" || token == "}" || token == "," ||
		token == "RETURN" || token == "WHERE" || token == "LIMIT" || token == "ORDER" ||
		token == "SKIP" || token == "FROM" || token == "JOIN" || token == "ON" || token == "AS" {
		return fmt.Errorf("expected expression at token %d, got %s", p.pos, token)
	}
	if token[0] == '$' || token[0] == '\'' || token[0] == '"' || token[0] == '`' ||
		pilotValidName(token) || pilotDigits(token) {
		p.pos++
		return nil
	}
	return fmt.Errorf("unsupported expression token %s at %d", token, p.pos)
}
