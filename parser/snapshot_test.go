package parser_test

import (
	"flag"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/sqlc-dev/meyer/internal/dump"
	"github.com/sqlc-dev/meyer/parser"
)

// -update rewrites the committed AST snapshots. They are meyer's own
// goldens, not an oracle: they exist so that a change in tree shape shows up
// in a diff a human reads, and they never outrank the accept/reject corpus.
var update = flag.Bool("update", false, "rewrite the AST snapshots in testdata/ast")

// TestASTSnapshots parses each testdata/ast/*.sql script and compares the
// rendered tree against the committed .tree file next to it.
//
// Unlike testdata/*.test, these inputs are hand-written and are meant to be
// edited: they are a curated tour of the node set, chosen so that every
// field of every node appears at least once.
func TestASTSnapshots(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("testdata", "ast", "*.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no snapshot inputs under testdata/ast")
	}
	for _, path := range paths {
		t.Run(strings.TrimSuffix(filepath.Base(path), ".sql"), func(t *testing.T) {
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			stmts, err := parser.ParseString(string(src))
			if err != nil {
				t.Fatalf("%s: %v", path, err)
			}
			got := dump.Stmts(stmts, dump.Snapshot)
			golden := strings.TrimSuffix(path, ".sql") + ".tree"
			if *update {
				if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("%v (run: go test ./parser -update)", err)
			}
			if got != string(want) {
				t.Errorf("%s: snapshot mismatch (run: go test ./parser -update)\n%s",
					golden, snapshotDiff(string(want), got))
			}
		})
	}
}

// snapshotDiff reports the first few differing lines, which is enough to see
// what moved without printing a whole tree.
func snapshotDiff(want, got string) string {
	wl, gl := strings.Split(want, "\n"), strings.Split(got, "\n")
	var b strings.Builder
	shown := 0
	for i := 0; i < len(wl) || i < len(gl) || shown < 5; i++ {
		if i >= len(wl) && i >= len(gl) {
			break
		}
		var a, c string
		if i < len(wl) {
			a = wl[i]
		}
		if i < len(gl) {
			c = gl[i]
		}
		if a == c {
			continue
		}
		b.WriteString("  line " + strconv.Itoa(i+1) + "\n    want: " + a + "\n    got:  " + c + "\n")
		shown++
		if shown == 5 {
			b.WriteString("  ...\n")
			break
		}
	}
	return b.String()
}
