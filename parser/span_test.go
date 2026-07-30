package parser_test

import (
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sqlc-dev/meyer/ast"
	"github.com/sqlc-dev/meyer/internal/testfile"
	"github.com/sqlc-dev/meyer/parser"
)

// TestSpans checks the invariants sqlc relies on when it slices the source
// with a node's byte offsets. Nothing else verifies them: a span can be
// wrong in a tree that is otherwise perfectly shaped, and both the corpus
// and the round-trip property would still pass.
//
// The invariants are:
//
//   - a span is a valid range within the input;
//   - a child lies within its parent;
//   - statements are disjoint and in source order, so the text between two
//     of them (where the "-- name:" comments live) is unambiguous.
func TestSpans(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("testdata", "*.test"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no corpus files; run go run ./cmd/regenerate-parse")
	}
	var checked atomic.Int64
	t.Cleanup(func() { t.Logf("checked spans of %d cases", checked.Load()) })
	for _, path := range paths {
		t.Run(strings.TrimSuffix(filepath.Base(path), ".test"), func(t *testing.T) {
			t.Parallel()
			cases, err := testfile.Read(path)
			if err != nil {
				t.Fatal(err)
			}
			for _, c := range cases {
				stmts, err := parser.ParseString(c.SQL)
				if err != nil {
					continue
				}
				checked.Add(1)
				checkStmtSpans(t, c, stmts)
			}
		})
	}
}

func checkStmtSpans(t *testing.T, c testfile.Case, stmts []ast.Stmt) {
	t.Helper()
	prevEnd := 0
	for _, s := range stmts {
		if s.Pos() < prevEnd {
			t.Errorf("%s: statement at %d starts before the previous one ended at %d\n  %q",
				c.Name, s.Pos(), prevEnd, c.SQL)
			return
		}
		prevEnd = s.End()
		if !checkNodeSpans(t, c, s, 0, len(c.SQL)) {
			return
		}
	}
}

// checkNodeSpans walks a subtree, returning false once something is wrong so
// that one bad node does not produce a page of consequences.
func checkNodeSpans(t *testing.T, c testfile.Case, n ast.Node, lo, hi int) bool {
	t.Helper()
	if n.Pos() > n.End() || n.Pos() < lo || n.End() > hi {
		t.Errorf("%s: %T span %d:%d is not within %d:%d\n  %q",
			c.Name, n, n.Pos(), n.End(), lo, hi, c.SQL)
		return false
	}
	for _, child := range n.Children() {
		if !checkNodeSpans(t, c, child, n.Pos(), n.End()) {
			return false
		}
	}
	return true
}
