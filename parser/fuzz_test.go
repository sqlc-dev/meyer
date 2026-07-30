package parser_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/sqlc-dev/meyer/internal/dump"
	"github.com/sqlc-dev/meyer/internal/roundtrip"
	"github.com/sqlc-dev/meyer/internal/testfile"
	"github.com/sqlc-dev/meyer/parser"
)

// adversarialSeeds are inputs chosen to break a hand-written parser rather
// than to be valid SQL: unterminated everything, nesting past any sane
// limit, and the lexical corners of tokenize.c.
var adversarialSeeds = []string{
	"",
	";",
	";;;;",
	"\x00",
	"\v",
	"SELECT",
	"SELECT 'unterminated",
	`SELECT "unterminated`,
	"SELECT `unterminated",
	"SELECT [unterminated",
	"SELECT x'0102",
	"SELECT x'0g'",
	"SELECT 1_",
	"SELECT 1_x",
	"SELECT 0x",
	"SELECT 1e",
	"SELECT .5e+",
	"SELECT $",
	"SELECT $a(",
	"SELECT #1",
	"SELECT ?99999999999999999999",
	"/* unterminated",
	"-- unterminated",
	"SELECT 1 /*",
	"\xef\xbb\xbfSELECT 1",
	"\xff\xfe\x00\x00",
	"SELECT 1 !",
	"SELECT 1 |",
	"CREATE TABLE t(",
	"CREATE TRIGGER t BEGIN",
	"WITH",
	"INSERT INTO t",
	"SELECT * FROM t WHERE a IN",
	"SELECT a NOT",
	"SELECT CASE END",
	"SELECT sum(a) OVER",
	"SELECT sum(a) FILTER",
	"VALUES",
	"PRAGMA",
	"EXPLAIN",
	"EXPLAIN QUERY",
}

// FuzzParse asserts the invariants that must hold for arbitrary bytes:
// parsing terminates without panicking, a rejection is always a
// *parser.Error, and anything accepted survives the round trip. The last of
// these turns the fuzzer into a search for renderer and tree bugs, not just
// for crashes.
func FuzzParse(f *testing.F) {
	for _, s := range adversarialSeeds {
		f.Add(s)
	}
	for _, s := range corpusSeeds(f) {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		stmts, err := parser.ParseString(src)
		if err != nil {
			var pe *parser.Error
			if !errors.As(err, &pe) {
				t.Fatalf("rejection is not a *parser.Error: %T %v", err, err)
			}
			if pe.Message == "" {
				t.Fatal("rejection with an empty message")
			}
			if pe.Offset < 0 || pe.Offset > len(src) {
				t.Fatalf("error offset %d is outside the input (%d bytes)", pe.Offset, len(src))
			}
			return
		}
		if r := roundtrip.Check(stmts); !r.Ok {
			t.Fatalf("%s\n  input:    %q\n  rendered: %q\n%s",
				r.Reason, src, r.Rendered, dump.FirstDiff(r.Want, r.Got))
		}
	})
}

// corpusSeeds samples the vendored corpus so the fuzzer starts from real
// SQL as well as from broken SQL. Every case would be too many seeds to run
// on each -short invocation, so this takes a deterministic slice.
func corpusSeeds(f *testing.F) []string {
	f.Helper()
	var out []string
	for _, path := range corpusFiles(f) {
		cases, err := testfile.Read(path)
		if err != nil {
			f.Fatal(err)
		}
		for i := 0; i < len(cases); i += 25 {
			out = append(out, cases[i].SQL)
		}
	}
	return out
}

// TestRecursionLimit pins the behaviour that made the limit necessary.
// Running out of goroutine stack is a fatal runtime error that no caller can
// recover from, so deep nesting has to be turned into an ordinary rejection.
func TestRecursionLimit(t *testing.T) {
	const n = 200000
	tests := []struct{ name, sql string }{
		{"parens", "SELECT " + strings.Repeat("(", n) + "1" + strings.Repeat(")", n)},
		{"not", "SELECT " + strings.Repeat("NOT ", n) + "1"},
		{"subquery", strings.Repeat("SELECT (", n) + "1" + strings.Repeat(")", n)},
		{"case", "SELECT " + strings.Repeat("CASE WHEN 1 THEN ", n) + "1" + strings.Repeat(" END", n)},
		{"from", "SELECT * FROM " + strings.Repeat("(", n) + "t" + strings.Repeat(")", n)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parser.ParseString(tt.sql)
			var pe *parser.Error
			if !errors.As(err, &pe) || pe.Message != "Recursion limit" {
				t.Fatalf("got %v, want a Recursion limit rejection", err)
			}
		})
	}
	// A long flat expression is not nesting and must still be accepted:
	// left-associative operators unwind before the next one is consumed.
	if _, err := parser.ParseString("SELECT " + strings.Repeat("1+", n) + "1"); err != nil {
		t.Fatalf("flat expression rejected: %v", err)
	}
}
