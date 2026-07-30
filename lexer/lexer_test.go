// Tests for the tokenizer. The corpus can only see whether a statement is
// accepted, so the lexical detail SQLite's tokenize.c is full of — which
// byte ends a token, which quirk makes something illegal — has to be pinned
// here. The cases are transcribed from tokenize.c and from SQLite's
// test/tokenize.test.
package lexer

import (
	"testing"

	"github.com/sqlc-dev/meyer/token"
)

// kinds lexes src and returns the token kinds, EOF excluded.
func kinds(src string) []token.Kind {
	toks := Lex(src)
	out := make([]token.Kind, 0, len(toks))
	for _, t := range toks[:len(toks)-1] {
		out = append(out, t.Kind)
	}
	return out
}

// texts lexes src and returns the token texts, EOF excluded.
func texts(src string) []string {
	toks := Lex(src)
	out := make([]string, 0, len(toks))
	for _, t := range toks[:len(toks)-1] {
		out = append(out, t.Text)
	}
	return out
}

func TestOperators(t *testing.T) {
	tests := []struct {
		src  string
		want []token.Kind
	}{
		{"+-*/%", []token.Kind{token.PLUS, token.MINUS, token.STAR, token.SLASH, token.REM}},
		{"= ==", []token.Kind{token.EQ, token.EQ}},
		{"!= <>", []token.Kind{token.NE, token.NE}},
		{"< <= << > >= >>", []token.Kind{
			token.LT, token.LE, token.LSHIFT, token.GT, token.GE, token.RSHIFT,
		}},
		{"& | ||", []token.Kind{token.BITAND, token.BITOR, token.CONCAT}},
		{"~ , . ; ( )", []token.Kind{
			token.BITNOT, token.COMMA, token.DOT, token.SEMI, token.LP, token.RP,
		}},
		{"-> ->>", []token.Kind{token.PTR, token.PTR}},
		// A lone "!" is an error token; "|" on its own is bitwise or.
		{"!", []token.Kind{token.ILLEGAL}},
		{"!<", []token.Kind{token.ILLEGAL, token.LT}},
		// "--" starts a comment, so "a--b" is one identifier.
		{"a--b", []token.Kind{token.ID}},
		{"a - -b", []token.Kind{token.ID, token.MINUS, token.MINUS, token.ID}},
	}
	for _, tt := range tests {
		if got := kinds(tt.src); !equalKinds(got, tt.want) {
			t.Errorf("Lex(%q) = %v, want %v", tt.src, got, tt.want)
		}
	}
}

func TestQuoting(t *testing.T) {
	tests := []struct {
		src  string
		kind token.Kind
		text string
	}{
		{`'abc'`, token.STRING, `'abc'`},
		{`''`, token.STRING, `''`},
		{`''''`, token.STRING, `''''`}, // one escaped quote
		{`'a''b'`, token.STRING, `'a''b'`},
		{`"abc"`, token.ID, `"abc"`},
		{`"a""b"`, token.ID, `"a""b"`},
		{"`abc`", token.ID, "`abc`"},
		{"`a``b`", token.ID, "`a``b`"},
		{`[abc]`, token.ID, `[abc]`},
		{`[a"b]`, token.ID, `[a"b]`}, // no escaping inside [...]
		// Unterminated quoting runs to the end of the input and is illegal.
		{`'abc`, token.ILLEGAL, `'abc`},
		{`"abc`, token.ILLEGAL, `"abc`},
		{"`abc", token.ILLEGAL, "`abc"},
		{`[abc`, token.ILLEGAL, `[abc`},
	}
	for _, tt := range tests {
		toks := Lex(tt.src)
		if len(toks) != 2 || toks[0].Kind != tt.kind || toks[0].Text != tt.text {
			t.Errorf("Lex(%q) = %v, want one %v %q", tt.src, toks[:len(toks)-1], tt.kind, tt.text)
		}
	}
}

func TestNumbers(t *testing.T) {
	tests := []struct {
		src  string
		kind token.Kind
	}{
		{"0", token.INTEGER},
		{"123", token.INTEGER},
		{"0xff", token.INTEGER},
		{"0XFF", token.INTEGER},
		{"1.", token.FLOAT},
		{".5", token.FLOAT},
		{"1.5", token.FLOAT},
		{"1e6", token.FLOAT},
		{"1E+6", token.FLOAT},
		{"1e-6", token.FLOAT},
		{"1_000", token.QNUMBER},
		{"0x1_f", token.QNUMBER},
		{"1.0_1", token.QNUMBER},
		{"1e1_0", token.QNUMBER},
		// A trailing separator still lexes as a QNUMBER; it is the grammar
		// action, sqlite3DequoteNumber, that rejects it.
		{"1_", token.QNUMBER},
		// A number running into identifier characters is one illegal token,
		// not a number followed by a name.
		{"1abc", token.ILLEGAL},
		{"0xg", token.ILLEGAL}, // "0" then "xg", which IdChar swallows
		{"1e", token.ILLEGAL},  // no exponent digits, so "e" swallows it
	}
	for _, tt := range tests {
		got := kinds(tt.src)
		if len(got) != 1 || got[0] != tt.kind {
			t.Errorf("Lex(%q) = %v, want [%v]", tt.src, got, tt.kind)
		}
	}
	// "0x" with no hex digits is not a hex literal: the "0" is an integer
	// that then runs into "x", which makes the whole thing illegal.
	if got := kinds("0x"); len(got) != 1 || got[0] != token.ILLEGAL {
		t.Errorf(`Lex("0x") = %v, want [ILLEGAL]`, got)
	}
	// A float followed by a dot is two tokens: "1.5" then ".".
	if got := texts("1.5.5"); len(got) != 2 || got[0] != "1.5" || got[1] != ".5" {
		t.Errorf(`Lex("1.5.5") texts = %q, want ["1.5" ".5"]`, got)
	}
}

func TestBlobLiterals(t *testing.T) {
	tests := []struct {
		src  string
		kind token.Kind
	}{
		{"x'01ab'", token.BLOB},
		{"X'01AB'", token.BLOB},
		{"x''", token.BLOB},
		{"x'0'", token.ILLEGAL},  // odd number of digits
		{"x'0g'", token.ILLEGAL}, // not hex
		{"x'01", token.ILLEGAL},  // unterminated
		{"xyz", token.ID},        // x only starts a blob before a quote
	}
	for _, tt := range tests {
		got := kinds(tt.src)
		if len(got) != 1 || got[0] != tt.kind {
			t.Errorf("Lex(%q) = %v, want [%v]", tt.src, got, tt.kind)
		}
	}
}

func TestVariables(t *testing.T) {
	tests := []struct {
		src  string
		kind token.Kind
		text string
	}{
		{"?", token.VARIABLE, "?"},
		{"?12", token.VARIABLE, "?12"},
		{":name", token.VARIABLE, ":name"},
		{"@name", token.VARIABLE, "@name"},
		{"$name", token.VARIABLE, "$name"},
		{"#1", token.VARIABLE, "#1"},
		// SQLite's tokenizer accepts TCL-style suffixes on $ variables.
		{"$foo::bar", token.VARIABLE, "$foo::bar"},
		{"$foo(baz)", token.VARIABLE, "$foo(baz)"},
		{"$foo::bar(baz)", token.VARIABLE, "$foo::bar(baz)"},
		// A sigil with no name is illegal, and an unclosed "(" suffix too.
		{":", token.ILLEGAL, ":"},
		{"@", token.ILLEGAL, "@"},
		{"$", token.ILLEGAL, "$"},
		{"$foo(baz", token.ILLEGAL, "$foo(baz"},
	}
	for _, tt := range tests {
		toks := Lex(tt.src)
		if len(toks) != 2 || toks[0].Kind != tt.kind || toks[0].Text != tt.text {
			t.Errorf("Lex(%q) = %v, want one %v %q", tt.src, toks[:len(toks)-1], tt.kind, tt.text)
		}
	}
	// "?" takes only digits, so "?a" is a bare "?" followed by a name.
	if got := texts("?a"); len(got) != 2 || got[0] != "?" || got[1] != "a" {
		t.Errorf(`Lex("?a") texts = %q, want ["?" "a"]`, got)
	}
}

func TestWhitespaceAndComments(t *testing.T) {
	tests := []struct {
		src  string
		want []string
	}{
		{"a  \t\n\r\f b", []string{"a", "b"}},
		{"a -- comment\nb", []string{"a", "b"}},
		{"a -- comment", []string{"a"}},
		{"a /* c */ b", []string{"a", "b"}},
		// An unterminated block comment is trailing whitespace, not an
		// error; tokenize.test relies on this.
		{"a /* unterminated", []string{"a"}},
		{"a /*/ b", []string{"a"}},
		// "/*" at the very end of input is a slash then a star, because
		// tokenize.c requires a third byte before it commits to a comment.
		{"a /*", []string{"a", "/", "*"}},
		// A UTF-8 BOM is whitespace, but only as a complete sequence.
		{"\xef\xbb\xbfSELECT", []string{"SELECT"}},
	}
	for _, tt := range tests {
		if got := texts(tt.src); !equalStrings(got, tt.want) {
			t.Errorf("Lex(%q) texts = %q, want %q", tt.src, got, tt.want)
		}
	}
	// Vertical tab may continue a run of whitespace but may not start one:
	// it is CC_ILLEGAL in aiClass[] yet passes sqlite3Isspace.
	if got := kinds("\va"); len(got) != 2 || got[0] != token.ILLEGAL {
		t.Errorf(`Lex("\va") = %v, want [ILLEGAL ID]`, got)
	}
	if got := kinds(" \va"); len(got) != 1 || got[0] != token.ID {
		t.Errorf(`Lex(" \va") = %v, want [ID]`, got)
	}
}

func TestKeywordsAreCaseInsensitive(t *testing.T) {
	for _, src := range []string{"select", "SELECT", "SeLeCt"} {
		if got := kinds(src); len(got) != 1 || got[0] != token.SELECT {
			t.Errorf("Lex(%q) = %v, want [SELECT]", src, got)
		}
	}
	// A keyword that runs into an identifier character is an identifier.
	for _, src := range []string{"select1", "select$", "selectx"} {
		if got := kinds(src); len(got) != 1 || got[0] != token.ID {
			t.Errorf("Lex(%q) = %v, want [ID]", src, got)
		}
	}
}

// TestWindowKeywordResolution covers analyzeWindowKeyword,
// analyzeOverKeyword and analyzeFilterKeyword, the three keywords SQLite
// cannot resolve with %fallback because the answer depends on the
// neighbouring tokens rather than on the parser state.
func TestWindowKeywordResolution(t *testing.T) {
	tests := []struct {
		src  string
		at   int // index of the token under test
		want token.Kind
	}{
		// WINDOW is a keyword only in "WINDOW <name> AS".
		{"SELECT 1 WINDOW w AS ()", 2, token.WINDOW},
		{"SELECT 1 window", 2, token.ID},
		{"SELECT 1 WINDOW w", 2, token.ID},
		{"SELECT 1 WINDOW w FROM t", 2, token.ID},
		{"SELECT window FROM t", 1, token.ID},
		// OVER is a keyword only after ")" and before "(" or a name.
		{"SELECT f() OVER (ORDER BY a)", 4, token.OVER},
		{"SELECT f() OVER w", 4, token.OVER},
		{"SELECT a OVER", 2, token.ID},
		{"SELECT f() OVER", 4, token.ID},
		{"SELECT f() over FROM t", 4, token.ID},
		// FILTER is a keyword only between ")" and "(".
		{"SELECT f() FILTER (WHERE a)", 4, token.FILTER},
		{"SELECT f() filter", 4, token.ID},
		{"SELECT filter (1)", 1, token.ID},
	}
	for _, tt := range tests {
		toks := Lex(tt.src)
		if tt.at >= len(toks) {
			t.Fatalf("Lex(%q) produced only %d tokens", tt.src, len(toks))
		}
		if toks[tt.at].Kind != tt.want {
			t.Errorf("Lex(%q)[%d] = %v %q, want %v",
				tt.src, tt.at, toks[tt.at].Kind, toks[tt.at].Text, tt.want)
		}
	}
}

func TestSpansCoverTheInput(t *testing.T) {
	const src = "SELECT  a /* c */ , 'x' -- done\nFROM t"
	toks := Lex(src)
	for i, tk := range toks {
		if tk.Kind == token.EOF {
			continue
		}
		if got := src[tk.Pos:tk.End]; got != tk.Text {
			t.Errorf("token %d: span %d:%d is %q, but Text is %q", i, tk.Pos, tk.End, got, tk.Text)
		}
		if i > 0 && tk.Pos < toks[i-1].End {
			t.Errorf("token %d starts at %d, before the previous token ended at %d",
				i, tk.Pos, toks[i-1].End)
		}
	}
	last := toks[len(toks)-1]
	if last.Kind != token.EOF || last.Pos != len(src) {
		t.Errorf("last token = %v at %d, want EOF at %d", last.Kind, last.Pos, len(src))
	}
}

// A NUL byte ends the input, because SQLite's tokenizer walks a
// NUL-terminated buffer.
func TestNulTerminatesInput(t *testing.T) {
	toks := Lex("SELECT\x00 1")
	if len(toks) != 2 || toks[0].Kind != token.SELECT || toks[1].Kind != token.EOF {
		t.Errorf("Lex with an embedded NUL = %v, want [SELECT EOF]", toks)
	}
	if toks[1].Pos != 6 {
		t.Errorf("EOF at %d, want 6", toks[1].Pos)
	}
}

func TestLexNeverLoops(t *testing.T) {
	// Every byte value must produce progress, on its own and after a name.
	for b := 0; b < 256; b++ {
		for _, src := range []string{string(rune(b)), "a" + string(rune(b)), string(rune(b)) + "a"} {
			toks := Lex(src)
			if len(toks) == 0 || toks[len(toks)-1].Kind != token.EOF {
				t.Fatalf("Lex(%q) did not terminate cleanly: %v", src, toks)
			}
		}
	}
}

func equalKinds(a, b []token.Kind) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
