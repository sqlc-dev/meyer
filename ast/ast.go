// Package ast declares the syntax tree for the SQLite SQL dialect.
//
// Node names follow SQLite rather than PostgreSQL or sqlc: the tree is a
// direct rendering of src/parse.y, and every node carries a comment naming
// the grammar rule it comes from. Mapping onto another representation is a
// consumer's job.
//
// Every node embeds Span and so reports byte offsets into the original
// input; sqlc slices the source with those offsets, so they are load-bearing
// rather than diagnostic (see PLAN.md).
package ast

import "reflect"

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

// SetSpan replaces the node's extent. The parser uses it to widen a
// statement's span once its terminating semicolon has been consumed.
func (s *Span) SetSpan(sp Span) { *s = sp }

// Walk calls fn for n and, unless fn returns false, for its descendants in
// source order.
func Walk(n Node, fn func(Node) bool) {
	if isNil(n) || !fn(n) {
		return
	}
	for _, c := range n.Children() {
		Walk(c, fn)
	}
}

// isNil reports whether n is either a nil interface or a typed nil pointer.
// Children() slices are assembled from optional fields, so typed nils are
// common and must not leak into a traversal.
func isNil(n Node) bool {
	if n == nil {
		return true
	}
	v := reflect.ValueOf(n)
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Slice, reflect.Map:
		return v.IsNil()
	}
	return false
}
