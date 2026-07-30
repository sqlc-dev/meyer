// Package lexer implements SQLite's tokenizer.
//
// It is a direct port of sqlite3GetToken() in src/tokenize.c together with
// the token post-processing sqlite3RunParser() performs around it: runs of
// whitespace and comments are dropped, and the WINDOW/OVER/FILTER keywords
// are resolved against their neighbours (analyzeWindowKeyword and friends).
//
// Illegal input is not reported here. sqlite3RunParser aborts at the first
// TK_ILLEGAL token it reaches, which is exactly as far as the parser had
// got, so meyer keeps ILLEGAL tokens in the stream and lets the parser
// report "unrecognized token" when it reaches one. That preserves SQLite's
// ordering between tokenizer errors and syntax errors.
package lexer

import (
	"github.com/sqlc-dev/meyer/token"
)

// Lex splits src into tokens. Whitespace and comments are dropped; the
// returned slice always ends with a single EOF token whose Pos is the end of
// the consumed input.
//
// A NUL byte terminates the input, matching SQLite, whose tokenizer walks a
// NUL-terminated buffer.
func Lex(src string) []token.Token {
	var toks []token.Token
	i := 0
	for i < len(src) {
		kind, n := scan(src, i)
		if kind == token.EOF { // NUL byte: end of input
			break
		}
		if n == 0 {
			n = 1
		}
		if kind != token.SPACE && kind != token.COMMENT {
			toks = append(toks, token.Token{Kind: kind, Text: src[i : i+n], Pos: i, End: i + n})
		}
		i += n
	}
	resolveWindowKeywords(toks)
	toks = append(toks, token.Token{Kind: token.EOF, Pos: i, End: i})
	return toks
}

// resolveWindowKeywords implements analyzeWindowKeyword, analyzeOverKeyword
// and analyzeFilterKeyword from tokenize.c. Those three keywords cannot use
// Lemon's %fallback mechanism, because whether they are keywords depends on
// the tokens around them rather than on the parser state.
func resolveWindowKeywords(toks []token.Token) {
	at := func(i int) token.Kind {
		if i < 0 || i >= len(toks) {
			return token.EOF
		}
		return toks[i].Kind
	}
	// getToken() in tokenize.c folds these kinds into TK_ID before the
	// analysis compares them. Note that INDEXED is deliberately absent.
	asID := func(k token.Kind) token.Kind {
		if k == token.ID || k == token.STRING || k == token.JOIN_KW ||
			k == token.WINDOW || k == token.OVER || token.CanFallback(k) {
			return token.ID
		}
		return k
	}
	prev := token.Kind(-1) // lastTokenParsed; -1 before the first token
	for i := range toks {
		switch toks[i].Kind {
		case token.WINDOW:
			// WINDOW is a keyword only in "WINDOW <name> AS".
			if !(asID(at(i+1)) == token.ID && at(i+2) == token.AS) {
				toks[i].Kind = token.ID
			}
		case token.OVER:
			// OVER is a keyword only after ")" and before "(" or a name.
			if !(prev == token.RP && (at(i+1) == token.LP || asID(at(i+1)) == token.ID)) {
				toks[i].Kind = token.ID
			}
		case token.FILTER:
			// FILTER is a keyword only between ")" and "(".
			if !(prev == token.RP && at(i+1) == token.LP) {
				toks[i].Kind = token.ID
			}
		}
		prev = toks[i].Kind
	}
}

// scan returns the kind and byte length of the token beginning at src[i].
// It mirrors the switch on aiClass[] in sqlite3GetToken(); the character
// classes are spelled out as predicates instead of a lookup table.
func scan(src string, i int) (token.Kind, int) {
	c := src[i]
	at := func(off int) byte { // z[off], with the C string's implicit NUL
		if i+off < len(src) {
			return src[i+off]
		}
		return 0
	}
	switch {
	case c == 0:
		return token.EOF, 0

	case isSpaceStart(c): // CC_SPACE
		n := 1
		for i+n < len(src) && isSpace(src[i+n]) {
			n++
		}
		return token.SPACE, n

	case c == '-': // CC_MINUS
		if at(1) == '-' {
			n := 2
			for i+n < len(src) && src[i+n] != '\n' {
				n++
			}
			return token.COMMENT, n
		}
		if at(1) == '>' {
			if at(2) == '>' {
				return token.PTR, 3
			}
			return token.PTR, 2
		}
		return token.MINUS, 1

	case c == '(':
		return token.LP, 1
	case c == ')':
		return token.RP, 1
	case c == ';':
		return token.SEMI, 1
	case c == '+':
		return token.PLUS, 1
	case c == '*':
		return token.STAR, 1

	case c == '/': // CC_SLASH
		if at(1) != '*' || at(2) == 0 {
			return token.SLASH, 1
		}
		// An unterminated /* comment runs to the end of the input and is
		// not an error; tokenize.test relies on this.
		n := 3
		prev := at(2)
		for ; i+n < len(src); n++ {
			if prev == '*' && src[i+n] == '/' {
				n++
				break
			}
			prev = src[i+n]
		}
		return token.COMMENT, n

	case c == '%':
		return token.REM, 1

	case c == '=': // CC_EQ
		if at(1) == '=' {
			return token.EQ, 2
		}
		return token.EQ, 1

	case c == '<': // CC_LT
		switch at(1) {
		case '=':
			return token.LE, 2
		case '>':
			return token.NE, 2
		case '<':
			return token.LSHIFT, 2
		}
		return token.LT, 1

	case c == '>': // CC_GT
		switch at(1) {
		case '=':
			return token.GE, 2
		case '>':
			return token.RSHIFT, 2
		}
		return token.GT, 1

	case c == '!': // CC_BANG
		if at(1) != '=' {
			return token.ILLEGAL, 1
		}
		return token.NE, 2

	case c == '|': // CC_PIPE
		if at(1) != '|' {
			return token.BITOR, 1
		}
		return token.CONCAT, 2

	case c == ',':
		return token.COMMA, 1
	case c == '&':
		return token.BITAND, 1
	case c == '~':
		return token.BITNOT, 1

	case c == '\'' || c == '"' || c == '`': // CC_QUOTE
		delim := c
		n := 1
		var last byte
		for ; i+n < len(src); n++ {
			last = src[i+n]
			if last == delim {
				if at(n+1) == delim {
					n++
					continue
				}
				break
			}
		}
		if i+n >= len(src) {
			last = 0
		}
		switch {
		case last == '\'':
			return token.STRING, n + 1
		case last != 0:
			return token.ID, n + 1
		default:
			return token.ILLEGAL, n
		}

	case c == '.': // CC_DOT
		if !isDigit(at(1)) {
			return token.DOT, 1
		}
		return scanNumber(src, i, at)

	case isDigit(c): // CC_DIGIT
		return scanNumber(src, i, at)

	case c == '[': // CC_QUOTE2
		n := 1
		for ; i+n < len(src); n++ {
			if src[i+n] == ']' {
				return token.ID, n + 1
			}
		}
		return token.ILLEGAL, n

	case c == '?': // CC_VARNUM
		n := 1
		for i+n < len(src) && isDigit(src[i+n]) {
			n++
		}
		return token.VARIABLE, n

	case c == '$' || c == '@' || c == '#' || c == ':': // CC_DOLLAR, CC_VARALPHA
		return scanVariable(src, i, at)

	case c == 'x' || c == 'X': // CC_X: blob literal, otherwise an identifier
		if at(1) == '\'' {
			n := 2
			for isHex(at(n)) {
				n++
			}
			kind := token.BLOB
			if at(n) != '\'' || n%2 != 0 {
				kind = token.ILLEGAL
				for at(n) != 0 && at(n) != '\'' {
					n++
				}
			}
			if at(n) != 0 {
				n++
			}
			return kind, n
		}
		return scanIdent(src, i, 1)

	case c == 0xef: // CC_BOM
		if at(1) == 0xbb && at(2) == 0xbf {
			return token.SPACE, 3
		}
		return scanIdent(src, i, 1)

	case isKeywordStart(c): // CC_KYWD0
		if !isKeywordChar(at(1)) {
			return scanIdent(src, i, 1)
		}
		n := 2
		for isKeywordChar(at(n)) {
			n++
		}
		if isIDChar(at(n)) {
			return scanIdent(src, i, n+1)
		}
		return token.Lookup(src[i : i+n]), n

	case isIDChar(c): // CC_KYWD ('_' and the digits are handled above), CC_ID
		return scanIdent(src, i, 1)
	}
	return token.ILLEGAL, 1
}

// scanIdent consumes identifier characters starting from an already-accepted
// prefix of n bytes: the "while( IdChar(z[i]) ){ i++; }" tail of
// sqlite3GetToken.
func scanIdent(src string, i, n int) (token.Kind, int) {
	for i+n < len(src) && isIDChar(src[i+n]) {
		n++
	}
	return token.ID, n
}

// scanNumber implements the CC_DIGIT case: hex integers, decimal integers,
// floats, exponents, and '_' digit separators (which turn the token into a
// QNUMBER for the grammar to validate). A number immediately followed by an
// identifier character is TK_ILLEGAL, so "1abc" is one bad token rather than
// two good ones.
func scanNumber(src string, i int, at func(int) byte) (token.Kind, int) {
	kind := token.INTEGER
	n := 0
	if at(0) == '0' && (at(1) == 'x' || at(1) == 'X') && isHex(at(2)) {
		for n = 3; ; n++ {
			if !isHex(at(n)) {
				if at(n) == '_' {
					kind = token.QNUMBER
				} else {
					break
				}
			}
		}
	} else {
		for n = 0; ; n++ {
			if !isDigit(at(n)) {
				if at(n) == '_' {
					kind = token.QNUMBER
				} else {
					break
				}
			}
		}
		if at(n) == '.' {
			if kind == token.INTEGER {
				kind = token.FLOAT
			}
			for n++; ; n++ {
				if !isDigit(at(n)) {
					if at(n) == '_' {
						kind = token.QNUMBER
					} else {
						break
					}
				}
			}
		}
		if (at(n) == 'e' || at(n) == 'E') &&
			(isDigit(at(n+1)) || ((at(n+1) == '+' || at(n+1) == '-') && isDigit(at(n+2)))) {
			if kind == token.INTEGER {
				kind = token.FLOAT
			}
			for n += 2; ; n++ {
				if !isDigit(at(n)) {
					if at(n) == '_' {
						kind = token.QNUMBER
					} else {
						break
					}
				}
			}
		}
	}
	for isIDChar(at(n)) {
		kind = token.ILLEGAL
		n++
	}
	return kind, n
}

// scanVariable implements the CC_DOLLAR/CC_VARALPHA case: the ?, :name,
// @name and $name bind-parameter spellings, including the TCL-style
// "$foo(bar)" and "$foo::bar" suffixes SQLite accepts.
func scanVariable(src string, i int, at func(int) byte) (token.Kind, int) {
	kind := token.VARIABLE
	nid := 0
	n := 1
	for ; at(n) != 0; n++ {
		c := at(n)
		switch {
		case isIDChar(c):
			nid++
		case c == '(' && nid > 0:
			for {
				n++
				c = at(n)
				if c == 0 || isSpace(c) || c == ')' {
					break
				}
			}
			if c == ')' {
				n++
			} else {
				kind = token.ILLEGAL
			}
			if nid == 0 {
				kind = token.ILLEGAL
			}
			return kind, n
		case c == ':' && at(n+1) == ':':
			n++
		default:
			if nid == 0 {
				kind = token.ILLEGAL
			}
			return kind, n
		}
	}
	if nid == 0 {
		kind = token.ILLEGAL
	}
	return kind, n
}

// The character classes of tokenize.c's aiClass[] and sqlite3CtypeMap[].

// isSpace is sqlite3Isspace(), which governs how far a run of whitespace
// extends once one has started.
func isSpace(c byte) bool {
	return c == ' ' || (c >= '\t' && c <= '\r')
}

// isSpaceStart is CC_SPACE, the narrower set that may *begin* a whitespace
// token. Vertical tab is missing from it, so a leading \v is an illegal
// token even though \v is absorbed by a run that started with a space.
func isSpaceStart(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\f' || c == '\r'
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isHex(c byte) bool {
	return isDigit(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func isAlpha(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// isIDChar is IdChar() from tokenize.c: alphanumerics, '_', '$', and every
// byte with the high bit set.
func isIDChar(c byte) bool {
	return isAlpha(c) || isDigit(c) || c == '_' || c == '$' || c >= 0x80
}

// isKeywordStart is CC_KYWD0: a letter other than 'x'/'X', which begin blob
// literals and are handled separately.
func isKeywordStart(c byte) bool {
	return isAlpha(c) && c != 'x' && c != 'X'
}

// isKeywordChar is CC_KYWD0 or CC_KYWD: characters that may appear inside a
// keyword. 'x' and 'X' qualify here even though they cannot start one.
func isKeywordChar(c byte) bool { return isAlpha(c) || c == '_' }
