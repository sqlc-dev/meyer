package parser

import (
	"strings"

	"github.com/sqlc-dev/meyer/ast"
	"github.com/sqlc-dev/meyer/token"
)

// parseCreateTrigger implements CREATE TRIGGER.
//
//	cmd ::= createkw trigger_decl BEGIN trigger_cmd_list END.
//	trigger_decl ::= temp TRIGGER ifnotexists nm dbnm trigger_time
//	                 trigger_event ON fullname foreach_clause when_clause.
//	trigger_time ::= BEFORE|AFTER. / INSTEAD OF. / .
//	trigger_event ::= DELETE|INSERT. / UPDATE. / UPDATE OF idlist.
//	foreach_clause ::= . / foreach_clause ::= FOR EACH ROW.
//	when_clause ::= . / when_clause ::= WHEN expr.
func (p *parser) parseCreateTrigger(start int, temp bool) ast.Stmt {
	p.expect(token.TRIGGER)
	n := &ast.CreateTriggerStmt{Temp: temp}
	n.IfNotExists = p.parseIfNotExists()
	n.Name = p.qualifiedName()
	switch p.cur().Kind {
	case token.BEFORE:
		p.advance()
		n.Time, n.HasTime = ast.TriggerBefore, true
	case token.AFTER:
		p.advance()
		n.Time, n.HasTime = ast.TriggerAfter, true
	case token.INSTEAD:
		p.advance()
		p.expect(token.OF)
		n.Time, n.HasTime = ast.TriggerInsteadOf, true
	}
	switch p.cur().Kind {
	case token.DELETE, token.INSERT:
		n.Event = strings.ToUpper(p.advance().Text)
	case token.UPDATE:
		n.Event = strings.ToUpper(p.advance().Text)
		if p.accept(token.OF) {
			n.UpdateOf = p.parseIDList()
		}
	default:
		p.syntaxError()
	}
	p.expect(token.ON)
	n.Table = p.qualifiedName()
	if p.accept(token.FOR) {
		p.expect(token.EACH)
		p.expect(token.ROW)
		n.ForEachRow = true
	}
	if p.accept(token.WHEN) {
		n.When = p.parseExpr(precLowest)
	}
	p.expect(token.BEGIN)
	// trigger_cmd_list ::= trigger_cmd SEMI. / trigger_cmd_list trigger_cmd SEMI.
	// The list is non-empty and every command carries its own semicolon.
	for {
		n.Body = append(n.Body, p.parseTriggerCmd())
		p.expect(token.SEMI)
		if p.at(token.END) {
			break
		}
	}
	p.expect(token.END)
	n.Span = p.span(start)
	return n
}

// parseTriggerCmd implements the restricted statement forms of a trigger
// body. They differ from the top-level statements in three ways: no WITH
// prefix; UPDATE and DELETE take where_opt rather than where_opt_ret, so
// they have no RETURNING, while INSERT still does because "upsert" carries
// one; and INSERT has no DEFAULT VALUES form.
//
//	trigger_cmd ::= UPDATE orconf xfullname tridxby SET setlist from where_opt.
//	trigger_cmd ::= insert_cmd INTO xfullname idlist_opt select upsert.
//	trigger_cmd ::= DELETE FROM xfullname tridxby where_opt.
//	trigger_cmd ::= select.
//
// "tridxby" is "indexed_opt" with a tailored error message, which SQLite
// raises from a grammar action rather than the grammar, so meyer accepts it
// wherever the top-level statements do.
func (p *parser) parseTriggerCmd() ast.Stmt {
	start := p.cur().Pos
	switch p.cur().Kind {
	case token.UPDATE:
		n := &ast.UpdateStmt{}
		p.updateCore(n)
		if p.accept(token.WHERE) { // where_opt, not where_opt_ret
			n.Where = p.parseExpr(precLowest)
		}
		n.Span = p.span(start)
		return n
	case token.INSERT, token.REPLACE:
		n := &ast.InsertStmt{}
		p.insertTarget(n) // no DEFAULT VALUES form here
		n.Select = p.parseSelect()
		p.parseUpsert(n)
		n.Span = p.span(start)
		return n
	case token.DELETE:
		n := &ast.DeleteStmt{}
		p.deleteCore(n)
		if p.accept(token.WHERE) { // where_opt, not where_opt_ret
			n.Where = p.parseExpr(precLowest)
		}
		n.Span = p.span(start)
		return n
	}
	return p.parseSelect()
}
