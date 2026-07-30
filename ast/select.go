package ast

// SelectStmt is a complete SELECT: an optional WITH clause followed by one
// or more query cores joined by compound operators.
//
//	select ::= WITH [RECURSIVE] wqlist selectnowith. / select ::= selectnowith.
//	selectnowith ::= selectnowith multiselect_op oneselect.
//
// Cores is never empty and Ops has exactly len(Cores)-1 entries. ORDER BY
// and LIMIT belong to the core they were written on: SQLite's grammar hangs
// them off oneselect, and rejects a leading one only in a grammar action
// ("ORDER BY clause should come after UNION not before"), which is not a
// parse error.
type SelectStmt struct {
	Span
	With  *With        `json:"with,omitempty"`
	Cores []SelectCore `json:"cores"`
	Ops   []CompoundOp `json:"ops,omitempty"`
}

func (*SelectStmt) stmtNode() {}
func (n *SelectStmt) Children() []Node {
	out := nodes(n.With)
	out = appendNodes(out, n.Cores)
	return out
}

// SelectCore is one term of a compound select: a SELECT or a VALUES clause.
type SelectCore interface {
	Node
	selectCoreNode()
}

// CompoundOp is UNION, UNION ALL, EXCEPT or INTERSECT.
//
//	multiselect_op ::= UNION. / UNION ALL. / EXCEPT|INTERSECT.
type CompoundOp struct {
	Span
	Op  string `json:"op"` // "UNION", "EXCEPT", "INTERSECT"
	All bool   `json:"all"`
}

func (n *CompoundOp) Children() []Node { return nil }

// Distinct records the DISTINCT/ALL qualifier of a SELECT.
type Distinct int

const (
	DistinctNone Distinct = iota
	DistinctDistinct
	DistinctAll
)

// SelectQuery is the SELECT form of a query core.
//
//	oneselect ::= SELECT distinct selcollist from where_opt groupby_opt
//	              having_opt [window_clause] orderby_opt limit_opt.
type SelectQuery struct {
	Span
	Distinct Distinct        `json:"distinct,omitempty"`
	Columns  []*ResultColumn `json:"columns"`
	From     []*TableRef     `json:"from,omitempty"`
	Where    Expr            `json:"where,omitempty"`
	GroupBy  []Expr          `json:"groupBy,omitempty"`
	Having   Expr            `json:"having,omitempty"`
	Windows  []*WindowDef    `json:"windows,omitempty"`
	OrderBy  []*OrderingTerm `json:"orderBy,omitempty"`
	Limit    *Limit          `json:"limit,omitempty"`
}

func (*SelectQuery) selectCoreNode() {}
func (n *SelectQuery) Children() []Node {
	var out []Node
	out = appendNodes(out, n.Columns)
	out = appendNodes(out, n.From)
	out = append(out, nodes(n.Where)...)
	out = appendNodes(out, n.GroupBy)
	out = append(out, nodes(n.Having)...)
	out = appendNodes(out, n.Windows)
	out = appendNodes(out, n.OrderBy)
	return append(out, nodes(n.Limit)...)
}

// ValuesClause is the VALUES form of a query core. SQLite models multi-row
// VALUES as a compound of single-row selects; meyer keeps it as one node.
//
//	values ::= VALUES LP nexprlist RP.
//	mvalues ::= values COMMA LP nexprlist RP. / mvalues COMMA LP nexprlist RP.
type ValuesClause struct {
	Span
	Rows [][]Expr `json:"rows"`
}

func (*ValuesClause) selectCoreNode() {}
func (n *ValuesClause) Children() []Node {
	var out []Node
	for _, row := range n.Rows {
		for _, e := range row {
			out = append(out, e)
		}
	}
	return out
}

// ResultColumn is one entry of a select list.
//
//	selcollist ::= sclp scanpt expr scanpt as.
type ResultColumn struct {
	Span
	Expr  Expr   `json:"expr"`
	Alias *Ident `json:"alias,omitempty"`
	HasAs bool   `json:"hasAs,omitempty"` // the alias was introduced by AS
}

func (n *ResultColumn) Children() []Node { return nodes(n.Expr, n.Alias) }

// JoinType is the bitmask of join keywords, matching sqlite3JoinType.
type JoinType int

const (
	JoinInner JoinType = 1 << iota
	JoinCross
	JoinNatural
	JoinLeft
	JoinRight
	JoinOuter
	JoinComma // the item was separated by "," rather than a JOIN keyword
)

// JoinOperator is the operator attaching a FROM item to the one before it.
//
//	joinop ::= COMMA|JOIN. / JOIN_KW JOIN. / JOIN_KW nm JOIN.
//	joinop ::= JOIN_KW nm nm JOIN.
type JoinOperator struct {
	Span
	Type  JoinType `json:"type"`
	Words []string `json:"words,omitempty"` // the keywords as written
}

func (n *JoinOperator) Children() []Node { return nil }

// TableRef is one item of a FROM clause. Following SQLite's SrcList, the
// list is flat and each item records how it attaches to the previous one.
// Exactly one of Name, Select and List describes the source.
//
//	seltablist ::= stl_prefix nm dbnm as [indexed_by] on_using.
//	seltablist ::= stl_prefix nm dbnm LP exprlist RP as on_using.
//	seltablist ::= stl_prefix LP select RP as on_using.
//	seltablist ::= stl_prefix LP seltablist RP as on_using.
type TableRef struct {
	Span
	Join       *JoinOperator  `json:"join,omitempty"` // nil for the first item
	Name       *QualifiedName `json:"name,omitempty"`
	Args       []Expr         `json:"args,omitempty"` // table-valued function
	HasArgs    bool           `json:"hasArgs,omitempty"`
	Select     *SelectStmt    `json:"select,omitempty"`
	List       []*TableRef    `json:"list,omitempty"`
	Alias      *Ident         `json:"alias,omitempty"`
	HasAs      bool           `json:"hasAs,omitempty"`
	IndexedBy  *Ident         `json:"indexedBy,omitempty"`
	NotIndexed bool           `json:"notIndexed,omitempty"`
	On         Expr           `json:"on,omitempty"`
	Using      []*Ident       `json:"using,omitempty"`
}

func (n *TableRef) Children() []Node {
	out := nodes(n.Join, n.Name)
	out = appendNodes(out, n.Args)
	out = append(out, nodes(n.Select)...)
	out = appendNodes(out, n.List)
	out = append(out, nodes(n.Alias, n.IndexedBy, n.On)...)
	out = appendNodes(out, n.Using)
	return out
}

// SortOrder is ASC, DESC, or unstated.
type SortOrder int

const (
	SortDefault SortOrder = iota
	SortAsc
	SortDesc
)

// NullsOrder is NULLS FIRST, NULLS LAST, or unstated.
type NullsOrder int

const (
	NullsDefault NullsOrder = iota
	NullsFirst
	NullsLast
)

// OrderingTerm is one entry of an ORDER BY (or index/aggregate) sort list.
//
//	sortlist ::= sortlist COMMA expr sortorder nulls.
type OrderingTerm struct {
	Span
	Expr  Expr       `json:"expr"`
	Order SortOrder  `json:"order,omitempty"`
	Nulls NullsOrder `json:"nulls,omitempty"`
}

func (n *OrderingTerm) Children() []Node { return nodes(n.Expr) }

// Limit is a LIMIT clause. In the "LIMIT x, y" spelling SQLite swaps the
// operands, so Count is y and Offset is x; Comma records the spelling.
//
//	limit_opt ::= LIMIT expr. / LIMIT expr OFFSET expr. / LIMIT expr COMMA expr.
type Limit struct {
	Span
	Count  Expr `json:"count"`
	Offset Expr `json:"offset,omitempty"`
	Comma  bool `json:"comma,omitempty"`
}

func (n *Limit) Children() []Node { return nodes(n.Count, n.Offset) }

// With is a WITH clause.
//
//	with ::= WITH [RECURSIVE] wqlist.
type With struct {
	Span
	Recursive bool   `json:"recursive,omitempty"`
	CTEs      []*CTE `json:"ctes"`
}

func (n *With) Children() []Node {
	out := make([]Node, len(n.CTEs))
	for i, c := range n.CTEs {
		out[i] = c
	}
	return out
}

// Materialized records the optional MATERIALIZED hint on a CTE.
type Materialized int

const (
	MaterializedAny Materialized = iota
	MaterializedYes
	MaterializedNo
)

// CTE is one common table expression.
//
//	wqitem ::= withnm eidlist_opt wqas LP select RP.
type CTE struct {
	Span
	Name         *Ident       `json:"name"`
	Columns      []*Ident     `json:"columns,omitempty"`
	Materialized Materialized `json:"materialized,omitempty"`
	Select       *SelectStmt  `json:"select"`
}

func (n *CTE) Children() []Node {
	out := nodes(n.Name)
	out = appendNodes(out, n.Columns)
	return append(out, nodes(n.Select)...)
}

// FrameType is RANGE, ROWS or GROUPS.
type FrameType int

const (
	FrameNone FrameType = iota
	FrameRange
	FrameRows
	FrameGroups
)

// FrameBoundType enumerates the window frame endpoints.
type FrameBoundType int

const (
	BoundUnboundedPreceding FrameBoundType = iota
	BoundPreceding
	BoundCurrentRow
	BoundFollowing
	BoundUnboundedFollowing
)

// FrameExclude is the EXCLUDE clause of a frame specification.
type FrameExclude int

const (
	ExcludeNone FrameExclude = iota
	ExcludeNoOthers
	ExcludeCurrentRow
	ExcludeGroup
	ExcludeTies
)

// FrameBound is one endpoint of a window frame.
//
//	frame_bound ::= expr PRECEDING|FOLLOWING. / CURRENT ROW.
//	frame_bound_s ::= UNBOUNDED PRECEDING. / frame_bound_e ::= UNBOUNDED FOLLOWING.
type FrameBound struct {
	Span
	Type FrameBoundType `json:"type"`
	Expr Expr           `json:"expr,omitempty"`
}

func (n *FrameBound) Children() []Node { return nodes(n.Expr) }

// WindowDef is a window definition: the body of an OVER(...) or of one entry
// in a WINDOW clause. A bare "OVER name" sets Base and nothing else.
//
//	window ::= [nm] [PARTITION BY nexprlist] [ORDER BY sortlist] frame_opt.
//	windowdefn ::= nm AS LP window RP.
type WindowDef struct {
	Span
	Name       *Ident          `json:"name,omitempty"` // WINDOW <name> AS (...)
	Base       *Ident          `json:"base,omitempty"` // inherited window name
	NameOnly   bool            `json:"nameOnly,omitempty"`
	Partition  []Expr          `json:"partition,omitempty"`
	OrderBy    []*OrderingTerm `json:"orderBy,omitempty"`
	Frame      FrameType       `json:"frame,omitempty"`
	StartBound *FrameBound     `json:"startBound,omitempty"`
	EndBound   *FrameBound     `json:"endBound,omitempty"`
	Exclude    FrameExclude    `json:"exclude,omitempty"`
}

func (n *WindowDef) Children() []Node {
	out := nodes(n.Name, n.Base)
	out = appendNodes(out, n.Partition)
	out = appendNodes(out, n.OrderBy)
	return append(out, nodes(n.StartBound, n.EndBound)...)
}
