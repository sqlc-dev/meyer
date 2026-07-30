package ast

// TransactionType is the DEFERRED/IMMEDIATE/EXCLUSIVE qualifier of BEGIN.
type TransactionType int

const (
	TxnDeferred TransactionType = iota
	TxnImmediate
	TxnExclusive
)

// BeginStmt is BEGIN [DEFERRED|IMMEDIATE|EXCLUSIVE] [TRANSACTION [name]].
//
//	cmd ::= BEGIN transtype trans_opt.
type BeginStmt struct {
	Span
	Type    TransactionType `json:"type,omitempty"`
	HasType bool            `json:"hasType,omitempty"`
	Name    *Ident          `json:"name,omitempty"`
}

func (*BeginStmt) stmtNode()          {}
func (n *BeginStmt) Children() []Node { return nodes(n.Name) }

// CommitStmt is COMMIT or END, optionally followed by TRANSACTION [name].
//
//	cmd ::= COMMIT|END trans_opt.
type CommitStmt struct {
	Span
	Keyword string `json:"keyword"` // "COMMIT" or "END"
	Name    *Ident `json:"name,omitempty"`
}

func (*CommitStmt) stmtNode()          {}
func (n *CommitStmt) Children() []Node { return nodes(n.Name) }

// RollbackStmt is ROLLBACK, with or without a savepoint.
//
//	cmd ::= ROLLBACK trans_opt.
//	cmd ::= ROLLBACK trans_opt TO savepoint_opt nm.
type RollbackStmt struct {
	Span
	Name      *Ident `json:"name,omitempty"`      // TRANSACTION <name>
	Savepoint *Ident `json:"savepoint,omitempty"` // TO [SAVEPOINT] <name>
}

func (*RollbackStmt) stmtNode()          {}
func (n *RollbackStmt) Children() []Node { return nodes(n.Name, n.Savepoint) }

// SavepointStmt is SAVEPOINT <name>.
//
//	cmd ::= SAVEPOINT nm.
type SavepointStmt struct {
	Span
	Name *Ident `json:"name"`
}

func (*SavepointStmt) stmtNode()          {}
func (n *SavepointStmt) Children() []Node { return nodes(n.Name) }

// ReleaseStmt is RELEASE [SAVEPOINT] <name>.
//
//	cmd ::= RELEASE savepoint_opt nm.
type ReleaseStmt struct {
	Span
	Name *Ident `json:"name"`
}

func (*ReleaseStmt) stmtNode()          {}
func (n *ReleaseStmt) Children() []Node { return nodes(n.Name) }

// AttachStmt is ATTACH [DATABASE] expr AS expr [KEY expr].
//
//	cmd ::= ATTACH database_kw_opt expr AS expr key_opt.
type AttachStmt struct {
	Span
	HasDatabase bool `json:"hasDatabase,omitempty"`
	File        Expr `json:"file"`
	Name        Expr `json:"name"`
	Key         Expr `json:"key,omitempty"`
}

func (*AttachStmt) stmtNode()          {}
func (n *AttachStmt) Children() []Node { return nodes(n.File, n.Name, n.Key) }

// DetachStmt is DETACH [DATABASE] expr.
//
//	cmd ::= DETACH database_kw_opt expr.
type DetachStmt struct {
	Span
	HasDatabase bool `json:"hasDatabase,omitempty"`
	Name        Expr `json:"name"`
}

func (*DetachStmt) stmtNode()          {}
func (n *DetachStmt) Children() []Node { return nodes(n.Name) }

// PragmaStmt is a PRAGMA in any of its three value spellings.
//
//	cmd ::= PRAGMA nm dbnm.
//	cmd ::= PRAGMA nm dbnm EQ nmnum. / PRAGMA nm dbnm EQ minus_num.
//	cmd ::= PRAGMA nm dbnm LP nmnum RP. / PRAGMA nm dbnm LP minus_num RP.
type PragmaStmt struct {
	Span
	Name     *QualifiedName `json:"name"`
	Value    string         `json:"value,omitempty"` // as written, "-" included
	Paren    bool           `json:"paren,omitempty"` // the LP ... RP spelling
	HasValue bool           `json:"hasValue,omitempty"`
}

func (*PragmaStmt) stmtNode()          {}
func (n *PragmaStmt) Children() []Node { return nodes(n.Name) }

// VacuumStmt is VACUUM [schema] [INTO expr].
//
//	cmd ::= VACUUM vinto. / cmd ::= VACUUM nm vinto.
type VacuumStmt struct {
	Span
	Schema *Ident `json:"schema,omitempty"`
	Into   Expr   `json:"into,omitempty"`
}

func (*VacuumStmt) stmtNode()          {}
func (n *VacuumStmt) Children() []Node { return nodes(n.Schema, n.Into) }

// AnalyzeStmt is ANALYZE [name [. name]].
//
//	cmd ::= ANALYZE. / cmd ::= ANALYZE nm dbnm.
type AnalyzeStmt struct {
	Span
	Name *QualifiedName `json:"name,omitempty"`
}

func (*AnalyzeStmt) stmtNode()          {}
func (n *AnalyzeStmt) Children() []Node { return nodes(n.Name) }

// ReindexStmt is REINDEX [name [. name]].
//
//	cmd ::= REINDEX. / cmd ::= REINDEX nm dbnm.
type ReindexStmt struct {
	Span
	Name *QualifiedName `json:"name,omitempty"`
}

func (*ReindexStmt) stmtNode()          {}
func (n *ReindexStmt) Children() []Node { return nodes(n.Name) }

// ExplainStmt wraps another statement in EXPLAIN or EXPLAIN QUERY PLAN.
//
//	ecmd ::= explain cmdx SEMI.
//	explain ::= EXPLAIN. / explain ::= EXPLAIN QUERY PLAN.
type ExplainStmt struct {
	Span
	QueryPlan bool `json:"queryPlan,omitempty"`
	Stmt      Stmt `json:"stmt"`
}

func (*ExplainStmt) stmtNode()          {}
func (n *ExplainStmt) Children() []Node { return nodes(n.Stmt) }
