package parser_test

import (
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sqlc-dev/meyer/ast"
	"github.com/sqlc-dev/meyer/internal/dump"
	"github.com/sqlc-dev/meyer/internal/testfile"
	"github.com/sqlc-dev/meyer/parser"
)

// TestRoundTrip is the property PLAN.md uses in place of upstream parse-tree
// goldens: rendering a tree back to SQL and re-parsing it must produce the
// same tree. Accept/reject conformance cannot see a dropped clause or a
// mis-associated operator; this can.
//
// Equality is structural. Byte spans and the Raw fields differ by
// construction after a round trip, and the renderer explicitly promises
// nothing about them, so dump.Structure leaves both out.
func TestRoundTrip(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("testdata", "*.test"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no corpus files; run go run ./cmd/regenerate-parse")
	}
	// The count is logged so that a corpus change that silently stops
	// exercising the property is visible in a -v run.
	t.Cleanup(func() { t.Logf("round-tripped %d cases", roundTripped.Load()) })
	for _, path := range paths {
		t.Run(strings.TrimSuffix(filepath.Base(path), ".test"), func(t *testing.T) {
			t.Parallel()
			cases, err := testfile.Read(path)
			if err != nil {
				t.Fatal(err)
			}
			meta, err := testfile.ReadMetadata(testfile.MetadataPath(path))
			if err != nil {
				t.Fatal(err)
			}
			for _, c := range cases {
				if meta.Todo[c.Name] {
					continue
				}
				checkRoundTrip(t, c)
			}
		})
	}
}

var roundTripped atomic.Int64

func checkRoundTrip(t *testing.T, c testfile.Case) {
	t.Helper()
	first, err := parser.ParseString(c.SQL)
	if err != nil {
		return // a must-reject case, or text SQLite never verified
	}
	roundTripped.Add(1)
	rendered := ast.Statements(first)
	second, err := parser.ParseString(rendered)
	if err != nil {
		t.Errorf("%s: rendered SQL does not parse: %v\n  rendered: %s", c.Name, err, rendered)
		return
	}
	want := dump.Stmts(first, dump.Structure)
	got := dump.Stmts(second, dump.Structure)
	if got != want {
		t.Errorf("%s: round trip changed the tree\n  source:   %s\n  rendered: %s\n%s",
			c.Name, strings.TrimSpace(c.SQL), rendered, firstDiff(want, got))
		return
	}
	// Rendering is idempotent, which catches a renderer that loses
	// information the dump happens not to show.
	if again := ast.Statements(second); again != rendered {
		t.Errorf("%s: rendering is not idempotent\n  once:  %s\n  twice: %s",
			c.Name, rendered, again)
	}
}

// firstDiff reports the first differing line of two dumps, with a little
// context, so a failure names the field that changed.
func firstDiff(want, got string) string {
	wl, gl := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := 0; i < len(wl) || i < len(gl); i++ {
		var a, b string
		if i < len(wl) {
			a = wl[i]
		}
		if i < len(gl) {
			b = gl[i]
		}
		if a != b {
			return "  first difference at dump line " + strconv.Itoa(i+1) +
				"\n    want: " + a + "\n    got:  " + b
		}
	}
	return ""
}
