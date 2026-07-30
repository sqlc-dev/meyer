package ast

// This file declares the expression nodes. Each carries a comment naming the
// parse.y rule(s) it comes from.

// QuoteStyle records how an identifier was spelled in the source. SQLite
// accepts four quoting styles for identifiers plus the unquoted form, and
// consumers (sqlc among them) need to tell them apart.
type QuoteStyle byte

const (
	QuoteNone   QuoteStyle = 0
	QuoteDouble QuoteStyle = '"'
	QuoteBack   QuoteStyle = '`'
	QuoteSquare QuoteStyle = '['
	QuoteSingle QuoteStyle = '\'' // a string literal used in a name position
)

// Ident is a name: a table, column, function, alias, collation, ...
//
//	nm ::= idj. / nm ::= STRING.
type Ident struct {
	Span
	Name  string     `json:"name"`  // dequoted
	Raw   string     `json:"raw"`   // exactly as it appeared
	Quote QuoteStyle `json:"quote"` // QuoteNone when unquoted
}

func (*Ident) exprNode()          {}
func (n *Ident) Children() []Node { return nil }

// QualifiedName is a "nm dbnm" pair: an optionally schema-qualified object
// name. Schema is nil when the name was unqualified.
//
//	fullname ::= nm. / fullname ::= nm DOT nm.
type QualifiedName struct {
	Span
	Schema *Ident `json:"schema,omitempty"`
	Name   *Ident `json:"name"`
}

func (n *QualifiedName) Children() []Node { return nodes(n.Schema, n.Name) }

// QualifiedRef is a dotted column reference with two or three parts.
//
//	expr ::= nm DOT nm. / expr ::= nm DOT nm DOT nm.
type QualifiedRef struct {
	Span
	Parts []*Ident `json:"parts"`
}

func (*QualifiedRef) exprNode() {}
func (n *QualifiedRef) Children() []Node {
	out := make([]Node, len(n.Parts))
	for i, p := range n.Parts {
		out[i] = p
	}
	return out
}

// Star is "*" or "tbl.*" in a result column list.
//
//	selcollist ::= sclp scanpt STAR. / selcollist ::= sclp scanpt nm DOT STAR.
type Star struct {
	Span
	Table *Ident `json:"table,omitempty"`
}

func (*Star) exprNode()          {}
func (n *Star) Children() []Node { return nodes(n.Table) }

// LiteralKind distinguishes the literal forms of the "term" production.
type LiteralKind int

const (
	LitNull LiteralKind = iota
	LitInteger
	LitFloat
	LitString
	LitBlob
	LitCurrentDate
	LitCurrentTime
	LitCurrentTimestamp
)

// Literal is a constant.
//
//	term ::= NULL|FLOAT|BLOB. / term ::= STRING. / term ::= INTEGER.
//	term ::= QNUMBER. / term ::= CTIME_KW.
type Literal struct {
	Span
	Kind  LiteralKind `json:"kind"`
	Value string      `json:"value"` // dequoted for strings, raw otherwise
	Raw   string      `json:"raw"`
}

func (*Literal) exprNode()          {}
func (n *Literal) Children() []Node { return nil }

// ParamKind distinguishes SQLite's four bind-parameter spellings.
type ParamKind int

const (
	ParamAnon   ParamKind = iota // ?
	ParamNumber                  // ?NNN
	ParamColon                   // :name
	ParamAt                      // @name
	ParamDollar                  // $name
)

// BindParam is a parameter marker. Number is assigned the way
// sqlite3ExprAssignVarNumber does: explicit for ?NNN, otherwise one past the
// highest number used so far.
//
//	expr ::= VARIABLE.
type BindParam struct {
	Span
	Kind   ParamKind `json:"kind"`
	Number int       `json:"number"`
	Name   string    `json:"name,omitempty"` // without the sigil
	Raw    string    `json:"raw"`
}

func (*BindParam) exprNode()          {}
func (n *BindParam) Children() []Node { return nil }

// Operator identifies a unary or binary operator.
type Operator int

const (
	OpNone Operator = iota
	// Binary
	OpOr
	OpAnd
	OpEq
	OpNe
	OpLt
	OpLe
	OpGt
	OpGe
	OpBitAnd
	OpBitOr
	OpLShift
	OpRShift
	OpAdd
	OpSub
	OpMul
	OpDiv
	OpMod
	OpConcat
	OpPtr  // ->
	OpPtr2 // ->>
	// Unary
	OpNot
	OpBitNot
	OpPlus
	OpMinus
)

var opText = map[Operator]string{
	OpOr: "OR", OpAnd: "AND", OpEq: "=", OpNe: "<>", OpLt: "<", OpLe: "<=",
	OpGt: ">", OpGe: ">=", OpBitAnd: "&", OpBitOr: "|", OpLShift: "<<",
	OpRShift: ">>", OpAdd: "+", OpSub: "-", OpMul: "*", OpDiv: "/",
	OpMod: "%", OpConcat: "||", OpPtr: "->", OpPtr2: "->>", OpNot: "NOT",
	OpBitNot: "~", OpPlus: "+", OpMinus: "-",
}

func (o Operator) String() string { return opText[o] }

// BinaryExpr is an infix operator application.
//
//	expr ::= expr AND expr. and the other %left/%right operator rules.
type BinaryExpr struct {
	Span
	Op Operator `json:"op"`
	X  Expr     `json:"x"`
	Y  Expr     `json:"y"`
}

func (*BinaryExpr) exprNode()          {}
func (n *BinaryExpr) Children() []Node { return nodes(n.X, n.Y) }

// UnaryExpr is a prefix operator application.
//
//	expr ::= NOT expr. / expr ::= BITNOT expr. / expr ::= PLUS|MINUS expr.
type UnaryExpr struct {
	Span
	Op Operator `json:"op"`
	X  Expr     `json:"x"`
}

func (*UnaryExpr) exprNode()          {}
func (n *UnaryExpr) Children() []Node { return nodes(n.X) }

// ParenExpr preserves an explicit parenthesisation. SQLite discards these
// (expr ::= LP expr RP just yields the inner expression), but keeping them
// makes the round-trip renderer exact.
type ParenExpr struct {
	Span
	X Expr `json:"x"`
}

func (*ParenExpr) exprNode()          {}
func (n *ParenExpr) Children() []Node { return nodes(n.X) }

// LikeExpr is a LIKE/GLOB/REGEXP/MATCH application, optionally negated and
// optionally with an ESCAPE operand.
//
//	expr ::= expr likeop expr. / expr ::= expr likeop expr ESCAPE expr.
type LikeExpr struct {
	Span
	Op     *Ident `json:"op"` // the LIKE/GLOB/REGEXP/MATCH token, as written
	Not    bool   `json:"not"`
	X      Expr   `json:"x"`
	Y      Expr   `json:"y"`
	Escape Expr   `json:"escape,omitempty"`
}

func (*LikeExpr) exprNode()          {}
func (n *LikeExpr) Children() []Node { return nodes(n.Op, n.X, n.Y, n.Escape) }

// IsExpr is "IS", "IS NOT", "IS DISTINCT FROM" or "IS NOT DISTINCT FROM".
//
//	expr ::= expr IS expr. and its three siblings.
type IsExpr struct {
	Span
	Not      bool `json:"not"`
	Distinct bool `json:"distinct"` // the DISTINCT FROM spelling was used
	X        Expr `json:"x"`
	Y        Expr `json:"y"`
}

func (*IsExpr) exprNode()          {}
func (n *IsExpr) Children() []Node { return nodes(n.X, n.Y) }

// NullTest distinguishes the three spellings of a postfix null test. They
// are not interchangeable: "expr NOT NULL" takes its precedence from NOT,
// while ISNULL and NOTNULL are comparison-level operators, so "NOT x NOTNULL"
// and "NOT x NOT NULL" are different trees.
type NullTest int

const (
	TestIsNull       NullTest = iota // ISNULL
	TestNotNull                      // NOTNULL
	TestNotNullWords                 // NOT NULL
)

// NullCheckExpr is a postfix null test.
//
//	expr ::= expr ISNULL|NOTNULL. / expr ::= expr NOT NULL.
type NullCheckExpr struct {
	Span
	Test NullTest `json:"test"`
	X    Expr     `json:"x"`
}

func (*NullCheckExpr) exprNode()          {}
func (n *NullCheckExpr) Children() []Node { return nodes(n.X) }

// BetweenExpr is "x BETWEEN lo AND hi", optionally negated.
//
//	expr ::= expr between_op expr AND expr.
type BetweenExpr struct {
	Span
	Not bool `json:"not"`
	X   Expr `json:"x"`
	Lo  Expr `json:"lo"`
	Hi  Expr `json:"hi"`
}

func (*BetweenExpr) exprNode()          {}
func (n *BetweenExpr) Children() []Node { return nodes(n.X, n.Lo, n.Hi) }

// InExpr is "x IN ...". Exactly one of List, Select and Table is set; an
// empty parenthesised list leaves all three unset with Parens true.
//
//	expr ::= expr in_op LP exprlist RP.
//	expr ::= expr in_op LP select RP.
//	expr ::= expr in_op nm dbnm paren_exprlist.
type InExpr struct {
	Span
	Not     bool           `json:"not"`
	X       Expr           `json:"x"`
	Parens  bool           `json:"parens"`
	List    []Expr         `json:"list,omitempty"`
	Select  *SelectStmt    `json:"select,omitempty"`
	Table   *QualifiedName `json:"table,omitempty"`
	Args    []Expr         `json:"args,omitempty"` // table-valued function args
	HasArgs bool           `json:"hasArgs,omitempty"`
}

func (*InExpr) exprNode() {}
func (n *InExpr) Children() []Node {
	out := nodes(n.X, n.Select, n.Table)
	out = appendNodes(out, n.List)
	out = appendNodes(out, n.Args)
	return out
}

// CaseExpr is a CASE expression. Operand is nil for the searched form.
//
//	expr ::= CASE case_operand case_exprlist case_else END.
type CaseExpr struct {
	Span
	Operand Expr        `json:"operand,omitempty"`
	Whens   []*CaseWhen `json:"whens"`
	Else    Expr        `json:"else,omitempty"`
}

func (*CaseExpr) exprNode() {}
func (n *CaseExpr) Children() []Node {
	out := nodes(n.Operand)
	out = appendNodes(out, n.Whens)
	return append(out, nodes(n.Else)...)
}

// CaseWhen is one WHEN/THEN pair.
//
//	case_exprlist ::= case_exprlist WHEN expr THEN expr.
type CaseWhen struct {
	Span
	When Expr `json:"when"`
	Then Expr `json:"then"`
}

func (n *CaseWhen) Children() []Node { return nodes(n.When, n.Then) }

// CastExpr is "CAST(expr AS type)".
//
//	expr ::= CAST LP expr AS typetoken RP.
type CastExpr struct {
	Span
	X    Expr      `json:"x"`
	Type *TypeName `json:"type"`
}

func (*CastExpr) exprNode()          {}
func (n *CastExpr) Children() []Node { return nodes(n.X, n.Type) }

// CollateExpr is "expr COLLATE name".
//
//	expr ::= expr COLLATE ids.
type CollateExpr struct {
	Span
	X    Expr   `json:"x"`
	Name *Ident `json:"name"`
}

func (*CollateExpr) exprNode()          {}
func (n *CollateExpr) Children() []Node { return nodes(n.X, n.Name) }

// FuncCall is a function invocation, including aggregate and window forms.
//
//	expr ::= idj LP distinct exprlist RP [filter_over].
//	expr ::= idj LP distinct exprlist ORDER BY sortlist RP [filter_over].
//	expr ::= idj LP STAR RP [filter_over].
type FuncCall struct {
	Span
	Name     *Ident          `json:"name"`
	Distinct bool            `json:"distinct"`
	All      bool            `json:"all"` // the redundant ALL qualifier
	Star     bool            `json:"star"`
	Args     []Expr          `json:"args,omitempty"`
	OrderBy  []*OrderingTerm `json:"orderBy,omitempty"` // aggregate inner ORDER BY
	Filter   Expr            `json:"filter,omitempty"`  // FILTER (WHERE ...)
	Over     *WindowDef      `json:"over,omitempty"`
}

func (*FuncCall) exprNode() {}
func (n *FuncCall) Children() []Node {
	out := nodes(n.Name)
	out = appendNodes(out, n.Args)
	out = appendNodes(out, n.OrderBy)
	return append(out, nodes(n.Filter, n.Over)...)
}

// ExistsExpr is "EXISTS (select)".
//
//	expr ::= EXISTS LP select RP.
type ExistsExpr struct {
	Span
	Select *SelectStmt `json:"select"`
}

func (*ExistsExpr) exprNode()          {}
func (n *ExistsExpr) Children() []Node { return nodes(n.Select) }

// SubqueryExpr is a parenthesised SELECT used as a scalar.
//
//	expr ::= LP select RP.
type SubqueryExpr struct {
	Span
	Select *SelectStmt `json:"select"`
}

func (*SubqueryExpr) exprNode()          {}
func (n *SubqueryExpr) Children() []Node { return nodes(n.Select) }

// VectorExpr is a parenthesised row value with two or more elements.
//
//	expr ::= LP nexprlist COMMA expr RP.
type VectorExpr struct {
	Span
	List []Expr `json:"list"`
}

func (*VectorExpr) exprNode() {}
func (n *VectorExpr) Children() []Node {
	out := make([]Node, len(n.List))
	for i, e := range n.List {
		out[i] = e
	}
	return out
}

// RaiseExpr is the trigger-only RAISE() form.
//
//	expr ::= RAISE LP IGNORE RP. / expr ::= RAISE LP raisetype COMMA expr RP.
type RaiseExpr struct {
	Span
	Action  string `json:"action"` // IGNORE, ROLLBACK, ABORT or FAIL
	Message Expr   `json:"message,omitempty"`
}

func (*RaiseExpr) exprNode()          {}
func (n *RaiseExpr) Children() []Node { return nodes(n.Message) }

// TypeName is a column or cast type: one or more identifier tokens followed
// by an optional size specification.
//
//	typetoken ::= typename [LP signed [COMMA signed] RP].
type TypeName struct {
	Span
	Name string   `json:"name"`           // the identifier tokens, space-joined
	Args []string `json:"args,omitempty"` // the signed numbers, as written
	Raw  string   `json:"raw"`            // the whole source span
}

func (n *TypeName) Children() []Node { return nil }

// nodes builds a Children() slice, dropping nil interfaces and nil
// pointers. Optional fields are common, so a node whose children are all
// absent allocates nothing.
func nodes(list ...Node) []Node {
	var out []Node
	for _, n := range list {
		if !isNil(n) {
			out = append(out, n)
		}
	}
	return out
}

// appendNodes appends a slice of concrete node pointers to a Children()
// result. It exists because every node with a repeated field would
// otherwise spell the same three-line loop out again.
func appendNodes[T Node](out []Node, list []T) []Node {
	for _, n := range list {
		out = append(out, n)
	}
	return out
}
