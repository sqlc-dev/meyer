package ast

import "testing"

// tree is "SELECT a FROM t" built by hand, so the traversal tests do not
// depend on the parser.
func tree() *SelectStmt {
	col := &Ident{Span: Span{Start: 7, Stop: 8}, Name: "a", Raw: "a"}
	tbl := &Ident{Span: Span{Start: 14, Stop: 15}, Name: "t", Raw: "t"}
	return &SelectStmt{
		Span: Span{Start: 0, Stop: 15},
		Cores: []SelectCore{&SelectQuery{
			Span:    Span{Start: 0, Stop: 15},
			Columns: []*ResultColumn{{Span: col.Span, Expr: col}},
			From: []*TableRef{{
				Span: tbl.Span,
				Name: &QualifiedName{Span: tbl.Span, Name: tbl},
			}},
		}},
	}
}

func TestWalkVisitsEveryNode(t *testing.T) {
	var seen []string
	Walk(tree(), func(n Node) bool {
		switch n.(type) {
		case *SelectStmt:
			seen = append(seen, "select")
		case *Ident:
			seen = append(seen, "ident")
		}
		return true
	})
	if len(seen) != 3 || seen[0] != "select" {
		t.Fatalf("Walk visited %v, want a select and two idents", seen)
	}
}

// Children() slices are assembled from optional fields, so they are full of
// typed nils; a traversal must not trip over one.
func TestWalkSkipsTypedNils(t *testing.T) {
	n := &QualifiedName{Name: &Ident{Name: "t"}} // Schema is a nil *Ident
	count := 0
	Walk(n, func(Node) bool { count++; return true })
	if count != 2 {
		t.Fatalf("Walk visited %d nodes, want 2 (the name and its ident)", count)
	}
}

func TestWalkStops(t *testing.T) {
	count := 0
	Walk(tree(), func(Node) bool { count++; return false })
	if count != 1 {
		t.Fatalf("Walk visited %d nodes after fn returned false, want 1", count)
	}
}

func TestSetSpan(t *testing.T) {
	n := tree()
	n.SetSpan(Span{Start: 0, Stop: 16})
	if n.Pos() != 0 || n.End() != 16 {
		t.Fatalf("span is %d:%d, want 0:16", n.Pos(), n.End())
	}
}

func TestString(t *testing.T) {
	if got, want := String(tree()), "SELECT a FROM t"; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
	if got, want := Statements([]Stmt{tree()}), "SELECT a FROM t;\n"; got != want {
		t.Fatalf("Statements() = %q, want %q", got, want)
	}
}
