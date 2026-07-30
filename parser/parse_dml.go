package parser

import (
	"github.com/sqlc-dev/meyer/ast"
	"github.com/sqlc-dev/meyer/token"
)

// parseInsert implements the INSERT and REPLACE commands.
//
//	cmd ::= with insert_cmd INTO xfullname idlist_opt select upsert.
//	cmd ::= with insert_cmd INTO xfullname idlist_opt DEFAULT VALUES returning.
//	insert_cmd ::= INSERT orconf. / insert_cmd ::= REPLACE.
func (p *parser) parseInsert(with *ast.With, start int) ast.Stmt {
	n := &ast.InsertStmt{With: with}
	if p.accept(token.REPLACE) {
		n.Replace = true
	} else {
		p.expect(token.INSERT)
		n.OrConflict = p.parseOrConflict()
	}
	p.expect(token.INTO)
	n.Table, n.Alias = p.xfullname()
	// idlist_opt ::= . / idlist_opt ::= LP idlist RP. A "select" never
	// starts with "(", so a leading "(" is always the column list.
	if p.at(token.LP) {
		p.advance()
		n.Columns = p.parseIDList()
		p.expect(token.RP)
	}
	if p.at(token.DEFAULT) {
		p.advance()
		p.expect(token.VALUES)
		n.DefaultValues = true
		n.Returning = p.parseReturning()
		n.Span = p.span(start)
		return n
	}
	n.Select = p.parseSelect()
	p.parseUpsert(n)
	n.Span = p.span(start)
	return n
}

// parseOrConflict implements "orconf".
//
//	orconf ::= . / orconf ::= OR resolvetype.
//	resolvetype ::= raisetype. / IGNORE. / REPLACE.
//	raisetype ::= ROLLBACK. / ABORT. / FAIL.
func (p *parser) parseOrConflict() ast.ConflictAction {
	if !p.accept(token.OR) {
		return ast.ConflictDefault
	}
	return p.parseResolveType()
}

func (p *parser) parseResolveType() ast.ConflictAction {
	switch p.cur().Kind {
	case token.ROLLBACK:
		p.advance()
		return ast.ConflictRollback
	case token.ABORT:
		p.advance()
		return ast.ConflictAbort
	case token.FAIL:
		p.advance()
		return ast.ConflictFail
	case token.IGNORE:
		p.advance()
		return ast.ConflictIgnore
	case token.REPLACE:
		p.advance()
		return ast.ConflictReplace
	}
	p.syntaxError()
	return ast.ConflictDefault
}

// parseUpsert implements the "upsert" chain and the trailing RETURNING.
//
//	upsert ::= . / upsert ::= RETURNING selcollist.
//	upsert ::= ON CONFLICT LP sortlist RP where_opt DO UPDATE SET setlist
//	           where_opt upsert.
//	upsert ::= ON CONFLICT LP sortlist RP where_opt DO NOTHING upsert.
//	upsert ::= ON CONFLICT DO NOTHING returning.
//	upsert ::= ON CONFLICT DO UPDATE SET setlist where_opt returning.
//
// Only a targeted clause (one with a conflict target) may be followed by
// another; an untargeted one ends the chain.
func (p *parser) parseUpsert(n *ast.InsertStmt) {
	for p.at(token.ON) {
		start := p.advance().Pos
		p.expect(token.CONFLICT)
		u := &ast.Upsert{}
		targeted := false
		if p.at(token.LP) {
			p.advance()
			u.Target = p.parseSortList()
			p.expect(token.RP)
			if p.accept(token.WHERE) {
				u.TargetWhere = p.parseExpr(precLowest)
			}
			targeted = true
		}
		p.expect(token.DO)
		if p.accept(token.NOTHING) {
			u.DoNothing = true
		} else {
			p.expect(token.UPDATE)
			p.expect(token.SET)
			u.Set = p.parseSetList()
			if p.accept(token.WHERE) {
				u.Where = p.parseExpr(precLowest)
			}
		}
		u.Span = p.span(start)
		n.Upserts = append(n.Upserts, u)
		if !targeted {
			break
		}
	}
	n.Returning = p.parseReturning()
}

// parseReturning implements the "returning" nonterminal.
//
//	returning ::= RETURNING selcollist. / returning ::= .
func (p *parser) parseReturning() []*ast.ResultColumn {
	if !p.accept(token.RETURNING) {
		return nil
	}
	return p.parseResultColumns()
}

// parseSetList implements "setlist".
//
//	setlist ::= nm EQ expr. / setlist ::= LP idlist RP EQ expr.
//	setlist ::= setlist COMMA nm EQ expr. / setlist COMMA LP idlist RP EQ expr.
func (p *parser) parseSetList() []*ast.SetPair {
	var out []*ast.SetPair
	for {
		start := p.cur().Pos
		pair := &ast.SetPair{}
		if p.at(token.LP) {
			p.advance()
			pair.Columns = p.parseIDList()
			pair.Paren = true
			p.expect(token.RP)
		} else {
			pair.Columns = []*ast.Ident{p.expectName()}
		}
		p.expect(token.EQ)
		pair.Value = p.parseExpr(precLowest)
		pair.Span = p.span(start)
		out = append(out, pair)
		if !p.accept(token.COMMA) {
			return out
		}
	}
}

// parseUpdate implements the UPDATE command.
//
//	cmd ::= with UPDATE orconf xfullname indexed_opt SET setlist from
//	        where_opt_ret.
func (p *parser) parseUpdate(with *ast.With, start int) ast.Stmt {
	p.expect(token.UPDATE)
	n := &ast.UpdateStmt{With: with}
	n.OrConflict = p.parseOrConflict()
	n.Table, n.Alias = p.xfullname()
	n.IndexedBy, n.NotIndexed = p.parseIndexedOpt()
	p.expect(token.SET)
	n.Set = p.parseSetList()
	if p.accept(token.FROM) {
		n.From = p.parseFromList()
	}
	n.Where, n.Returning = p.parseWhereOptRet()
	n.Span = p.span(start)
	return n
}

// parseDelete implements the DELETE command.
//
//	cmd ::= with DELETE FROM xfullname indexed_opt where_opt_ret.
func (p *parser) parseDelete(with *ast.With, start int) ast.Stmt {
	p.expect(token.DELETE)
	p.expect(token.FROM)
	n := &ast.DeleteStmt{With: with}
	n.Table, n.Alias = p.xfullname()
	n.IndexedBy, n.NotIndexed = p.parseIndexedOpt()
	n.Where, n.Returning = p.parseWhereOptRet()
	n.Span = p.span(start)
	return n
}

// parseIndexedOpt implements "indexed_opt".
//
// NOT here can only begin "NOT INDEXED": nothing else that may follow a
// table reference starts with it. So NOT is consumed before the check, which
// is what SQLite does -- it shifts the NOT and only then discovers there is
// no INDEXED, so "FROM t NOT NOT INDEXED" is reported at the second NOT.
//
//	indexed_opt ::= . / indexed_by ::= INDEXED BY nm. / indexed_by ::= NOT INDEXED.
func (p *parser) parseIndexedOpt() (*ast.Ident, bool) {
	switch {
	case p.at(token.INDEXED):
		p.advance()
		p.expect(token.BY)
		return p.expectName(), false
	case p.at(token.NOT):
		p.advance()
		p.expect(token.INDEXED)
		return nil, true
	}
	return nil, false
}

// parseWhereOptRet implements "where_opt_ret".
//
//	where_opt_ret ::= . / WHERE expr. / RETURNING selcollist.
//	where_opt_ret ::= WHERE expr RETURNING selcollist.
func (p *parser) parseWhereOptRet() (ast.Expr, []*ast.ResultColumn) {
	var where ast.Expr
	if p.accept(token.WHERE) {
		where = p.parseExpr(precLowest)
	}
	return where, p.parseReturning()
}
