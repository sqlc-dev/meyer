// Package parser implements a hand-written recursive descent parser for the
// SQLite SQL dialect.
//
// The grammar is SQLite's src/parse.y, a Lemon LALR(1) grammar. Every
// non-trivial function here names the rule or rules it implements. Where
// parse.y resolves an ambiguity with a precedence annotation or with rule
// ordering, the equivalent decision is spelled out and commented.
//
// Errors match SQLite byte for byte. There are exactly three parser-level
// messages:
//
//	near "X": syntax error     the token X could not be shifted
//	unrecognized token: "X"    the tokenizer produced TK_ILLEGAL at X
//	incomplete input           the error fell on the end of the input,
//	                           where SQLite feeds a synthetic empty token
//
// Parsing is fail-fast: the first error aborts, as it does in SQLite, which
// reports exactly one parse error per statement.
package parser

import (
	"context"
	"io"
	"strings"

	"github.com/sqlc-dev/meyer/ast"
	"github.com/sqlc-dev/meyer/lexer"
	"github.com/sqlc-dev/meyer/token"
)

// Error is the error type returned for inputs meyer rejects. Message
// matches SQLite's parser wording (e.g. `near "FROM": syntax error`);
// Offset is the byte offset of the error in the input, or -1 if unknown.
type Error struct {
	Message string
	Offset  int
}

func (e *Error) Error() string { return e.Message }

// Parse reads SQL from r and returns one ast.Stmt per statement.
func Parse(ctx context.Context, r io.Reader) ([]ast.Stmt, error) {
	src, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return ParseString(string(src))
}

// ParseString parses a complete SQL script.
func ParseString(src string) (stmts []ast.Stmt, err error) {
	p := newParser(src)
	defer func() {
		if r := recover(); r != nil {
			b, ok := r.(bail)
			if !ok {
				panic(r)
			}
			stmts, err = nil, b.err
		}
	}()
	return p.parseScript(), nil
}

// ParseStatement parses exactly one statement and rejects trailing input.
func ParseStatement(src string) (ast.Stmt, error) {
	stmts, err := ParseString(src)
	if err != nil {
		return nil, err
	}
	if len(stmts) != 1 {
		return nil, &Error{Message: "expected exactly one statement", Offset: -1}
	}
	return stmts[0], nil
}

// ParseExpr parses a single expression. It exists for tests and tooling.
func ParseExpr(src string) (expr ast.Expr, err error) {
	p := newParser(src)
	defer func() {
		if r := recover(); r != nil {
			b, ok := r.(bail)
			if !ok {
				panic(r)
			}
			expr, err = nil, b.err
		}
	}()
	e := p.parseExpr(precLowest)
	if !p.at(token.EOF) {
		p.syntaxError()
	}
	return e, nil
}

// bail carries a parse error out through the recursive descent.
type bail struct{ err *Error }

type parser struct {
	src  string
	toks []token.Token
	i    int

	nVar    int            // bind parameters seen so far
	varNums map[string]int // named parameters already assigned a number
}

func newParser(src string) *parser {
	p := &parser{src: src, toks: lexer.Lex(src), varNums: map[string]int{}}
	p.checkIllegal()
	return p
}

// cur is the lookahead token.
func (p *parser) cur() token.Token { return p.toks[p.i] }

// peek returns the token n positions after the lookahead, clamped to EOF.
func (p *parser) peek(n int) token.Token {
	if p.i+n >= len(p.toks) {
		return p.toks[len(p.toks)-1]
	}
	return p.toks[p.i+n]
}

func (p *parser) at(k token.Kind) bool { return p.toks[p.i].Kind == k }

// advance consumes the lookahead and returns it.
func (p *parser) advance() token.Token {
	t := p.toks[p.i]
	if p.i < len(p.toks)-1 {
		p.i++
	}
	p.checkIllegal()
	return t
}

// accept consumes the lookahead if it has kind k.
func (p *parser) accept(k token.Kind) bool {
	if p.at(k) {
		p.advance()
		return true
	}
	return false
}

// expect consumes the lookahead, which must have kind k.
func (p *parser) expect(k token.Kind) token.Token {
	if !p.at(k) {
		p.syntaxError()
	}
	return p.advance()
}

// checkIllegal reports the tokenizer's TK_ILLEGAL as soon as the token
// becomes the lookahead. sqlite3RunParser abandons its loop when the
// tokenizer produces an illegal token, before handing it to the parser, so
// this error always wins over the syntax error the parser would raise at the
// same position.
func (p *parser) checkIllegal() {
	if t := p.toks[p.i]; t.Kind == token.ILLEGAL {
		panic(bail{&Error{Message: `unrecognized token: "` + t.Text + `"`, Offset: t.Pos}})
	}
}

// syntaxError aborts the parse at the lookahead token.
//
//	%syntax_error {
//	  if( TOKEN.z[0] ) parserSyntaxError(pParse, &TOKEN);
//	  else sqlite3ErrorMsg(pParse, "incomplete input");
//	}
func (p *parser) syntaxError() { p.syntaxErrorAt(p.cur()) }

func (p *parser) syntaxErrorAt(t token.Token) {
	if t.Kind == token.EOF {
		// At the end of input SQLite feeds synthetic SEMI and 0 tokens
		// whose text is empty, which %syntax_error reports this way.
		panic(bail{&Error{Message: "incomplete input", Offset: t.Pos}})
	}
	panic(bail{&Error{Message: `near "` + t.Text + `": syntax error`, Offset: t.Pos}})
}

// fail raises one of SQLite's non-positional parser messages.
func (p *parser) fail(msg string, offset int) {
	panic(bail{&Error{Message: msg, Offset: offset}})
}

// parseScript implements the top-level rules.
//
//	input ::= cmdlist. / cmdlist ::= cmdlist ecmd. / cmdlist ::= ecmd.
//	ecmd ::= SEMI. / ecmd ::= cmdx SEMI. / ecmd ::= explain cmdx SEMI.
func (p *parser) parseScript() []ast.Stmt {
	var stmts []ast.Stmt
	for !p.at(token.EOF) {
		if p.accept(token.SEMI) { // ecmd ::= SEMI: an empty statement
			continue
		}
		start := p.cur().Pos
		stmt := p.parseExplainable()
		end := p.prevEnd()
		// The statement span runs to its terminating semicolon so that
		// consumers can slice the source between statements.
		if p.at(token.SEMI) {
			end = p.cur().End
			p.advance()
		} else if !p.at(token.EOF) {
			p.syntaxError()
		}
		setStmtSpan(stmt, start, end)
		stmts = append(stmts, stmt)
	}
	return stmts
}

// parseExplainable implements "explain cmdx" plus the bare "cmdx".
//
//	explain ::= EXPLAIN. / explain ::= EXPLAIN QUERY PLAN.
func (p *parser) parseExplainable() ast.Stmt {
	if !p.at(token.EXPLAIN) {
		return p.parseCmd()
	}
	start := p.advance().Pos
	n := &ast.ExplainStmt{}
	if p.at(token.QUERY) {
		p.advance()
		p.expect(token.PLAN)
		n.QueryPlan = true
	}
	n.Stmt = p.parseCmd()
	n.Span = ast.Span{Start: start, Stop: p.prevEnd()}
	return n
}

// parseCmd dispatches on the leading keyword of a statement. Every "cmd"
// alternative in parse.y begins with a keyword, and none of them can be
// reached through %fallback: at the start of a statement each of those
// keywords has a shift action of its own, so it is never re-read as an ID.
func (p *parser) parseCmd() ast.Stmt {
	switch p.cur().Kind {
	case token.SELECT, token.VALUES, token.WITH:
		return p.parseSelectStmtOrDML()
	case token.INSERT, token.REPLACE:
		return p.parseInsert(nil, p.cur().Pos)
	case token.UPDATE:
		return p.parseUpdate(nil, p.cur().Pos)
	case token.DELETE:
		return p.parseDelete(nil, p.cur().Pos)
	case token.CREATE:
		return p.parseCreate()
	case token.DROP:
		return p.parseDrop()
	case token.ALTER:
		return p.parseAlterTable()
	case token.BEGIN:
		return p.parseBegin()
	case token.COMMIT, token.END:
		return p.parseCommit()
	case token.ROLLBACK:
		return p.parseRollback()
	case token.SAVEPOINT:
		return p.parseSavepoint()
	case token.RELEASE:
		return p.parseRelease()
	case token.ATTACH:
		return p.parseAttach()
	case token.DETACH:
		return p.parseDetach()
	case token.PRAGMA:
		return p.parsePragma()
	case token.VACUUM:
		return p.parseVacuum()
	case token.ANALYZE:
		return p.parseAnalyze()
	case token.REINDEX:
		return p.parseReindex()
	}
	p.syntaxError()
	return nil
}

// prevEnd is the end offset of the most recently consumed token.
func (p *parser) prevEnd() int {
	if p.i == 0 {
		return 0
	}
	return p.toks[p.i-1].End
}

// span builds a Span from a start offset to the last consumed token.
func (p *parser) span(start int) ast.Span {
	return ast.Span{Start: start, Stop: p.prevEnd()}
}

// setStmtSpan widens a statement's span to cover its terminating semicolon.
// Every node embeds ast.Span, which supplies SetSpan.
func setStmtSpan(stmt ast.Stmt, start, end int) {
	if s, ok := stmt.(interface{ SetSpan(ast.Span) }); ok {
		s.SetSpan(ast.Span{Start: start, Stop: end})
	}
}

// Name productions. parse.y expresses these as token classes plus the
// %fallback ID set; token.IsName and friends encode the membership, and
// these helpers turn an accepted token into an Ident.

// expectName implements "nm": idj | STRING.
func (p *parser) expectName() *ast.Ident {
	if !token.IsName(p.cur().Kind) {
		p.syntaxError()
	}
	return identOf(p.advance())
}

// expectIDS implements the "ids" class: ID | STRING.
func (p *parser) expectIDS() *ast.Ident {
	if !token.IsIDS(p.cur().Kind) {
		p.syntaxError()
	}
	return identOf(p.advance())
}

// identOf builds an Ident from an accepted name token, undoing SQLite's four
// quoting styles.
func identOf(t token.Token) *ast.Ident {
	id := &ast.Ident{
		Span: ast.Span{Start: t.Pos, Stop: t.End},
		Raw:  t.Text,
		Name: t.Text,
	}
	if len(t.Text) >= 2 {
		switch t.Text[0] {
		case '"', '\'', '`':
			id.Quote = ast.QuoteStyle(t.Text[0])
			id.Name = dequote(t.Text, t.Text[0])
		case '[':
			id.Quote = ast.QuoteSquare
			id.Name = t.Text[1 : len(t.Text)-1]
		}
	}
	return id
}

// dequote strips the delimiters of a quoted token and collapses the doubled
// delimiters SQLite uses for escaping. Mirrors sqlite3Dequote.
func dequote(s string, delim byte) string {
	body := s[1:]
	if n := len(body); n > 0 && body[n-1] == delim {
		body = body[:n-1]
	}
	d := string([]byte{delim})
	if !strings.Contains(body, d) {
		return body
	}
	return strings.ReplaceAll(body, d+d, d)
}

// qualifiedName implements "nm dbnm", an optionally schema-qualified object
// name. SQLite writes the qualifier first: in "a.b", a is the schema.
//
//	fullname ::= nm. / fullname ::= nm DOT nm.
//	dbnm ::= . / dbnm ::= DOT nm.
func (p *parser) qualifiedName() *ast.QualifiedName {
	first := p.expectName()
	n := &ast.QualifiedName{Span: first.Span, Name: first}
	if p.at(token.DOT) {
		p.advance()
		second := p.expectName()
		n.Schema, n.Name = first, second
		n.Span = ast.Span{Start: first.Pos(), Stop: second.End()}
	}
	return n
}

// xfullname adds the optional alias that INSERT, UPDATE and DELETE targets
// accept.
//
//	xfullname ::= nm [DOT nm] [AS nm].
func (p *parser) xfullname() (*ast.QualifiedName, *ast.Ident) {
	name := p.qualifiedName()
	var alias *ast.Ident
	if p.accept(token.AS) {
		alias = p.expectName()
	}
	return name, alias
}
