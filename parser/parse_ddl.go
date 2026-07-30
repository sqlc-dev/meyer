package parser

import (
	"github.com/sqlc-dev/meyer/ast"
	"github.com/sqlc-dev/meyer/token"
)

// parseCreate dispatches the CREATE commands. The optional TEMP, UNIQUE and
// VIRTUAL qualifiers each belong to a different object kind, so one or two
// tokens after CREATE decide which rule applies.
//
//	createkw ::= CREATE.
func (p *parser) parseCreate() ast.Stmt {
	start := p.expect(token.CREATE).Pos
	temp := p.accept(token.TEMP) // temp ::= TEMP. / temp ::= .
	switch p.cur().Kind {
	case token.TABLE:
		return p.parseCreateTable(start, temp)
	case token.VIEW:
		return p.parseCreateView(start, temp)
	case token.TRIGGER:
		return p.parseCreateTrigger(start, temp)
	}
	if temp {
		p.syntaxError()
	}
	switch p.cur().Kind {
	case token.UNIQUE:
		p.advance()
		return p.parseCreateIndex(start, true)
	case token.INDEX:
		return p.parseCreateIndex(start, false)
	case token.VIRTUAL:
		return p.parseCreateVirtualTable(start)
	}
	p.syntaxError()
	return nil
}

// parseIfNotExists implements "ifnotexists".
//
//	ifnotexists ::= . / ifnotexists ::= IF NOT EXISTS.
func (p *parser) parseIfNotExists() bool {
	if !p.at(token.IF) {
		return false
	}
	p.advance()
	p.expect(token.NOT)
	p.expect(token.EXISTS)
	return true
}

// parseIfExists implements "ifexists".
//
//	ifexists ::= IF EXISTS. / ifexists ::= .
func (p *parser) parseIfExists() bool {
	if !p.at(token.IF) {
		return false
	}
	p.advance()
	p.expect(token.EXISTS)
	return true
}

// parseCreateTable implements CREATE TABLE.
//
//	create_table ::= createkw temp TABLE ifnotexists nm dbnm.
//	create_table_args ::= LP columnlist conslist_opt RP table_option_set.
//	create_table_args ::= AS select.
func (p *parser) parseCreateTable(start int, temp bool) ast.Stmt {
	p.expect(token.TABLE)
	n := &ast.CreateTableStmt{Temp: temp}
	n.IfNotExists = p.parseIfNotExists()
	n.Name = p.qualifiedName()
	if p.accept(token.AS) {
		n.Select = p.parseSelect()
		n.Span = p.span(start)
		return n
	}
	p.expect(token.LP)
	p.parseColumnAndConstraintList(n)
	p.expect(token.RP)
	p.parseTableOptions(n)
	n.Span = p.span(start)
	return n
}

// parseColumnAndConstraintList implements "columnlist conslist_opt", and
// so also "conslist", "tconscomma" and the column half of "carglist".
//
// The two lists are told apart by their first token: a column definition
// starts with a name, and none of CONSTRAINT, PRIMARY, UNIQUE, CHECK and
// FOREIGN is in the %fallback set, so none of them can be a column name.
// Note that "tconscomma ::= ." makes the comma between table constraints
// optional.
func (p *parser) parseColumnAndConstraintList(n *ast.CreateTableStmt) {
	// "columnlist" is not optional: CREATE TABLE t(PRIMARY KEY(a)) is a
	// syntax error at PRIMARY, so the first item is always a column.
	for {
		n.Columns = append(n.Columns, p.parseColumnDef())
		if !p.accept(token.COMMA) {
			return
		}
		if p.atTableConstraint() {
			break
		}
	}
	var pending *ast.Ident
	pendingStart := 0
	for {
		start := p.cur().Pos
		if p.at(token.CONSTRAINT) {
			// tcons ::= CONSTRAINT nm names whatever constraint follows,
			// and is itself a complete tcons: a list may end with one.
			pendingStart = p.advance().Pos
			pending = p.expectName()
		} else {
			c := p.parseTableConstraint(start)
			if pending != nil {
				// The name is part of the constraint it introduces, so the
				// constraint's span has to start at its CONSTRAINT keyword.
				c.Name, c.Span.Start = pending, pendingStart
				pending = nil
			}
			n.Constraints = append(n.Constraints, c)
		}
		// tconscomma ::= COMMA. / tconscomma ::= . The comma is optional
		// between constraints, but a comma that is there must be followed
		// by one: "CREATE TABLE t(a, PRIMARY KEY(a), )" is an error.
		if p.accept(token.COMMA) {
			continue
		}
		if !p.atTableConstraint() {
			return
		}
	}
}

func (p *parser) atTableConstraint() bool {
	switch p.cur().Kind {
	case token.CONSTRAINT, token.PRIMARY, token.UNIQUE, token.CHECK, token.FOREIGN:
		return true
	}
	return false
}

// parseColumnDef implements "columnname carglist".
//
//	columnname ::= nm typetoken.
func (p *parser) parseColumnDef() *ast.ColumnDef {
	start := p.cur().Pos
	n := &ast.ColumnDef{Name: p.expectName()}
	n.Type = p.parseTypeToken()
	var pending *ast.Ident
	pendingStart := 0
	for {
		cStart := p.cur().Pos
		if p.at(token.CONSTRAINT) { // ccons ::= CONSTRAINT nm.
			pendingStart = p.advance().Pos
			pending = p.expectName()
			continue
		}
		c := p.parseColumnConstraint(cStart)
		if c == nil {
			break
		}
		if pending != nil {
			// As for table constraints, the CONSTRAINT clause belongs to
			// the constraint that follows it.
			c.Name, c.Span.Start = pending, pendingStart
			pending = nil
		}
		n.Constraints = append(n.Constraints, c)
	}
	n.Span = p.span(start)
	return n
}

// parseColumnConstraint implements one "ccons", returning nil when the
// column's constraint list is over.
//
//	ccons ::= NULL onconf. / NOT NULL onconf.
//	ccons ::= PRIMARY KEY sortorder onconf autoinc. / UNIQUE onconf.
//	ccons ::= CHECK LP expr RP. / COLLATE ids.
//	ccons ::= DEFAULT term. / DEFAULT LP expr RP. / DEFAULT PLUS|MINUS term.
//	ccons ::= DEFAULT id.
//	ccons ::= REFERENCES nm eidlist_opt refargs. / defer_subclause.
//	ccons ::= GENERATED ALWAYS AS generated. / ccons ::= AS generated.
func (p *parser) parseColumnConstraint(start int) *ast.ColumnConstraint {
	c := &ast.ColumnConstraint{}
	switch p.cur().Kind {
	case token.NULL:
		p.advance()
		c.Kind = ast.ColumnNull
		c.OnConflict = p.parseOnConflict()
	case token.NOT:
		// NOT introduces either "NOT NULL" or "NOT DEFERRABLE". Which one
		// is settled after consuming it, as SQLite does: it shifts the NOT
		// and reports whatever cannot follow, so "NOT LEFT" names LEFT.
		notPos := p.advance().Pos
		if p.accept(token.NULL) {
			c.Kind = ast.ColumnNotNull
			c.OnConflict = p.parseOnConflict()
			break
		}
		c.Kind = ast.ColumnDeferrable
		c.Deferrable = p.parseDeferClause(notPos, true)
	case token.DEFERRABLE:
		c.Kind = ast.ColumnDeferrable
		c.Deferrable = p.parseDeferClause(p.cur().Pos, false)
	case token.PRIMARY:
		p.advance()
		p.expect(token.KEY)
		c.Kind = ast.ColumnPrimaryKey
		switch {
		case p.accept(token.ASC):
			c.Order = ast.SortAsc
		case p.accept(token.DESC):
			c.Order = ast.SortDesc
		}
		c.OnConflict = p.parseOnConflict()
		if p.accept(token.AUTOINCR) { // autoinc ::= AUTOINCR. / autoinc ::= .
			c.AutoIncrement = true
		}
	case token.UNIQUE:
		p.advance()
		c.Kind = ast.ColumnUnique
		c.OnConflict = p.parseOnConflict()
	case token.CHECK:
		p.advance()
		p.expect(token.LP)
		c.Kind = ast.ColumnCheck
		c.Expr = p.parseExpr(precLowest)
		p.expect(token.RP)
	case token.COLLATE:
		p.advance()
		c.Kind = ast.ColumnCollate
		c.Collation = p.expectIDS()
	case token.DEFAULT:
		p.advance()
		c.Kind = ast.ColumnDefault
		c.Expr = p.parseDefaultValue()
	case token.REFERENCES:
		c.Kind = ast.ColumnReferences
		c.References = p.parseReferences()
	case token.GENERATED:
		p.advance()
		p.expect(token.ALWAYS)
		p.expect(token.AS)
		c.Kind = ast.ColumnGenerated
		c.AlwaysWord = true
		c.Expr, c.GeneratedKind = p.parseGenerated()
	case token.AS:
		p.advance()
		c.Kind = ast.ColumnGenerated
		c.Expr, c.GeneratedKind = p.parseGenerated()
	default:
		return nil
	}
	c.Span = p.span(start)
	return c
}

// parseDefaultValue implements the DEFAULT alternatives, which accept a
// literal, a signed literal, a parenthesised expression or a bare
// identifier, but not a general expression.
func (p *parser) parseDefaultValue() ast.Expr {
	start := p.cur().Pos
	switch p.cur().Kind {
	case token.LP:
		p.advance()
		x := p.parseExpr(precLowest)
		p.expect(token.RP)
		return &ast.ParenExpr{Span: p.span(start), X: x}
	case token.PLUS, token.MINUS:
		t := p.advance()
		op := ast.OpPlus
		if t.Kind == token.MINUS {
			op = ast.OpMinus
		}
		x := p.parseTerm()
		return &ast.UnaryExpr{Span: p.span(start), Op: op, X: x}
	}
	if p.atTerm() {
		return p.parseTerm()
	}
	// ccons ::= DEFAULT scantok id: the id class is ID|INDEXED.
	if !token.IsID(p.cur().Kind) {
		p.syntaxError()
	}
	return p.identOf(p.advance())
}

// atTerm reports whether the lookahead begins a "term": a literal constant.
func (p *parser) atTerm() bool {
	switch p.cur().Kind {
	case token.NULL, token.FLOAT, token.BLOB, token.STRING, token.INTEGER,
		token.QNUMBER, token.CTIME_KW:
		return true
	}
	return false
}

// parseGenerated implements the "generated" nonterminal.
//
//	generated ::= LP expr RP. / generated ::= LP expr RP ID.
//
// The trailing token is the storage class (STORED or VIRTUAL) and is a plain
// TK_ID, so keywords reach it only through %fallback.
func (p *parser) parseGenerated() (ast.Expr, *ast.Ident) {
	p.expect(token.LP)
	x := p.parseExpr(precLowest)
	p.expect(token.RP)
	if token.IsPlainID(p.cur().Kind) {
		return x, p.identOf(p.advance())
	}
	return x, nil
}

// parseOnConflict implements "onconf".
//
//	onconf ::= . / onconf ::= ON CONFLICT resolvetype.
func (p *parser) parseOnConflict() ast.ConflictAction {
	if !p.at(token.ON) {
		return ast.ConflictDefault
	}
	p.advance()
	p.expect(token.CONFLICT)
	return p.parseResolveType()
}

// parseDeferClause implements "defer_subclause".
//
// The caller has already consumed any leading NOT, because that decision
// belongs to it: NOT also begins "NOT NULL".
//
//	defer_subclause ::= NOT DEFERRABLE init_deferred_pred_opt.
//	defer_subclause ::= DEFERRABLE init_deferred_pred_opt.
//	init_deferred_pred_opt ::= . / INITIALLY DEFERRED. / INITIALLY IMMEDIATE.
func (p *parser) parseDeferClause(start int, not bool) *ast.DeferClause {
	d := &ast.DeferClause{Not: not}
	p.expect(token.DEFERRABLE)
	if p.accept(token.INITIALLY) {
		d.HasInitially = true
		switch {
		case p.accept(token.DEFERRED):
			d.InitiallyDeferred = true
		case p.accept(token.IMMEDIATE):
		default:
			p.syntaxError()
		}
	}
	d.Span = p.span(start)
	return d
}

// parseReferences implements a REFERENCES clause and its arguments.
//
//	ccons ::= REFERENCES nm eidlist_opt refargs.
//	refargs ::= . / refargs ::= refargs refarg.
//	refarg ::= MATCH nm. / ON INSERT refact. / ON DELETE refact. / ON UPDATE refact.
//	refact ::= SET NULL. / SET DEFAULT. / CASCADE. / RESTRICT. / NO ACTION.
func (p *parser) parseReferences() *ast.ForeignKeyClause {
	start := p.expect(token.REFERENCES).Pos
	fk := &ast.ForeignKeyClause{Table: p.expectName()}
	if p.at(token.LP) {
		p.advance()
		fk.Columns = p.parseIndexedColumnList()
		p.expect(token.RP)
	}
	for {
		aStart := p.cur().Pos
		switch p.cur().Kind {
		case token.MATCH:
			p.advance()
			match := p.expectName()
			fk.Args = append(fk.Args, &ast.ForeignKeyArg{Span: p.span(aStart), Match: match})
			continue
		case token.ON:
			p.advance()
			arg := &ast.ForeignKeyArg{}
			switch p.cur().Kind {
			case token.INSERT, token.DELETE, token.UPDATE:
				arg.Event = p.text(p.advance())
			default:
				p.syntaxError()
			}
			arg.Action = p.parseRefAct()
			arg.Span = p.span(aStart)
			fk.Args = append(fk.Args, arg)
			continue
		}
		break
	}
	fk.Span = p.span(start)
	return fk
}

func (p *parser) parseRefAct() ast.ForeignKeyAction {
	switch p.cur().Kind {
	case token.SET:
		p.advance()
		switch {
		case p.accept(token.NULL):
			return ast.FKSetNull
		case p.accept(token.DEFAULT):
			return ast.FKSetDefault
		}
		p.syntaxError()
	case token.CASCADE:
		p.advance()
		return ast.FKCascade
	case token.RESTRICT:
		p.advance()
		return ast.FKRestrict
	case token.NO:
		p.advance()
		p.expect(token.ACTION)
		return ast.FKNoAction
	}
	p.syntaxError()
	return ast.FKNoAction
}

// parseTableConstraint implements one "tcons".
//
//	tcons ::= PRIMARY KEY LP sortlist autoinc RP onconf.
//	tcons ::= UNIQUE LP sortlist RP onconf. / CHECK LP expr RP onconf.
//	tcons ::= FOREIGN KEY LP eidlist RP REFERENCES nm eidlist_opt refargs
//	          defer_subclause_opt.
func (p *parser) parseTableConstraint(start int) *ast.TableConstraint {
	c := &ast.TableConstraint{}
	switch p.cur().Kind {
	case token.PRIMARY:
		p.advance()
		p.expect(token.KEY)
		p.expect(token.LP)
		c.Kind = ast.TablePrimaryKey
		c.Columns = p.parseSortList()
		if p.accept(token.AUTOINCR) {
			c.AutoIncrement = true
		}
		p.expect(token.RP)
		c.OnConflict = p.parseOnConflict()
	case token.UNIQUE:
		p.advance()
		p.expect(token.LP)
		c.Kind = ast.TableUnique
		c.Columns = p.parseSortList()
		p.expect(token.RP)
		c.OnConflict = p.parseOnConflict()
	case token.CHECK:
		p.advance()
		p.expect(token.LP)
		c.Kind = ast.TableCheck
		c.Expr = p.parseExpr(precLowest)
		p.expect(token.RP)
		c.OnConflict = p.parseOnConflict()
	case token.FOREIGN:
		p.advance()
		p.expect(token.KEY)
		p.expect(token.LP)
		c.Kind = ast.TableForeignKey
		c.FKColumns = p.parseIndexedColumnList()
		p.expect(token.RP)
		c.References = p.parseReferences()
		// Nothing else that may follow a REFERENCES clause here starts with
		// NOT, so it always begins a defer_subclause.
		if start := p.cur().Pos; p.at(token.DEFERRABLE) {
			c.Deferrable = p.parseDeferClause(start, false)
		} else if p.accept(token.NOT) {
			c.Deferrable = p.parseDeferClause(start, true)
		}
	default:
		p.syntaxError()
	}
	c.Span = p.span(start)
	return c
}

// parseTableOptions implements "table_option_set". Unknown options are a
// grammar-action error ("unknown table option: %s"), not a parse error, so
// any name is accepted here.
//
// The first option may be missing while later ones are present, because
// table_option_set recurses on a possibly empty prefix: "CREATE TABLE t(a),
// STRICT" parses. A comma must be followed by an option, though.
//
//	table_option_set ::= . / table_option. / table_option_set COMMA table_option.
//	table_option ::= WITHOUT nm. / table_option ::= nm.
func (p *parser) parseTableOptions(n *ast.CreateTableStmt) {
	option := func() {
		start := p.cur().Pos
		o := &ast.TableOption{Without: p.accept(token.WITHOUT)}
		o.Name = p.expectName()
		o.Span = p.span(start)
		n.Options = append(n.Options, o)
	}
	switch {
	case p.at(token.WITHOUT) || token.IsName(p.cur().Kind):
		option()
	case !p.at(token.COMMA):
		return
	}
	for p.accept(token.COMMA) {
		option()
	}
}

// parseCreateIndex implements CREATE INDEX.
//
//	cmd ::= createkw uniqueflag INDEX ifnotexists nm dbnm ON nm LP sortlist RP
//	        where_opt.
func (p *parser) parseCreateIndex(start int, unique bool) ast.Stmt {
	p.expect(token.INDEX)
	n := &ast.CreateIndexStmt{Unique: unique}
	n.IfNotExists = p.parseIfNotExists()
	n.Name = p.qualifiedName()
	p.expect(token.ON)
	n.Table = p.expectName()
	p.expect(token.LP)
	n.Columns = p.parseSortList()
	p.expect(token.RP)
	if p.accept(token.WHERE) {
		n.Where = p.parseExpr(precLowest)
	}
	n.Span = p.span(start)
	return n
}

// parseCreateView implements CREATE VIEW.
//
//	cmd ::= createkw temp VIEW ifnotexists nm dbnm eidlist_opt AS select.
func (p *parser) parseCreateView(start int, temp bool) ast.Stmt {
	p.expect(token.VIEW)
	n := &ast.CreateViewStmt{Temp: temp}
	n.IfNotExists = p.parseIfNotExists()
	n.Name = p.qualifiedName()
	if p.at(token.LP) {
		p.advance()
		n.Columns = p.parseIndexedColumnList()
		p.expect(token.RP)
	}
	p.expect(token.AS)
	n.Select = p.parseSelect()
	n.Span = p.span(start)
	return n
}

// parseCreateVirtualTable implements CREATE VIRTUAL TABLE. Module arguments
// are an arbitrary balanced token sequence: the grammar matches them with
// %wildcard ANY and keeps only their source text.
//
//	create_vtab ::= createkw VIRTUAL TABLE ifnotexists nm dbnm USING nm.
//	cmd ::= create_vtab. / cmd ::= create_vtab LP vtabarglist RP.
//	vtabarglist ::= vtabarg. / vtabarglist COMMA vtabarg.
//	vtabarg ::= . / vtabarg vtabargtoken.
//	vtabargtoken ::= ANY. / lp anylist RP.
//	anylist ::= . / anylist LP anylist RP. / anylist ANY.
func (p *parser) parseCreateVirtualTable(start int) ast.Stmt {
	p.expect(token.VIRTUAL)
	p.expect(token.TABLE)
	n := &ast.CreateVirtualTableStmt{}
	n.IfNotExists = p.parseIfNotExists()
	n.Name = p.qualifiedName()
	p.expect(token.USING)
	n.Module = p.expectName()
	if p.at(token.LP) {
		p.advance()
		n.HasArgs = true
		n.Args = p.parseVTabArgs()
		p.expect(token.RP)
	}
	n.Span = p.span(start)
	return n
}

func (p *parser) parseVTabArgs() []string {
	var args []string
	depth := 0
	argStart := p.cur().Pos
	argEnd := argStart
	for {
		t := p.cur()
		switch {
		case t.Kind == token.EOF:
			p.syntaxError()
		case t.Kind == token.LP:
			depth++
		case t.Kind == token.RP:
			if depth == 0 {
				return append(args, p.src[argStart:argEnd])
			}
			depth--
		case t.Kind == token.COMMA && depth == 0:
			args = append(args, p.src[argStart:argEnd])
			p.advance()
			argStart, argEnd = p.cur().Pos, p.cur().Pos
			continue
		}
		p.advance()
		argEnd = t.End
	}
}

// parseDrop implements the four DROP commands.
//
//	cmd ::= DROP TABLE ifexists fullname. / DROP VIEW ifexists fullname.
//	cmd ::= DROP INDEX ifexists fullname. / DROP TRIGGER ifexists fullname.
func (p *parser) parseDrop() ast.Stmt {
	start := p.expect(token.DROP).Pos
	kind := p.cur().Kind
	switch kind {
	case token.TABLE, token.VIEW, token.INDEX, token.TRIGGER:
		p.advance()
	default:
		p.syntaxError()
	}
	ifExists := p.parseIfExists()
	name := p.qualifiedName()
	span := p.span(start)
	switch kind {
	case token.TABLE:
		return &ast.DropTableStmt{Span: span, IfExists: ifExists, Name: name}
	case token.VIEW:
		return &ast.DropViewStmt{Span: span, IfExists: ifExists, Name: name}
	case token.INDEX:
		return &ast.DropIndexStmt{Span: span, IfExists: ifExists, Name: name}
	default:
		return &ast.DropTriggerStmt{Span: span, IfExists: ifExists, Name: name}
	}
}

// parseAlterTable implements every ALTER TABLE form of the pinned build.
//
//	cmd ::= ALTER TABLE fullname RENAME TO nm.
//	cmd ::= ALTER TABLE fullname RENAME kwcolumn_opt nm TO nm.
//	cmd ::= alter_add carglist.
//	alter_add ::= ALTER TABLE fullname ADD kwcolumn_opt nm typetoken.
//	cmd ::= ALTER TABLE fullname ADD CONSTRAINT nm CHECK LP expr RP onconf.
//	cmd ::= ALTER TABLE fullname ADD CHECK LP expr RP onconf.
//	cmd ::= ALTER TABLE fullname DROP kwcolumn_opt nm.
//	cmd ::= ALTER TABLE fullname DROP CONSTRAINT nm.
//	cmd ::= ALTER TABLE fullname ALTER kwcolumn_opt nm DROP NOT NULL.
//	cmd ::= ALTER TABLE fullname ALTER kwcolumn_opt nm SET NOT NULL onconf.
//	kwcolumn_opt ::= . / kwcolumn_opt ::= COLUMNKW.
func (p *parser) parseAlterTable() ast.Stmt {
	start := p.expect(token.ALTER).Pos
	p.expect(token.TABLE)
	n := &ast.AlterTableStmt{Table: p.qualifiedName()}
	switch p.cur().Kind {
	case token.RENAME:
		p.advance()
		if p.accept(token.TO) {
			n.Action = ast.AlterRenameTable
			n.NewName = p.expectName()
			break
		}
		n.Action = ast.AlterRenameColumn
		n.ColumnKeyword = p.accept(token.COLUMNKW)
		n.Column = p.expectName()
		p.expect(token.TO)
		n.NewName = p.expectName()
	case token.ADD:
		p.advance()
		switch p.cur().Kind {
		case token.CONSTRAINT:
			p.advance()
			n.Action = ast.AlterAddConstraintCheck
			n.ConstraintName = p.expectName()
			p.expect(token.CHECK)
			p.expect(token.LP)
			n.Expr = p.parseExpr(precLowest)
			p.expect(token.RP)
			n.OnConflict = p.parseOnConflict()
		case token.CHECK:
			p.advance()
			n.Action = ast.AlterAddConstraintCheck
			p.expect(token.LP)
			n.Expr = p.parseExpr(precLowest)
			p.expect(token.RP)
			n.OnConflict = p.parseOnConflict()
		default:
			n.Action = ast.AlterAddColumn
			n.ColumnKeyword = p.accept(token.COLUMNKW)
			n.ColumnDef = p.parseColumnDef()
			n.Column = n.ColumnDef.Name
		}
	case token.DROP:
		p.advance()
		if p.at(token.CONSTRAINT) {
			p.advance()
			n.Action = ast.AlterDropConstraint
			n.ConstraintName = p.expectName()
			break
		}
		n.Action = ast.AlterDropColumn
		n.ColumnKeyword = p.accept(token.COLUMNKW)
		n.Column = p.expectName()
	case token.ALTER:
		p.advance()
		n.ColumnKeyword = p.accept(token.COLUMNKW)
		n.Column = p.expectName()
		switch {
		case p.accept(token.DROP):
			n.Action = ast.AlterDropNotNull
			p.expect(token.NOT)
			p.expect(token.NULL)
		case p.accept(token.SET):
			n.Action = ast.AlterSetNotNull
			p.expect(token.NOT)
			p.expect(token.NULL)
			n.OnConflict = p.parseOnConflict()
		default:
			p.syntaxError()
		}
	default:
		p.syntaxError()
	}
	n.Span = p.span(start)
	return n
}
