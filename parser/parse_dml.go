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
	p.insertTarget(n)
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

// insertTarget reads "insert_cmd INTO xfullname idlist_opt", the part an
// INSERT shares with a trigger body's insert.
//
//	insert_cmd ::= INSERT orconf. / insert_cmd ::= REPLACE.
//	idlist_opt ::= . / idlist_opt ::= LP idlist RP.
func (p *parser) insertTarget(n *ast.InsertStmt) {
	if p.accept(token.REPLACE) {
		n.Replace = true
	} else {
		p.expect(token.INSERT)
		n.OrConflict = p.parseOrConflict()
	}
	p.expect(token.INTO)
	n.Table, n.Alias = p.xfullname()
	// A "select" never starts with "(", so a leading "(" is the column list.
	if p.at(token.LP) {
		p.advance()
		n.Columns = p.parseIDList()
		p.expect(token.RP)
	}
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
//	cmd ::= with UPDATE orconf xfullname indexed_opt SET setlist from
//	        where_opt_ret orderby_opt limit_opt.
func (p *parser) parseUpdate(with *ast.With, start int) ast.Stmt {
	n := &ast.UpdateStmt{With: with}
	p.updateCore(n)
	n.Where, n.Returning = p.parseWhereOptRet()
	n.OrderBy, n.Limit = p.parseUpdateDeleteLimit()
	n.Span = p.span(start)
	return n
}

// updateCore reads "UPDATE orconf xfullname indexed_opt SET setlist from",
// everything an UPDATE shares with a trigger body's update. Only the WHERE
// clause differs: the trigger form takes where_opt, with no RETURNING.
func (p *parser) updateCore(n *ast.UpdateStmt) {
	p.expect(token.UPDATE)
	n.OrConflict = p.parseOrConflict()
	n.Table, n.Alias = p.xfullname()
	n.IndexedBy, n.NotIndexed = p.parseIndexedOpt()
	p.expect(token.SET)
	n.Set = p.parseSetList()
	if p.accept(token.FROM) {
		n.From = p.parseFromList()
	}
}

// parseDelete implements the DELETE command.
//
//	cmd ::= with DELETE FROM xfullname indexed_opt where_opt_ret.
//	cmd ::= with DELETE FROM xfullname indexed_opt where_opt_ret orderby_opt
//	        limit_opt.
func (p *parser) parseDelete(with *ast.With, start int) ast.Stmt {
	n := &ast.DeleteStmt{With: with}
	p.deleteCore(n)
	n.Where, n.Returning = p.parseWhereOptRet()
	n.OrderBy, n.Limit = p.parseUpdateDeleteLimit()
	n.Span = p.span(start)
	return n
}

// parseUpdateDeleteLimit implements the "orderby_opt limit_opt" tail that
// UPDATE and DELETE grow when SQLite is compiled with
// SQLITE_ENABLE_UPDATE_DELETE_LIMIT. Without Options.UpdateDeleteLimit the
// pinned build's rules are in force, where the statement simply ends: ORDER
// or LIMIT is then a token no rule can shift, and the caller's own end-of-
// statement check reports it.
//
// A build with SQLITE_UDL_CAPABLE_PARSER but not the option itself is a
// third behaviour -- it parses the clauses and then rejects them from a
// grammar action, "syntax error near \"ORDER BY\"" -- which meyer does not
// model: it accepts exactly the SQL its caller's database accepts, and no
// database accepts less than one of these two.
//
//	orderby_opt ::= . / orderby_opt ::= ORDER BY sortlist.
//	limit_opt ::= . / LIMIT expr. / LIMIT expr OFFSET expr. / LIMIT expr COMMA expr.
func (p *parser) parseUpdateDeleteLimit() ([]*ast.OrderingTerm, *ast.Limit) {
	if !p.opts.UpdateDeleteLimit {
		return nil, nil
	}
	var order []*ast.OrderingTerm
	if p.at(token.ORDER) {
		p.advance()
		p.expect(token.BY)
		order = p.parseSortList()
	}
	return order, p.parseLimit()
}

// deleteCore reads "DELETE FROM xfullname indexed_opt", everything a DELETE
// shares with a trigger body's delete.
func (p *parser) deleteCore(n *ast.DeleteStmt) {
	p.expect(token.DELETE)
	p.expect(token.FROM)
	n.Table, n.Alias = p.xfullname()
	n.IndexedBy, n.NotIndexed = p.parseIndexedOpt()
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
