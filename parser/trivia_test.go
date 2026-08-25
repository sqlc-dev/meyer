package parser_test

import (
	"reflect"
	"testing"

	"github.com/sqlc-dev/meyer/internal/testfile"
	"github.com/sqlc-dev/meyer/lexer"
	"github.com/sqlc-dev/meyer/parser"
	"github.com/sqlc-dev/meyer/token"
)

// TestCorpusTrivia checks the LexFile decomposition over the whole corpus:
// tokens and trivia tile the consumed input exactly — in order, with no
// gap, no overlap, and no byte belonging to neither — and the trivia
// channel holds nothing but whitespace and comment runs.
func TestCorpusTrivia(t *testing.T) {
	forEachCorpusCase(t, func(t *testing.T, c testfile.Case) {
		toks, trivia := lexer.LexFile(c.SQL)
		for _, tr := range trivia {
			if tr.Kind != token.SPACE && tr.Kind != token.COMMENT {
				t.Fatalf("%s: trivia kind %v", c.Name, tr.Kind)
			}
		}
		at := 0
		ti, vi := 0, 0
		body := toks[:len(toks)-1] // drop the zero-width EOF
		for ti < len(body) || vi < len(trivia) {
			var next token.Token
			switch {
			case ti == len(body):
				next, vi = trivia[vi], vi+1
			case vi == len(trivia):
				next, ti = body[ti], ti+1
			case body[ti].Pos < trivia[vi].Pos:
				next, ti = body[ti], ti+1
			default:
				next, vi = trivia[vi], vi+1
			}
			if next.Pos != at {
				t.Fatalf("%s: tiling broken at %d, next span starts at %d", c.Name, at, next.Pos)
			}
			at = next.End
		}
		if eof := toks[len(toks)-1]; at != eof.Pos {
			t.Fatalf("%s: tiling ends at %d, EOF at %d", c.Name, at, eof.Pos)
		}
	})
}

// TestCorpusParseFile checks that ParseFile agrees with ParseString on
// every case: the same statements and the same error, since both run the
// same grammar over the same token stream.
func TestCorpusParseFile(t *testing.T) {
	forEachCorpusCase(t, func(t *testing.T, c testfile.Case) {
		stmts, err := parser.ParseString(c.SQL)
		f, ferr := parser.ParseFile(c.SQL)
		switch {
		case (err == nil) != (ferr == nil):
			t.Fatalf("%s: ParseString err=%v, ParseFile err=%v", c.Name, err, ferr)
		case err != nil:
			if err.Error() != ferr.Error() {
				t.Fatalf("%s: error mismatch:\n%v\n%v", c.Name, err, ferr)
			}
		case !reflect.DeepEqual(stmts, f.Stmts):
			t.Fatalf("%s: ParseFile statements differ from ParseString", c.Name)
		}
	})
}
