package parser

import (
	"strings"

	"github.com/sqlc-dev/meyer/ast"
	"github.com/sqlc-dev/meyer/token"
)

// parseSelectStmtOrDML handles the statement forms that may begin with WITH.
// parse.y spreads the WITH clause over four rules: "select ::= WITH wqlist
// selectnowith" for queries, and a leading "with" nonterminal on the INSERT,
// UPDATE and DELETE commands.
func (p *parser) parseSelectStmtOrDML() ast.Stmt {
	if !p.at(token.WITH) {
		return p.parseSelect()
	}
	start := p.cur().Pos
	with := p.parseWith()
	switch p.cur().Kind {
	case token.SELECT, token.VALUES:
		sel := p.parseSelectNoWith()
		sel.With = with
		sel.Span = ast.Span{Start: start, Stop: p.prevEnd()}
		return sel
	case token.INSERT, token.REPLACE:
		return p.parseInsert(with, start)
	case token.UPDATE:
		return p.parseUpdate(with, start)
	case token.DELETE:
		return p.parseDelete(with, start)
	}
	p.syntaxError()
	return nil
}

// parseSelect implements the "select" nonterminal.
//
//	select ::= WITH wqlist selectnowith. / WITH RECURSIVE wqlist selectnowith.
//	select ::= selectnowith.
func (p *parser) parseSelect() *ast.SelectStmt {
	start := p.cur().Pos
	var with *ast.With
	if p.at(token.WITH) {
		with = p.parseWith()
	}
	sel := p.parseSelectNoWith()
	sel.With = with
	sel.Span = ast.Span{Start: start, Stop: p.prevEnd()}
	return sel
}

// parseSelectNoWith implements the compound-select chain.
//
//	selectnowith ::= oneselect.
//	selectnowith ::= selectnowith multiselect_op oneselect.
//	multiselect_op ::= UNION. / UNION ALL. / EXCEPT|INTERSECT.
func (p *parser) parseSelectNoWith() *ast.SelectStmt {
	p.enter()
	defer p.leave()
	start := p.cur().Pos
	n := &ast.SelectStmt{Cores: []ast.SelectCore{p.parseOneSelect()}}
	for {
		t := p.cur()
		var op ast.CompoundOp
		switch t.Kind {
		case token.UNION:
			p.advance()
			op = ast.CompoundOp{Op: "UNION"}
			if p.at(token.ALL) {
				p.advance()
				op.All = true
			}
		case token.EXCEPT, token.INTERSECT:
			p.advance()
			op = ast.CompoundOp{Op: strings.ToUpper(t.Text)}
		default:
			n.Span = ast.Span{Start: start, Stop: p.prevEnd()}
			return n
		}
		op.Span = p.span(t.Pos)
		n.Ops = append(n.Ops, op)
		n.Cores = append(n.Cores, p.parseOneSelect())
	}
}

// parseOneSelect implements the "oneselect" nonterminal: a SELECT core or a
// VALUES clause.
//
//	oneselect ::= SELECT distinct selcollist from where_opt groupby_opt
//	              having_opt [window_clause] orderby_opt limit_opt.
//	oneselect ::= values. / oneselect ::= mvalues.
func (p *parser) parseOneSelect() ast.SelectCore {
	if p.at(token.VALUES) {
		return p.parseValues()
	}
	start := p.expect(token.SELECT).Pos
	n := &ast.SelectQuery{}
	switch p.cur().Kind { // distinct ::= DISTINCT. / ALL. / .
	case token.DISTINCT:
		p.advance()
		n.Distinct = ast.DistinctDistinct
	case token.ALL:
		p.advance()
		n.Distinct = ast.DistinctAll
	}
	n.Columns = p.parseResultColumns()
	if p.accept(token.FROM) { // from ::= . / from ::= FROM seltablist.
		n.From = p.parseFromList()
	}
	if p.accept(token.WHERE) {
		n.Where = p.parseExpr(precLowest)
	}
	if p.at(token.GROUP) {
		p.advance()
		p.expect(token.BY)
		n.GroupBy = p.parseExprList()
	}
	if p.accept(token.HAVING) {
		n.Having = p.parseExpr(precLowest)
	}
	if p.at(token.WINDOW) { // window_clause ::= WINDOW windowdefn_list.
		p.advance()
		n.Windows = p.parseWindowDefnList()
	}
	if p.at(token.ORDER) {
		p.advance()
		p.expect(token.BY)
		n.OrderBy = p.parseSortList()
	}
	n.Limit = p.parseLimit()
	n.Span = p.span(start)
	return n
}

// parseValues implements the single- and multi-row VALUES clauses.
//
//	values ::= VALUES LP nexprlist RP.
//	mvalues ::= values COMMA LP nexprlist RP. / mvalues COMMA LP nexprlist RP.
func (p *parser) parseValues() ast.SelectCore {
	start := p.expect(token.VALUES).Pos
	n := &ast.ValuesClause{}
	for {
		p.expect(token.LP)
		n.Rows = append(n.Rows, p.parseExprList())
		p.expect(token.RP)
		if !p.at(token.COMMA) {
			break
		}
		p.advance()
	}
	n.Span = p.span(start)
	return n
}

// parseResultColumns implements "selcollist".
//
//	selcollist ::= sclp scanpt expr scanpt as.
//	selcollist ::= sclp scanpt STAR. / selcollist ::= sclp scanpt nm DOT STAR.
func (p *parser) parseResultColumns() []*ast.ResultColumn {
	var out []*ast.ResultColumn
	for {
		start := p.cur().Pos
		c := &ast.ResultColumn{}
		switch {
		case p.at(token.STAR):
			t := p.advance()
			c.Expr = &ast.Star{Span: spanOf(t)}
		case token.IsName(p.cur().Kind) && p.peek(1).Kind == token.DOT && p.peek(2).Kind == token.STAR:
			tbl := p.expectName()
			p.advance() // DOT
			p.advance() // STAR
			c.Expr = &ast.Star{Span: p.span(start), Table: tbl}
		default:
			c.Expr = p.parseExpr(precLowest)
			c.Alias, c.HasAs = p.parseAlias()
		}
		c.Span = p.span(start)
		out = append(out, c)
		if !p.accept(token.COMMA) {
			return out
		}
	}
}

// parseAlias implements the "as" nonterminal. A bare alias must be an ID or
// a STRING: the "ids" class excludes INDEXED and the join keywords, so
// "FROM t LEFT JOIN u" cannot read LEFT as an alias.
//
//	as ::= AS nm. / as ::= ids. / as ::= .
func (p *parser) parseAlias() (*ast.Ident, bool) {
	if p.accept(token.AS) {
		return p.expectName(), true
	}
	if token.IsIDS(p.cur().Kind) {
		return identOf(p.advance()), false
	}
	return nil, false
}

// parseFromList implements "seltablist": a flat list of source items, each
// carrying the operator that attaches it to the one before.
func (p *parser) parseFromList() []*ast.TableRef {
	list := []*ast.TableRef{p.parseTableRef(nil)}
	for {
		op := p.tryJoinOperator()
		if op == nil {
			return list
		}
		list = append(list, p.parseTableRef(op))
	}
}

// tryJoinOperator implements "joinop", returning nil when the FROM list
// ends. sqlite3JoinType rejects unknown keyword combinations, but it does so
// after parsing, so any names are accepted here.
//
//	joinop ::= COMMA|JOIN. / JOIN_KW JOIN.
//	joinop ::= JOIN_KW nm JOIN. / JOIN_KW nm nm JOIN.
func (p *parser) tryJoinOperator() *ast.JoinOperator {
	t := p.cur()
	switch t.Kind {
	case token.COMMA:
		p.advance()
		return &ast.JoinOperator{Span: spanOf(t), Type: ast.JoinInner | ast.JoinComma}
	case token.JOIN:
		p.advance()
		return &ast.JoinOperator{Span: spanOf(t), Type: ast.JoinInner, Words: []string{t.Text}}
	case token.JOIN_KW:
		words := []string{p.advance().Text}
		for i := 0; i < 2 && !p.at(token.JOIN); i++ {
			words = append(words, p.expectName().Raw)
		}
		p.expect(token.JOIN)
		words = append(words, "JOIN")
		return &ast.JoinOperator{Span: p.span(t.Pos), Type: joinTypeOf(words), Words: words}
	}
	return nil
}

// joinTypeOf mirrors sqlite3JoinType's keyword-to-bitmask mapping. Invalid
// combinations are reported by SQLite after parsing, so they are simply
// recorded here.
func joinTypeOf(words []string) ast.JoinType {
	var t ast.JoinType
	for _, w := range words {
		switch strings.ToUpper(w) {
		case "NATURAL":
			t |= ast.JoinNatural
		case "LEFT":
			t |= ast.JoinLeft | ast.JoinOuter
		case "RIGHT":
			t |= ast.JoinRight | ast.JoinOuter
		case "FULL":
			t |= ast.JoinLeft | ast.JoinRight | ast.JoinOuter
		case "OUTER":
			t |= ast.JoinOuter
		case "INNER":
			t |= ast.JoinInner
		case "CROSS":
			t |= ast.JoinInner | ast.JoinCross
		}
	}
	return t
}

// parseTableRef implements one "seltablist" item.
//
//	seltablist ::= stl_prefix nm dbnm as on_using.
//	seltablist ::= stl_prefix nm dbnm as indexed_by on_using.
//	seltablist ::= stl_prefix nm dbnm LP exprlist RP as on_using.
//	seltablist ::= stl_prefix LP select RP as on_using.
//	seltablist ::= stl_prefix LP seltablist RP as on_using.
func (p *parser) parseTableRef(join *ast.JoinOperator) *ast.TableRef {
	p.enter()
	defer p.leave()
	start := p.cur().Pos
	if join != nil {
		start = join.Pos()
	}
	n := &ast.TableRef{Join: join}
	if p.at(token.LP) {
		p.advance()
		// A "select" can only begin with SELECT, VALUES or WITH, so one
		// token of lookahead separates a subquery from a nested join list.
		if p.atSelect() {
			n.Select = p.parseSelect()
		} else {
			n.List = p.parseFromList()
		}
		p.expect(token.RP)
		n.Alias, n.HasAs = p.parseAlias()
		p.parseOnUsing(n)
		n.Span = p.span(start)
		return n
	}
	n.Name = p.qualifiedName()
	if p.at(token.LP) { // table-valued function
		p.advance()
		n.HasArgs = true
		if !p.at(token.RP) {
			n.Args = p.parseExprList()
		}
		p.expect(token.RP)
		n.Alias, n.HasAs = p.parseAlias()
		p.parseOnUsing(n)
		n.Span = p.span(start)
		return n
	}
	n.Alias, n.HasAs = p.parseAlias()
	// indexed_by ::= INDEXED BY nm. / indexed_by ::= NOT INDEXED.
	if p.at(token.INDEXED) {
		p.advance()
		p.expect(token.BY)
		n.IndexedBy = p.expectName()
	} else if p.at(token.NOT) && p.peek(1).Kind == token.INDEXED {
		p.advance()
		p.advance()
		n.NotIndexed = true
	}
	p.parseOnUsing(n)
	n.Span = p.span(start)
	return n
}

// parseOnUsing implements "on_using". The [OR] precedence mark on the empty
// alternative makes the parser prefer shifting ON into the join over
// reducing an empty constraint, which is why "INSERT ... JOIN b ON CONFLICT"
// reads CONFLICT as the join condition rather than starting an upsert.
//
//	on_using ::= ON expr. / USING LP idlist RP. / . [OR]
func (p *parser) parseOnUsing(n *ast.TableRef) {
	switch {
	case p.accept(token.ON):
		n.On = p.parseExpr(precLowest)
	case p.accept(token.USING):
		p.expect(token.LP)
		n.Using = p.parseIDList()
		p.expect(token.RP)
	}
}

// parseIDList implements "idlist": one or more comma-separated names.
//
//	idlist ::= idlist COMMA nm. / idlist ::= nm.
func (p *parser) parseIDList() []*ast.Ident {
	out := []*ast.Ident{p.expectName()}
	for p.accept(token.COMMA) {
		out = append(out, p.expectName())
	}
	return out
}

// parseSortList implements "sortlist".
//
//	sortlist ::= sortlist COMMA expr sortorder nulls. / expr sortorder nulls.
//	sortorder ::= ASC. / DESC. / .
//	nulls ::= NULLS FIRST. / NULLS LAST. / .
func (p *parser) parseSortList() []*ast.OrderingTerm {
	var out []*ast.OrderingTerm
	for {
		start := p.cur().Pos
		t := &ast.OrderingTerm{Expr: p.parseExpr(precLowest)}
		switch {
		case p.accept(token.ASC):
			t.Order = ast.SortAsc
		case p.accept(token.DESC):
			t.Order = ast.SortDesc
		}
		if p.at(token.NULLS) {
			p.advance()
			switch {
			case p.accept(token.FIRST):
				t.Nulls = ast.NullsFirst
			case p.accept(token.LAST):
				t.Nulls = ast.NullsLast
			default:
				p.syntaxError()
			}
		}
		t.Span = p.span(start)
		out = append(out, t)
		if !p.accept(token.COMMA) {
			return out
		}
	}
}

// parseLimit implements "limit_opt". The comma spelling swaps the operands:
// "LIMIT x, y" means "LIMIT y OFFSET x".
//
//	limit_opt ::= . / LIMIT expr. / LIMIT expr OFFSET expr. / LIMIT expr COMMA expr.
func (p *parser) parseLimit() *ast.Limit {
	if !p.at(token.LIMIT) {
		return nil
	}
	start := p.advance().Pos
	n := &ast.Limit{Count: p.parseExpr(precLowest)}
	switch {
	case p.accept(token.OFFSET):
		n.Offset = p.parseExpr(precLowest)
	case p.accept(token.COMMA):
		n.Offset, n.Count = n.Count, p.parseExpr(precLowest)
		n.Comma = true
	}
	n.Span = p.span(start)
	return n
}

// parseWith implements the WITH clause.
//
//	with ::= WITH wqlist. / with ::= WITH RECURSIVE wqlist.
//	wqlist ::= wqitem. / wqlist ::= wqlist COMMA wqitem.
func (p *parser) parseWith() *ast.With {
	start := p.expect(token.WITH).Pos
	n := &ast.With{}
	if p.accept(token.RECURSIVE) {
		n.Recursive = true
	}
	for {
		n.CTEs = append(n.CTEs, p.parseCTE())
		if !p.accept(token.COMMA) {
			break
		}
	}
	n.Span = p.span(start)
	return n
}

// parseCTE implements one common table expression.
//
//	wqitem ::= withnm eidlist_opt wqas LP select RP.
//	wqas ::= AS. / AS MATERIALIZED. / AS NOT MATERIALIZED.
func (p *parser) parseCTE() *ast.CTE {
	start := p.cur().Pos
	n := &ast.CTE{Name: p.expectName()}
	if p.at(token.LP) {
		p.advance()
		for _, c := range p.parseIndexedColumnList() {
			n.Columns = append(n.Columns, c.Name)
		}
		p.expect(token.RP)
	}
	p.expect(token.AS)
	switch {
	case p.accept(token.MATERIALIZED):
		n.Materialized = ast.MaterializedYes
	case p.at(token.NOT) && p.peek(1).Kind == token.MATERIALIZED:
		p.advance()
		p.advance()
		n.Materialized = ast.MaterializedNo
	}
	p.expect(token.LP)
	n.Select = p.parseSelect()
	p.expect(token.RP)
	n.Span = p.span(start)
	return n
}

// parseIndexedColumnList implements "eidlist". The COLLATE and ASC/DESC
// decorations are a compatibility wart: parserAddExprIdListTerm accepts them
// and then reports "syntax error after column name", which is a grammar
// action rather than a parse error, so they must be parsed here.
//
//	eidlist ::= eidlist COMMA nm collate sortorder. / nm collate sortorder.
//	collate ::= . / collate ::= COLLATE ids.
func (p *parser) parseIndexedColumnList() []*ast.IndexedColumn {
	var out []*ast.IndexedColumn
	for {
		start := p.cur().Pos
		c := &ast.IndexedColumn{Name: p.expectName()}
		if p.accept(token.COLLATE) {
			c.Collation = p.expectIDS()
		}
		switch {
		case p.accept(token.ASC):
			c.Order = ast.SortAsc
		case p.accept(token.DESC):
			c.Order = ast.SortDesc
		}
		c.Span = p.span(start)
		out = append(out, c)
		if !p.accept(token.COMMA) {
			return out
		}
	}
}

// parseWindowDefnList implements "windowdefn_list".
//
//	windowdefn ::= nm AS LP window RP.
func (p *parser) parseWindowDefnList() []*ast.WindowDef {
	var out []*ast.WindowDef
	for {
		start := p.cur().Pos
		name := p.expectName()
		p.expect(token.AS)
		p.expect(token.LP)
		w := p.parseWindow()
		p.expect(token.RP)
		w.Name = name
		w.Span = p.span(start)
		out = append(out, w)
		if !p.accept(token.COMMA) {
			return out
		}
	}
}

// parseWindow implements the "window" nonterminal, the body of an OVER(...)
// or of a WINDOW clause entry.
//
//	window ::= [nm] [PARTITION BY nexprlist] [ORDER BY sortlist] frame_opt.
//
// PARTITION, ORDER, RANGE, ROWS and GROUPS all have shift actions at the
// start of a window, so despite being in the %fallback set they are never
// read as the inherited window name.
func (p *parser) parseWindow() *ast.WindowDef {
	start := p.cur().Pos
	n := &ast.WindowDef{}
	switch p.cur().Kind {
	case token.PARTITION, token.ORDER, token.RANGE, token.ROWS, token.GROUPS, token.RP:
	default:
		n.Base = p.expectName()
	}
	if p.at(token.PARTITION) {
		p.advance()
		p.expect(token.BY)
		n.Partition = p.parseExprList()
	}
	if p.at(token.ORDER) {
		p.advance()
		p.expect(token.BY)
		n.OrderBy = p.parseSortList()
	}
	p.parseFrame(n)
	n.Span = p.span(start)
	return n
}

// parseFrame implements "frame_opt" and its bounds.
//
//	frame_opt ::= . / range_or_rows frame_bound_s frame_exclude_opt.
//	frame_opt ::= range_or_rows BETWEEN frame_bound_s AND frame_bound_e
//	              frame_exclude_opt.
//	range_or_rows ::= RANGE|ROWS|GROUPS.
func (p *parser) parseFrame(n *ast.WindowDef) {
	switch p.cur().Kind {
	case token.RANGE:
		n.Frame = ast.FrameRange
	case token.ROWS:
		n.Frame = ast.FrameRows
	case token.GROUPS:
		n.Frame = ast.FrameGroups
	default:
		return
	}
	p.advance()
	if p.accept(token.BETWEEN) {
		n.StartBound = p.parseFrameBound(false)
		p.expect(token.AND)
		n.EndBound = p.parseFrameBound(true)
	} else {
		n.StartBound = p.parseFrameBound(false)
	}
	n.Exclude = p.parseFrameExclude()
}

// parseFrameBound implements "frame_bound_s" and "frame_bound_e". UNBOUNDED
// and CURRENT have shift actions here, so they are keywords rather than
// fallback identifiers.
//
//	frame_bound_s ::= frame_bound. / UNBOUNDED PRECEDING.
//	frame_bound_e ::= frame_bound. / UNBOUNDED FOLLOWING.
//	frame_bound ::= expr PRECEDING|FOLLOWING. / CURRENT ROW.
func (p *parser) parseFrameBound(isEnd bool) *ast.FrameBound {
	start := p.cur().Pos
	switch p.cur().Kind {
	case token.UNBOUNDED:
		p.advance()
		if isEnd {
			p.expect(token.FOLLOWING)
			return &ast.FrameBound{Span: p.span(start), Type: ast.BoundUnboundedFollowing}
		}
		p.expect(token.PRECEDING)
		return &ast.FrameBound{Span: p.span(start), Type: ast.BoundUnboundedPreceding}
	case token.CURRENT:
		p.advance()
		p.expect(token.ROW)
		return &ast.FrameBound{Span: p.span(start), Type: ast.BoundCurrentRow}
	}
	x := p.parseExpr(precLowest)
	b := &ast.FrameBound{Expr: x}
	switch {
	case p.accept(token.PRECEDING):
		b.Type = ast.BoundPreceding
	case p.accept(token.FOLLOWING):
		b.Type = ast.BoundFollowing
	default:
		p.syntaxError()
	}
	b.Span = p.span(start)
	return b
}

// parseFrameExclude implements "frame_exclude_opt".
//
//	frame_exclude ::= NO OTHERS. / CURRENT ROW. / GROUP|TIES.
func (p *parser) parseFrameExclude() ast.FrameExclude {
	if !p.at(token.EXCLUDE) {
		return ast.ExcludeNone
	}
	p.advance()
	switch p.cur().Kind {
	case token.NO:
		p.advance()
		p.expect(token.OTHERS)
		return ast.ExcludeNoOthers
	case token.CURRENT:
		p.advance()
		p.expect(token.ROW)
		return ast.ExcludeCurrentRow
	case token.GROUP:
		p.advance()
		return ast.ExcludeGroup
	case token.TIES:
		p.advance()
		return ast.ExcludeTies
	}
	p.syntaxError()
	return ast.ExcludeNone
}
