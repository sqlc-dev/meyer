package parser_test

import (
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sqlc-dev/meyer/internal/dump"
	"github.com/sqlc-dev/meyer/internal/roundtrip"
	"github.com/sqlc-dev/meyer/internal/testfile"
	"github.com/sqlc-dev/meyer/parser"
)

// TestRoundTrip is the property PLAN.md uses in place of upstream parse-tree
// goldens: rendering a tree back to SQL and re-parsing it must produce the
// same tree. Accept/reject conformance cannot see a dropped clause or a
// mis-associated operator; this can. The property itself lives in
// internal/roundtrip, which the fuzz target and cmd/difftest share.
func TestRoundTrip(t *testing.T) {
	var checked atomic.Int64
	// The count is logged so that a corpus change that silently stops
	// exercising the property is visible in a -v run.
	t.Cleanup(func() { t.Logf("round-tripped %d cases", checked.Load()) })
	forEachCorpusCase(t, func(t *testing.T, c testfile.Case) {
		stmts, err := parser.ParseString(c.SQL)
		if err != nil {
			return // a must-reject case, or text SQLite never verified
		}
		checked.Add(1)
		if r := roundtrip.Check(stmts); !r.Ok {
			t.Errorf("%s: %s\n  source:   %s\n  rendered: %s\n%s",
				c.Name, r.Reason, strings.TrimSpace(c.SQL), r.Rendered,
				dump.FirstDiff(r.Want, r.Got))
		}
	})
}
