package parser

import (
	"strconv"
	"strings"

	"github.com/sqlc-dev/meyer/ast"
	"github.com/sqlc-dev/meyer/token"
)

// Operator precedence, transcribed from the %left/%right/%nonassoc block of
// parse.y in the order it declares them (lowest binding first):
//
//	%left OR. %left AND. %right NOT.
//	%left IS MATCH LIKE_KW BETWEEN IN ISNULL NOTNULL NE EQ.
//	%left GT LE LT GE. %right ESCAPE.
//	%left BITAND BITOR LSHIFT RSHIFT. %left PLUS MINUS.
//	%left STAR SLASH REM. %left CONCAT PTR. %left COLLATE. %right BITNOT.
//
// Rather than eleven mutually recursive functions, parseExpr climbs the
// ladder: parseExpr(min) consumes every operator whose precedence is at
// least min. A left-associative operator of precedence n parses its right
// operand at min n+1, which is what "reduce on equal precedence" means.
const (
	precLowest     = 0
	precOr         = 1
	precAnd        = 2
	precNot        = 3
	precCompare    = 4 // IS MATCH LIKE_KW BETWEEN IN ISNULL NOTNULL NE EQ
	precRelational = 5 // GT LE LT GE
	precEscape     = 6
	precBitwise    = 7
	precAdd        = 8
	precMul        = 9
	precConcat     = 10
	precCollate    = 11
	precUnary      = 12 // BITNOT, and the [BITNOT] mark on unary PLUS/MINUS
)

// parseExpr parses an expression, consuming operators of precedence min and
// above.
func (p *parser) parseExpr(min int) ast.Expr {
	p.enter()
	defer p.leave()
	return p.parseOperators(p.parsePrefix(), min, false)
}

// parseBetweenBound parses the lower bound of a BETWEEN, which ends at the
// AND that belongs to the BETWEEN rule itself. That AND is only the one at
// the top level: SQLite shifts it because the rule's own precedence beats
// AND's, but inside the right operand of a weaker operator the ordinary
// precedence applies, so "x BETWEEN a OR b AND c" reads the whole of
// "a OR (b AND c)" as the lower bound and then still wants an AND.
func (p *parser) parseBetweenBound() ast.Expr {
	p.enter()
	defer p.leave()
	return p.parseOperators(p.parsePrefix(), precOr, true)
}

// parsePrefix handles the prefix operators and then a primary expression.
//
//	expr ::= NOT expr. / expr ::= BITNOT expr. / expr ::= PLUS|MINUS expr. [BITNOT]
func (p *parser) parsePrefix() ast.Expr {
	t := p.cur()
	switch t.Kind {
	case token.NOT:
		p.advance()
		x := p.parseExpr(precNot + 1)
		return &ast.UnaryExpr{Span: ast.Span{Start: t.Pos, Stop: x.End()}, Op: ast.OpNot, X: x}
	case token.BITNOT:
		p.advance()
		x := p.parseExpr(precUnary + 1)
		return &ast.UnaryExpr{Span: ast.Span{Start: t.Pos, Stop: x.End()}, Op: ast.OpBitNot, X: x}
	case token.PLUS, token.MINUS:
		p.advance()
		op := ast.OpPlus
		if t.Kind == token.MINUS {
			op = ast.OpMinus
		}
		x := p.parseExpr(precUnary + 1)
		return &ast.UnaryExpr{Span: ast.Span{Start: t.Pos, Stop: x.End()}, Op: op, X: x}
	}
	return p.parsePrimary()
}

// parseOperators is the climbing loop: it extends x with every infix or
// postfix operator whose precedence is at least min.
func (p *parser) parseOperators(x ast.Expr, min int, stopAtAnd bool) ast.Expr {
	for {
		t := p.cur()
		switch t.Kind {
		case token.OR:
			if precOr < min {
				return x
			}
			x = p.binary(x, ast.OpOr, precOr)
		case token.AND:
			if stopAtAnd || precAnd < min {
				return x
			}
			x = p.binary(x, ast.OpAnd, precAnd)
		case token.EQ:
			if precCompare < min {
				return x
			}
			x = p.binary(x, ast.OpEq, precCompare)
		case token.NE:
			if precCompare < min {
				return x
			}
			x = p.binary(x, ast.OpNe, precCompare)
		case token.LT, token.LE, token.GT, token.GE:
			if precRelational < min {
				return x
			}
			op := map[token.Kind]ast.Operator{
				token.LT: ast.OpLt, token.LE: ast.OpLe,
				token.GT: ast.OpGt, token.GE: ast.OpGe,
			}[t.Kind]
			x = p.binary(x, op, precRelational)
		case token.BITAND, token.BITOR, token.LSHIFT, token.RSHIFT:
			if precBitwise < min {
				return x
			}
			op := map[token.Kind]ast.Operator{
				token.BITAND: ast.OpBitAnd, token.BITOR: ast.OpBitOr,
				token.LSHIFT: ast.OpLShift, token.RSHIFT: ast.OpRShift,
			}[t.Kind]
			x = p.binary(x, op, precBitwise)
		case token.PLUS, token.MINUS:
			if precAdd < min {
				return x
			}
			op := ast.OpAdd
			if t.Kind == token.MINUS {
				op = ast.OpSub
			}
			x = p.binary(x, op, precAdd)
		case token.STAR, token.SLASH, token.REM:
			if precMul < min {
				return x
			}
			op := map[token.Kind]ast.Operator{
				token.STAR: ast.OpMul, token.SLASH: ast.OpDiv, token.REM: ast.OpMod,
			}[t.Kind]
			x = p.binary(x, op, precMul)
		case token.CONCAT:
			if precConcat < min {
				return x
			}
			x = p.binary(x, ast.OpConcat, precConcat)
		case token.PTR:
			// expr ::= expr PTR expr. The "->" and "->>" spellings share a
			// token; the text distinguishes them.
			if precConcat < min {
				return x
			}
			op := ast.OpPtr
			if t.Text == "->>" {
				op = ast.OpPtr2
			}
			x = p.binary(x, op, precConcat)
		case token.COLLATE:
			// expr ::= expr COLLATE ids.
			if precCollate < min {
				return x
			}
			p.advance()
			name := p.expectIDS()
			x = &ast.CollateExpr{Span: ast.Span{Start: x.Pos(), Stop: name.End()}, X: x, Name: name}
		case token.ISNULL, token.NOTNULL:
			// expr ::= expr ISNULL|NOTNULL.
			if precCompare < min {
				return x
			}
			p.advance()
			x = &ast.NullCheckExpr{
				Span: ast.Span{Start: x.Pos(), Stop: p.prevEnd()},
				Not:  t.Kind == token.NOTNULL,
				X:    x,
			}
		case token.IS:
			if precCompare < min {
				return x
			}
			x = p.parseIs(x)
		case token.LIKE_KW, token.MATCH:
			if precCompare < min {
				return x
			}
			x = p.parseLike(x, false)
		case token.BETWEEN:
			if precCompare < min {
				return x
			}
			x = p.parseBetween(x, false)
		case token.IN:
			if precCompare < min {
				return x
			}
			x = p.parseIn(x, false)
		case token.NOT:
			// The negated postfix forms all begin with NOT:
			//
			//	expr ::= expr NOT NULL.                      [NOT]
			//	likeop ::= NOT LIKE_KW|MATCH.                [LIKE_KW]
			//	between_op ::= NOT BETWEEN. / in_op ::= NOT IN.
			//
			// "expr NOT NULL" takes its precedence from its leftmost
			// terminal, NOT; the others are marked with the operator they
			// negate. Deciding needs one more token of lookahead.
			next := p.peek(1).Kind
			prec := precCompare
			if next == token.NULL {
				prec = precNot
			}
			if prec < min {
				return x
			}
			p.advance() // NOT
			switch next {
			case token.NULL:
				p.advance()
				x = &ast.NullCheckExpr{Span: ast.Span{Start: x.Pos(), Stop: p.prevEnd()}, Not: true, X: x}
			case token.LIKE_KW, token.MATCH:
				x = p.parseLike(x, true)
			case token.BETWEEN:
				x = p.parseBetween(x, true)
			case token.IN:
				x = p.parseIn(x, true)
			default:
				// SQLite shifts the NOT and only then finds it has no
				// continuation, so the error names the following token.
				p.syntaxError()
			}
		default:
			return x
		}
	}
}

// binary consumes a left-associative infix operator and its right operand.
func (p *parser) binary(x ast.Expr, op ast.Operator, prec int) ast.Expr {
	p.advance()
	y := p.parseExpr(prec + 1)
	return &ast.BinaryExpr{Span: ast.Span{Start: x.Pos(), Stop: y.End()}, Op: op, X: x, Y: y}
}

// parseIs implements the four IS forms.
//
//	expr ::= expr IS expr. / expr IS NOT expr.
//	expr ::= expr IS NOT DISTINCT FROM expr. / expr IS DISTINCT FROM expr.
func (p *parser) parseIs(x ast.Expr) ast.Expr {
	p.advance() // IS
	n := &ast.IsExpr{X: x}
	if p.accept(token.NOT) {
		n.Not = true
	}
	// DISTINCT is not in the %fallback set, so it can only be the keyword.
	if p.at(token.DISTINCT) {
		p.advance()
		p.expect(token.FROM)
		n.Distinct = true
	}
	n.Y = p.parseExpr(precCompare + 1)
	n.Span = ast.Span{Start: x.Pos(), Stop: n.Y.End()}
	return n
}

// parseLike implements the LIKE/GLOB/REGEXP/MATCH operators.
//
//	likeop ::= LIKE_KW|MATCH. / likeop ::= NOT LIKE_KW|MATCH.
//	expr ::= expr likeop expr. [LIKE_KW]
//	expr ::= expr likeop expr ESCAPE expr. [LIKE_KW]
func (p *parser) parseLike(x ast.Expr, not bool) ast.Expr {
	op := identOf(p.advance())
	n := &ast.LikeExpr{Op: op, Not: not, X: x}
	n.Y = p.parseExpr(precCompare + 1)
	if p.at(token.ESCAPE) {
		// %right ESCAPE binds tighter than the LIKE rule, so the escape
		// operand absorbs everything above LIKE_KW's own precedence.
		p.advance()
		n.Escape = p.parseExpr(precCompare + 1)
	}
	n.Span = ast.Span{Start: x.Pos(), Stop: p.prevEnd()}
	return n
}

// parseBetween implements the BETWEEN operator. The lower bound stops short
// of AND, which belongs to the BETWEEN rule itself.
//
//	expr ::= expr between_op expr AND expr. [BETWEEN]
func (p *parser) parseBetween(x ast.Expr, not bool) ast.Expr {
	p.advance() // BETWEEN
	n := &ast.BetweenExpr{Not: not, X: x}
	n.Lo = p.parseBetweenBound()
	p.expect(token.AND)
	n.Hi = p.parseExpr(precCompare + 1)
	n.Span = ast.Span{Start: x.Pos(), Stop: n.Hi.End()}
	return n
}

// parseIn implements the three IN right-hand sides.
//
//	expr ::= expr in_op LP exprlist RP. [IN]
//	expr ::= expr in_op LP select RP. [IN]
//	expr ::= expr in_op nm dbnm paren_exprlist. [IN]
func (p *parser) parseIn(x ast.Expr, not bool) ast.Expr {
	p.advance() // IN
	n := &ast.InExpr{Not: not, X: x}
	if p.at(token.LP) {
		p.advance()
		n.Parens = true
		if p.atSelect() {
			n.Select = p.parseSelect()
		} else if !p.at(token.RP) {
			n.List = p.parseExprList()
		}
		p.expect(token.RP)
	} else {
		n.Table = p.qualifiedName()
		if p.at(token.LP) { // paren_exprlist
			p.advance()
			n.HasArgs = true
			if !p.at(token.RP) {
				n.Args = p.parseExprList()
			}
			p.expect(token.RP)
		}
	}
	n.Span = ast.Span{Start: x.Pos(), Stop: p.prevEnd()}
	return n
}

// atSelect reports whether the lookahead can begin a "select": the only
// three possibilities are SELECT, VALUES and WITH.
func (p *parser) atSelect() bool {
	switch p.cur().Kind {
	case token.SELECT, token.VALUES, token.WITH:
		return true
	}
	return false
}

// parseExprList implements "nexprlist": one or more comma-separated
// expressions.
func (p *parser) parseExprList() []ast.Expr {
	list := []ast.Expr{p.parseExpr(precLowest)}
	for p.accept(token.COMMA) {
		list = append(list, p.parseExpr(precLowest))
	}
	return list
}

// parsePrimary implements the non-operator "expr" and "term" alternatives.
func (p *parser) parsePrimary() ast.Expr {
	t := p.cur()
	switch t.Kind {
	case token.LP:
		return p.parseParenExpr()

	case token.NULL, token.INTEGER, token.FLOAT, token.BLOB, token.QNUMBER,
		token.CTIME_KW:
		return p.parseTerm()

	case token.STRING:
		// STRING is both a literal and a member of "nm", so "'a'.'b'" is a
		// qualified reference rather than a string.
		if p.peek(1).Kind == token.DOT {
			return p.parseQualifiedRef()
		}
		return p.parseTerm()

	case token.VARIABLE:
		p.advance()
		return p.bindParam(t)

	case token.CAST:
		// expr ::= CAST LP expr AS typetoken RP.
		p.advance()
		p.expect(token.LP)
		x := p.parseExpr(precLowest)
		p.expect(token.AS)
		typ := p.parseTypeToken()
		p.expect(token.RP)
		return &ast.CastExpr{Span: p.span(t.Pos), X: x, Type: typ}

	case token.CASE:
		return p.parseCase()

	case token.EXISTS:
		// expr ::= EXISTS LP select RP.
		p.advance()
		p.expect(token.LP)
		sel := p.parseSelect()
		p.expect(token.RP)
		return &ast.ExistsExpr{Span: p.span(t.Pos), Select: sel}

	case token.RAISE:
		return p.parseRaise()
	}

	if token.IsIDJ(t.Kind) {
		switch p.peek(1).Kind {
		case token.LP:
			return p.parseFuncCall()
		case token.DOT:
			return p.parseQualifiedRef()
		}
		p.advance()
		return identOf(t)
	}
	p.syntaxError()
	return nil
}

func spanOf(t token.Token) ast.Span { return ast.Span{Start: t.Pos, Stop: t.End} }

// parseTerm implements the literal alternatives of "term". It is separate
// from parsePrimary because DEFAULT takes a bare term: in
// "b DEFAULT 'xyzzy'. c" the dot is a syntax error, not the start of a
// qualified reference.
//
//	term ::= NULL|FLOAT|BLOB. / STRING. / INTEGER. / QNUMBER. / CTIME_KW.
func (p *parser) parseTerm() ast.Expr {
	t := p.cur()
	switch t.Kind {
	case token.NULL:
		p.advance()
		return &ast.Literal{Span: spanOf(t), Kind: ast.LitNull, Value: t.Text, Raw: t.Text}
	case token.INTEGER:
		p.advance()
		return &ast.Literal{Span: spanOf(t), Kind: ast.LitInteger, Value: t.Text, Raw: t.Text}
	case token.FLOAT:
		p.advance()
		return &ast.Literal{Span: spanOf(t), Kind: ast.LitFloat, Value: t.Text, Raw: t.Text}
	case token.BLOB:
		p.advance()
		return &ast.Literal{Span: spanOf(t), Kind: ast.LitBlob, Value: t.Text, Raw: t.Text}
	case token.STRING:
		p.advance()
		return &ast.Literal{
			Span: spanOf(t), Kind: ast.LitString,
			Value: dequote(t.Text, '\''), Raw: t.Text,
		}
	case token.QNUMBER:
		// The grammar action dequotes the digit separators and rejects a
		// misplaced one.
		p.advance()
		value := p.dequoteNumber(t)
		kind := ast.LitInteger
		if strings.ContainsAny(value, ".eE") && !strings.HasPrefix(strings.ToLower(value), "0x") {
			kind = ast.LitFloat
		}
		return &ast.Literal{Span: spanOf(t), Kind: kind, Value: value, Raw: t.Text}
	case token.CTIME_KW:
		// CTIME_KW falls back to ID elsewhere, but here it has a shift
		// action, so it is always the keyword.
		p.advance()
		kind := ast.LitCurrentTimestamp
		switch strings.ToUpper(t.Text) {
		case "CURRENT_DATE":
			kind = ast.LitCurrentDate
		case "CURRENT_TIME":
			kind = ast.LitCurrentTime
		}
		return &ast.Literal{Span: spanOf(t), Kind: kind, Value: t.Text, Raw: t.Text}
	}
	p.syntaxError()
	return nil
}

// parseParenExpr distinguishes the three parenthesised forms.
//
//	expr ::= LP expr RP. / expr ::= LP nexprlist COMMA expr RP.
//	expr ::= LP select RP.
func (p *parser) parseParenExpr() ast.Expr {
	start := p.expect(token.LP).Pos
	if p.atSelect() {
		sel := p.parseSelect()
		p.expect(token.RP)
		return &ast.SubqueryExpr{Span: p.span(start), Select: sel}
	}
	first := p.parseExpr(precLowest)
	if !p.at(token.COMMA) {
		p.expect(token.RP)
		return &ast.ParenExpr{Span: p.span(start), X: first}
	}
	list := []ast.Expr{first}
	for p.accept(token.COMMA) {
		list = append(list, p.parseExpr(precLowest))
	}
	p.expect(token.RP)
	return &ast.VectorExpr{Span: p.span(start), List: list}
}

// parseQualifiedRef implements the dotted column references.
//
//	expr ::= nm DOT nm. / expr ::= nm DOT nm DOT nm.
func (p *parser) parseQualifiedRef() ast.Expr {
	parts := []*ast.Ident{p.expectName()}
	p.expect(token.DOT)
	parts = append(parts, p.expectName())
	if p.at(token.DOT) {
		p.advance()
		parts = append(parts, p.expectName())
	}
	return &ast.QualifiedRef{
		Span:  ast.Span{Start: parts[0].Pos(), Stop: parts[len(parts)-1].End()},
		Parts: parts,
	}
}

// parseCase implements CASE.
//
//	expr ::= CASE case_operand case_exprlist case_else END.
func (p *parser) parseCase() ast.Expr {
	start := p.expect(token.CASE).Pos
	n := &ast.CaseExpr{}
	if !p.at(token.WHEN) { // case_operand ::= expr. / case_operand ::= .
		n.Operand = p.parseExpr(precLowest)
	}
	for p.at(token.WHEN) { // case_exprlist is one or more WHEN/THEN pairs
		wStart := p.advance().Pos
		when := p.parseExpr(precLowest)
		p.expect(token.THEN)
		then := p.parseExpr(precLowest)
		n.Whens = append(n.Whens, &ast.CaseWhen{Span: p.span(wStart), When: when, Then: then})
	}
	if len(n.Whens) == 0 {
		p.syntaxError()
	}
	if p.accept(token.ELSE) {
		n.Else = p.parseExpr(precLowest)
	}
	p.expect(token.END)
	n.Span = p.span(start)
	return n
}

// parseRaise implements the trigger-only RAISE expression.
//
//	expr ::= RAISE LP IGNORE RP.
//	expr ::= RAISE LP raisetype COMMA expr RP.
//	raisetype ::= ROLLBACK. / ABORT. / FAIL.
func (p *parser) parseRaise() ast.Expr {
	start := p.expect(token.RAISE).Pos
	p.expect(token.LP)
	n := &ast.RaiseExpr{}
	switch p.cur().Kind {
	case token.IGNORE:
		n.Action = "IGNORE"
		p.advance()
	case token.ROLLBACK, token.ABORT, token.FAIL:
		n.Action = strings.ToUpper(p.advance().Text)
		p.expect(token.COMMA)
		n.Message = p.parseExpr(precLowest)
	default:
		p.syntaxError()
	}
	p.expect(token.RP)
	n.Span = p.span(start)
	return n
}

// parseFuncCall implements the function-call forms and their window
// decorations.
//
//	expr ::= idj LP distinct exprlist RP filter_over.
//	expr ::= idj LP distinct exprlist ORDER BY sortlist RP filter_over.
//	expr ::= idj LP STAR RP filter_over.
func (p *parser) parseFuncCall() ast.Expr {
	name := identOf(p.advance())
	n := &ast.FuncCall{Name: name}
	p.expect(token.LP)
	if p.at(token.STAR) {
		p.advance()
		n.Star = true
	} else {
		switch p.cur().Kind { // distinct ::= DISTINCT. / ALL. / .
		case token.DISTINCT:
			p.advance()
			n.Distinct = true
		case token.ALL:
			p.advance()
			n.All = true
		}
		if !p.at(token.RP) && !p.at(token.ORDER) {
			n.Args = p.parseExprList()
		}
		if p.at(token.ORDER) { // aggregate inner ORDER BY
			p.advance()
			p.expect(token.BY)
			n.OrderBy = p.parseSortList()
		}
	}
	p.expect(token.RP)
	p.parseFilterOver(n)
	n.Span = ast.Span{Start: name.Pos(), Stop: p.prevEnd()}
	return n
}

// parseFilterOver implements the optional FILTER and OVER decorations. The
// tokenizer has already decided whether FILTER and OVER are keywords here
// (see lexer.resolveWindowKeywords), so no lookahead is needed.
//
//	filter_over ::= filter_clause over_clause. / over_clause. / filter_clause.
//	filter_clause ::= FILTER LP WHERE expr RP.
//	over_clause ::= OVER LP window RP. / over_clause ::= OVER nm.
func (p *parser) parseFilterOver(n *ast.FuncCall) {
	if p.at(token.FILTER) {
		p.advance()
		p.expect(token.LP)
		p.expect(token.WHERE)
		n.Filter = p.parseExpr(precLowest)
		p.expect(token.RP)
	}
	if p.at(token.OVER) {
		start := p.advance().Pos
		if p.at(token.LP) {
			p.advance()
			n.Over = p.parseWindow()
			p.expect(token.RP)
			n.Over.Span = p.span(start)
		} else {
			base := p.expectName()
			n.Over = &ast.WindowDef{Span: p.span(start), Base: base, NameOnly: true}
		}
	}
}

// parseTypeToken implements "typetoken", the concatenation of one or more
// identifier tokens with an optional size specification. The whole thing may
// be empty, which is how "x" and "CAST(1 AS )" both work.
//
//	typetoken ::= . / typename. / typename LP signed RP.
//	typetoken ::= typename LP signed COMMA signed RP.
//	typename ::= ids. / typename ::= typename ids.
func (p *parser) parseTypeToken() *ast.TypeName {
	if !token.IsIDS(p.cur().Kind) {
		return nil
	}
	start := p.cur().Pos
	var words []string
	for token.IsIDS(p.cur().Kind) {
		words = append(words, p.advance().Text)
	}
	n := &ast.TypeName{Name: strings.Join(words, " ")}
	if p.at(token.LP) {
		p.advance()
		n.Args = append(n.Args, p.parseSignedNumber())
		if p.accept(token.COMMA) {
			n.Args = append(n.Args, p.parseSignedNumber())
		}
		p.expect(token.RP)
	}
	n.Span = p.span(start)
	n.Raw = p.src[start:n.Stop]
	return n
}

// parseSignedNumber implements "signed": an INTEGER or FLOAT with an
// optional sign.
//
//	signed ::= plus_num. / signed ::= minus_num.
//	plus_num ::= PLUS number. / plus_num ::= number.
//	minus_num ::= MINUS number.
func (p *parser) parseSignedNumber() string {
	var sign string
	if p.at(token.PLUS) || p.at(token.MINUS) {
		sign = p.advance().Text
	}
	if !p.at(token.INTEGER) && !p.at(token.FLOAT) {
		p.syntaxError()
	}
	return sign + p.advance().Text
}

// bindParam builds a BindParam and assigns its number the way
// sqlite3ExprAssignVarNumber does.
//
//	expr ::= VARIABLE.
func (p *parser) bindParam(t token.Token) ast.Expr {
	// "#NNN" is a VDBE register reference, legal only during a nested
	// parse. User-facing SQLite reports a plain syntax error for it -- but
	// from a grammar action, which does not stop the LALR parser: it
	// finishes handling the token it was given, and any syntax error that
	// falls out of that overwrites the message and its offset. So
	// "SELECT #1 #1" is reported at the second reference, not the first.
	// Deferring the error reproduces that, since a real syntax error later
	// in the statement aborts the parse and wins on its own.
	if len(t.Text) >= 2 && t.Text[0] == '#' && t.Text[1] >= '0' && t.Text[1] <= '9' {
		p.defer_(`near "`+t.Text+`": syntax error`, t.Pos)
	}
	n := &ast.BindParam{Span: spanOf(t), Raw: t.Text}
	switch {
	case t.Text == "?":
		n.Kind = ast.ParamAnon
		p.nVar++
		n.Number = p.nVar
	case t.Text[0] == '?':
		n.Kind = ast.ParamNumber
		v, err := strconv.Atoi(t.Text[1:])
		if err == nil {
			n.Number = v
			if v > p.nVar {
				p.nVar = v
			}
		}
	default:
		switch t.Text[0] {
		case ':':
			n.Kind = ast.ParamColon
		case '@':
			n.Kind = ast.ParamAt
		default:
			n.Kind = ast.ParamDollar
		}
		n.Name = t.Text[1:]
		if v, ok := p.varNums[t.Text]; ok {
			n.Number = v
		} else {
			p.nVar++
			n.Number = p.nVar
			p.varNums[t.Text] = p.nVar
		}
	}
	return n
}

// dequoteNumber removes '_' digit separators from a QNUMBER, reporting
// SQLite's "unrecognized token" error for a separator that is not between
// two digits. It reproduces sqlite3DequoteNumber, including its habit of
// reporting the partially rewritten token rather than the original.
func (p *parser) dequoteNumber(t token.Token) string {
	in := []byte(t.Text)
	buf := append([]byte(nil), in...)
	bHex := len(in) > 1 && in[0] == '0' && (in[1] == 'x' || in[1] == 'X')
	isok := func(c byte) bool {
		if bHex {
			return isHexDigit(c)
		}
		return c >= '0' && c <= '9'
	}
	out := 0
	for i := 0; i < len(buf); i++ {
		if buf[i] != '_' {
			buf[out] = buf[i]
			out++
			continue
		}
		prevOK := out > 0 && isok(buf[out-1])
		nextOK := i+1 < len(buf) && isok(buf[i+1])
		if !prevOK || !nextOK {
			p.fail(`unrecognized token: "`+string(buf)+`"`, t.Pos)
		}
	}
	return string(buf[:out])
}

func isHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}
