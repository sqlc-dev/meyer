// Package ast declares the syntax tree for the SQLite SQL dialect.
//
// This is the milestone-1 skeleton: the interfaces exist so the parser API
// and test harness are stable, and node types are added as the parser is
// implemented (see PLAN.md for the full intended node set).
package ast

// Node is implemented by every syntax tree node. Positions are byte offsets
// into the original input.
type Node interface {
	Pos() int
	End() int
	Children() []Node
}

// Stmt is implemented by all statement nodes.
type Stmt interface {
	Node
	stmtNode()
}

// Expr is implemented by all expression nodes.
type Expr interface {
	Node
	exprNode()
}

// Span carries a node's byte extent and is embedded in every node type.
type Span struct {
	Start int `json:"start"`
	Stop  int `json:"end"`
}

func (s Span) Pos() int { return s.Start }
func (s Span) End() int { return s.Stop }
