package ast

// ConflictAction is the conflict-resolution algorithm of an ON CONFLICT or
// OR clause.
type ConflictAction int

const (
	ConflictDefault ConflictAction = iota
	ConflictRollback
	ConflictAbort
	ConflictFail
	ConflictIgnore
	ConflictReplace
)

var conflictText = map[ConflictAction]string{
	ConflictRollback: "ROLLBACK", ConflictAbort: "ABORT", ConflictFail: "FAIL",
	ConflictIgnore: "IGNORE", ConflictReplace: "REPLACE",
}

func (a ConflictAction) String() string { return conflictText[a] }

// InsertStmt is INSERT, INSERT OR <action>, or REPLACE.
//
//	cmd ::= with insert_cmd INTO xfullname idlist_opt select upsert.
//	cmd ::= with insert_cmd INTO xfullname idlist_opt DEFAULT VALUES returning.
type InsertStmt struct {
	Span
	With          *With           `json:"with,omitempty"`
	Replace       bool            `json:"replace,omitempty"` // spelled REPLACE
	OrConflict    ConflictAction  `json:"orConflict,omitempty"`
	Table         *QualifiedName  `json:"table"`
	Alias         *Ident          `json:"alias,omitempty"`
	Columns       []*Ident        `json:"columns,omitempty"`
	Select        *SelectStmt     `json:"select,omitempty"`
	DefaultValues bool            `json:"defaultValues,omitempty"`
	Upserts       []*Upsert       `json:"upserts,omitempty"`
	Returning     []*ResultColumn `json:"returning,omitempty"`
}

func (*InsertStmt) stmtNode() {}
func (n *InsertStmt) Children() []Node {
	out := nodes(n.With, n.Table, n.Alias)
	for _, c := range n.Columns {
		out = append(out, c)
	}
	out = append(out, nodes(n.Select)...)
	for _, u := range n.Upserts {
		out = append(out, u)
	}
	for _, r := range n.Returning {
		out = append(out, r)
	}
	return out
}

// Upsert is one ON CONFLICT clause of an INSERT.
//
//	upsert ::= ON CONFLICT [LP sortlist RP where_opt] DO UPDATE SET setlist where_opt.
//	upsert ::= ON CONFLICT [LP sortlist RP where_opt] DO NOTHING.
type Upsert struct {
	Span
	Target      []*OrderingTerm `json:"target,omitempty"`
	TargetWhere Expr            `json:"targetWhere,omitempty"`
	DoNothing   bool            `json:"doNothing,omitempty"`
	Set         []*SetPair      `json:"set,omitempty"`
	Where       Expr            `json:"where,omitempty"`
}

func (n *Upsert) Children() []Node {
	var out []Node
	for _, t := range n.Target {
		out = append(out, t)
	}
	out = append(out, nodes(n.TargetWhere)...)
	for _, s := range n.Set {
		out = append(out, s)
	}
	return append(out, nodes(n.Where)...)
}

// SetPair is one assignment of an UPDATE ... SET or upsert DO UPDATE SET
// list. Columns holds more than one name for the "(a,b) = expr" form.
//
//	setlist ::= nm EQ expr. / setlist ::= LP idlist RP EQ expr.
type SetPair struct {
	Span
	Columns []*Ident `json:"columns"`
	Paren   bool     `json:"paren,omitempty"`
	Value   Expr     `json:"value"`
}

func (n *SetPair) Children() []Node {
	out := make([]Node, 0, len(n.Columns)+1)
	for _, c := range n.Columns {
		out = append(out, c)
	}
	return append(out, nodes(n.Value)...)
}

// UpdateStmt is an UPDATE statement.
//
//	cmd ::= with UPDATE orconf xfullname indexed_opt SET setlist from
//	        where_opt_ret.
//
// The pinned build defines neither SQLITE_ENABLE_UPDATE_DELETE_LIMIT nor
// SQLITE_UDL_CAPABLE_PARSER, so UPDATE has no ORDER BY or LIMIT at all: they
// are a plain syntax error rather than a grammar-action message.
type UpdateStmt struct {
	Span
	With       *With           `json:"with,omitempty"`
	OrConflict ConflictAction  `json:"orConflict,omitempty"`
	Table      *QualifiedName  `json:"table"`
	Alias      *Ident          `json:"alias,omitempty"`
	IndexedBy  *Ident          `json:"indexedBy,omitempty"`
	NotIndexed bool            `json:"notIndexed,omitempty"`
	Set        []*SetPair      `json:"set"`
	From       []*TableRef     `json:"from,omitempty"`
	Where      Expr            `json:"where,omitempty"`
	Returning  []*ResultColumn `json:"returning,omitempty"`
}

func (*UpdateStmt) stmtNode() {}
func (n *UpdateStmt) Children() []Node {
	out := nodes(n.With, n.Table, n.Alias, n.IndexedBy)
	for _, s := range n.Set {
		out = append(out, s)
	}
	for _, f := range n.From {
		out = append(out, f)
	}
	out = append(out, nodes(n.Where)...)
	for _, r := range n.Returning {
		out = append(out, r)
	}
	return out
}

// DeleteStmt is a DELETE statement.
//
//	cmd ::= with DELETE FROM xfullname indexed_opt where_opt_ret.
//
// As for UPDATE, the pinned build's DELETE takes no ORDER BY or LIMIT.
type DeleteStmt struct {
	Span
	With       *With           `json:"with,omitempty"`
	Table      *QualifiedName  `json:"table"`
	Alias      *Ident          `json:"alias,omitempty"`
	IndexedBy  *Ident          `json:"indexedBy,omitempty"`
	NotIndexed bool            `json:"notIndexed,omitempty"`
	Where      Expr            `json:"where,omitempty"`
	Returning  []*ResultColumn `json:"returning,omitempty"`
}

func (*DeleteStmt) stmtNode() {}
func (n *DeleteStmt) Children() []Node {
	out := nodes(n.With, n.Table, n.Alias, n.IndexedBy, n.Where)
	for _, r := range n.Returning {
		out = append(out, r)
	}
	return out
}
