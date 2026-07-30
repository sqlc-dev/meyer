package parser_test

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sqlc-dev/meyer/internal/testfile"
	"github.com/sqlc-dev/meyer/parser"
)

// -check-parse re-runs todo cases; those that now pass are removed from the
// metadata sidecar (logged as "PARSE PASSES NOW") so progress is committed
// alongside the code that produced it. See CLAUDE.md.
var checkParse = flag.Bool("check-parse", false, "re-run todo cases and update metadata for those that pass")

func TestParse(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("testdata", "*.test"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no corpus files; run go run ./cmd/regenerate-parse")
	}
	for _, path := range paths {
		t.Run(strings.TrimSuffix(filepath.Base(path), ".test"), func(t *testing.T) {
			t.Parallel()
			runFile(t, path)
		})
	}
}

func runFile(t *testing.T, path string) {
	cases, err := testfile.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	metaPath := testfile.MetadataPath(path)
	meta, err := testfile.ReadMetadata(metaPath)
	if err != nil {
		t.Fatal(err)
	}
	changed := false
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			todo := meta.Todo[c.Name]
			if todo && !*checkParse {
				t.Skip("todo")
			}
			err := runCase(c)
			if todo {
				if err != nil {
					t.Skipf("still failing: %v", err)
				}
				t.Log("PARSE PASSES NOW")
				delete(meta.Todo, c.Name)
				changed = true
				return
			}
			if err != nil {
				t.Error(err)
			}
		})
	}
	if changed {
		if err := testfile.WriteMetadata(metaPath, meta); err != nil {
			t.Fatal(err)
		}
	}
}

// runCase checks one corpus case against the oracle-derived expectation:
// meyer must accept exactly when SQLite's parser accepted, and must produce
// the identical message on rejection. (Error offsets are recorded in the
// corpus but not asserted yet; they become part of this check once the
// parser tracks positions.)
func runCase(c testfile.Case) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := parser.Parse(ctx, strings.NewReader(c.SQL))
	exp := c.Expected()
	if exp.OK {
		if err != nil {
			// A grammar action can fail part-way through a statement, and
			// SQLite then leaves the rest of it unparsed. The corpus records
			// how far it got; failing inside text SQLite never reached is
			// not a conformance failure, because nothing verified it.
			var pe *parser.Error
			if errors.As(err, &pe) && exp.IsUnreached(pe.Offset) {
				return nil
			}
			return fmt.Errorf("expected successful parse, got error: %v", err)
		}
		return nil
	}
	if err == nil {
		return fmt.Errorf("expected parse error %q, but parse succeeded", exp.Message)
	}
	var pe *parser.Error
	if !errors.As(err, &pe) {
		return fmt.Errorf("expected *parser.Error %q, got %T: %v", exp.Message, err, err)
	}
	if pe.Message != exp.Message {
		return fmt.Errorf("error message mismatch:\n  got:  %s\n  want: %s", pe.Message, exp.Message)
	}
	return nil
}
