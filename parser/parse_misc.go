package parser

import (
	"strings"

	"github.com/sqlc-dev/meyer/ast"
	"github.com/sqlc-dev/meyer/token"
)

// parseBegin implements BEGIN.
//
//	cmd ::= BEGIN transtype trans_opt.
//	transtype ::= . / DEFERRED. / IMMEDIATE. / EXCLUSIVE.
func (p *parser) parseBegin() ast.Stmt {
	start := p.expect(token.BEGIN).Pos
	n := &ast.BeginStmt{}
	switch p.cur().Kind {
	case token.DEFERRED:
		p.advance()
		n.Type, n.HasType = ast.TxnDeferred, true
	case token.IMMEDIATE:
		p.advance()
		n.Type, n.HasType = ast.TxnImmediate, true
	case token.EXCLUSIVE:
		p.advance()
		n.Type, n.HasType = ast.TxnExclusive, true
	}
	n.Name = p.parseTransOpt()
	n.Span = p.span(start)
	return n
}

// parseTransOpt implements "trans_opt".
//
//	trans_opt ::= . / TRANSACTION. / TRANSACTION nm.
func (p *parser) parseTransOpt() *ast.Ident {
	if !p.accept(token.TRANSACTION) {
		return nil
	}
	if token.IsName(p.cur().Kind) {
		return p.expectName()
	}
	return nil
}

// parseCommit implements COMMIT and its END synonym.
//
//	cmd ::= COMMIT|END trans_opt.
func (p *parser) parseCommit() ast.Stmt {
	t := p.advance()
	n := &ast.CommitStmt{Keyword: strings.ToUpper(t.Text)}
	n.Name = p.parseTransOpt()
	n.Span = p.span(t.Pos)
	return n
}

// parseRollback implements both ROLLBACK forms.
//
//	cmd ::= ROLLBACK trans_opt.
//	cmd ::= ROLLBACK trans_opt TO savepoint_opt nm.
func (p *parser) parseRollback() ast.Stmt {
	start := p.expect(token.ROLLBACK).Pos
	n := &ast.RollbackStmt{}
	n.Name = p.parseTransOpt()
	if p.accept(token.TO) {
		p.accept(token.SAVEPOINT) // savepoint_opt ::= SAVEPOINT. / .
		n.Savepoint = p.expectName()
	}
	n.Span = p.span(start)
	return n
}

// parseSavepoint implements SAVEPOINT.
//
//	cmd ::= SAVEPOINT nm.
func (p *parser) parseSavepoint() ast.Stmt {
	start := p.expect(token.SAVEPOINT).Pos
	n := &ast.SavepointStmt{Name: p.expectName()}
	n.Span = p.span(start)
	return n
}

// parseRelease implements RELEASE.
//
//	cmd ::= RELEASE savepoint_opt nm.
func (p *parser) parseRelease() ast.Stmt {
	start := p.expect(token.RELEASE).Pos
	p.accept(token.SAVEPOINT)
	n := &ast.ReleaseStmt{Name: p.expectName()}
	n.Span = p.span(start)
	return n
}

// parseAttach implements ATTACH.
//
//	cmd ::= ATTACH database_kw_opt expr AS expr key_opt.
//	database_kw_opt ::= DATABASE. / database_kw_opt ::= .
//	key_opt ::= . / key_opt ::= KEY expr.
func (p *parser) parseAttach() ast.Stmt {
	start := p.expect(token.ATTACH).Pos
	n := &ast.AttachStmt{HasDatabase: p.accept(token.DATABASE)}
	n.File = p.parseExpr(precLowest)
	p.expect(token.AS)
	n.Name = p.parseExpr(precLowest)
	if p.accept(token.KEY) {
		n.Key = p.parseExpr(precLowest)
	}
	n.Span = p.span(start)
	return n
}

// parseDetach implements DETACH.
//
//	cmd ::= DETACH database_kw_opt expr.
func (p *parser) parseDetach() ast.Stmt {
	start := p.expect(token.DETACH).Pos
	n := &ast.DetachStmt{HasDatabase: p.accept(token.DATABASE)}
	n.Name = p.parseExpr(precLowest)
	n.Span = p.span(start)
	return n
}

// parsePragma implements PRAGMA in all five spellings.
//
//	cmd ::= PRAGMA nm dbnm.
//	cmd ::= PRAGMA nm dbnm EQ nmnum. / PRAGMA nm dbnm EQ minus_num.
//	cmd ::= PRAGMA nm dbnm LP nmnum RP. / PRAGMA nm dbnm LP minus_num RP.
func (p *parser) parsePragma() ast.Stmt {
	start := p.expect(token.PRAGMA).Pos
	n := &ast.PragmaStmt{Name: p.qualifiedName()}
	switch {
	case p.accept(token.EQ):
		n.Value, n.HasValue = p.parsePragmaValue(), true
	case p.at(token.LP):
		p.advance()
		n.Value, n.HasValue, n.Paren = p.parsePragmaValue(), true, true
		p.expect(token.RP)
	}
	n.Span = p.span(start)
	return n
}

// parsePragmaValue implements "nmnum" and "minus_num".
//
//	nmnum ::= plus_num. / nm. / ON. / DELETE. / DEFAULT.
//	plus_num ::= PLUS number. / number.    minus_num ::= MINUS number.
//	number ::= INTEGER|FLOAT.
func (p *parser) parsePragmaValue() string {
	switch p.cur().Kind {
	case token.PLUS, token.MINUS:
		sign := p.advance().Text
		if !p.at(token.INTEGER) && !p.at(token.FLOAT) {
			p.syntaxError()
		}
		return sign + p.advance().Text
	case token.INTEGER, token.FLOAT, token.ON, token.DELETE, token.DEFAULT:
		return p.advance().Text
	}
	return p.expectName().Raw
}

// parseVacuum implements VACUUM.
//
//	cmd ::= VACUUM vinto. / cmd ::= VACUUM nm vinto.
//	vinto ::= INTO expr. / vinto ::= .
func (p *parser) parseVacuum() ast.Stmt {
	start := p.expect(token.VACUUM).Pos
	n := &ast.VacuumStmt{}
	if token.IsName(p.cur().Kind) {
		n.Schema = p.expectName()
	}
	if p.accept(token.INTO) {
		n.Into = p.parseExpr(precLowest)
	}
	n.Span = p.span(start)
	return n
}

// parseAnalyze implements ANALYZE.
//
//	cmd ::= ANALYZE. / cmd ::= ANALYZE nm dbnm.
func (p *parser) parseAnalyze() ast.Stmt {
	start := p.expect(token.ANALYZE).Pos
	n := &ast.AnalyzeStmt{}
	if token.IsName(p.cur().Kind) {
		n.Name = p.qualifiedName()
	}
	n.Span = p.span(start)
	return n
}

// parseReindex implements REINDEX.
//
//	cmd ::= REINDEX. / cmd ::= REINDEX nm dbnm.
func (p *parser) parseReindex() ast.Stmt {
	start := p.expect(token.REINDEX).Pos
	n := &ast.ReindexStmt{}
	if token.IsName(p.cur().Kind) {
		n.Name = p.qualifiedName()
	}
	n.Span = p.span(start)
	return n
}
