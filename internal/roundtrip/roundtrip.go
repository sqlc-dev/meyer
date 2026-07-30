// Package roundtrip states meyer's round-trip property in one place.
//
// PLAN.md uses it in place of the parse-tree goldens SQLite cannot produce:
// rendering a tree back to SQL and re-parsing it must yield the same tree.
// Accept/reject conformance cannot see a dropped clause or an operator
// associated the wrong way; this can.
//
// It lives in its own package because three callers need it — the corpus
// test, the fuzz target and cmd/difftest — and when each had its own copy
// they drifted: two of the three had quietly stopped checking that
// rendering is idempotent.
package roundtrip

import (
	"github.com/sqlc-dev/meyer/ast"
	"github.com/sqlc-dev/meyer/internal/dump"
	"github.com/sqlc-dev/meyer/parser"
)

// Result describes one round trip. Ok is false when the property failed,
// and the other fields say how.
type Result struct {
	Ok       bool
	Rendered string // the SQL the tree was rendered back to
	Reason   string // why it failed: one short phrase
	Want     string // the structural dump of the original tree
	Got      string // the structural dump of the re-parsed one
}

// Check renders stmts back to SQL, re-parses it, and compares the two trees.
//
// Equality is structural: byte spans and the Raw fields differ after a round
// trip by construction, and the renderer promises nothing about them, so
// dump.Structure leaves both out.
func Check(stmts []ast.Stmt) Result {
	rendered := ast.Statements(stmts)
	again, err := parser.ParseString(rendered)
	if err != nil {
		return Result{Rendered: rendered, Reason: "rendered SQL does not parse: " + err.Error()}
	}
	want := dump.Stmts(stmts, dump.Structure)
	if got := dump.Stmts(again, dump.Structure); got != want {
		return Result{Rendered: rendered, Reason: "round trip changed the tree", Want: want, Got: got}
	}
	// Rendering twice must give the same text, which catches a renderer
	// that loses something the dump happens not to show.
	if twice := ast.Statements(again); twice != rendered {
		return Result{
			Rendered: rendered, Reason: "rendering is not idempotent",
			Want: rendered, Got: twice,
		}
	}
	return Result{Ok: true, Rendered: rendered}
}

// CheckSQL parses src and, if it parses, checks the property. An input the
// parser rejects is not a round-trip failure, and reports Ok.
func CheckSQL(src string) Result {
	stmts, err := parser.ParseString(src)
	if err != nil {
		return Result{Ok: true}
	}
	return Check(stmts)
}
