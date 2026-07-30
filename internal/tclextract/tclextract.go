// Package tclextract pulls literal SQL test cases out of SQLite's TCL test
// scripts (test/*.test in the SQLite source tree).
//
// It recognizes the two dominant test commands:
//
//	do_execsql_test  <name> {SQL} ?{expected}?
//	do_catchsql_test <name> {SQL} {expected}
//
// plus the older style used by e.g. tokenize.test, but only when the body is
// exactly one execsql/catchsql call:
//
//	do_test <name> {catchsql {SQL}} {expected}
//	do_test <name> {execsql {SQL}} {expected}
//
// Only the name and the SQL block are used; expected values are re-derived
// from a real SQLite build by cmd/regenerate-parse, so TCL expectations are
// ignored. Extraction is deliberately conservative: any case whose SQL block
// is not a plain TCL brace literal — it contains `$` (variable substitution),
// `[` (command substitution), or `\` (backslash escapes change brace
// counting) — is skipped and counted, never guessed at.
package tclextract

import (
	"fmt"
	"strings"
)

// Case is one extracted test case.
type Case struct {
	Name string // TCL test name, or a generated one if the name was dynamic
	SQL  string // literal SQL, outer braces removed, normalized to end in "\n"
}

// Stats reports what extraction skipped, so coverage loss is visible.
type Stats struct {
	Found          int // command occurrences seen
	Extracted      int
	SkippedSubst   int // SQL contained $, [ or \
	SkippedForm    int // SQL argument was not a brace literal / malformed command
	GeneratedNames int // dynamic test names replaced with file-local ones
}

var commands = []string{"do_execsql_test", "do_catchsql_test", "do_test"}

// File extracts all cases from one TCL test script. base names the script
// (e.g. "select1") and seeds generated case names.
func File(base string, src []byte) ([]Case, Stats) {
	var cases []Case
	var stats Stats
	seen := make(map[string]int)
	text := string(src)
	lines := strings.Split(text, "\n")

	// Byte offset of each line start, so command scanning can be line-based
	// (to reject comment lines) while word parsing is offset-based.
	offset := 0
	for _, line := range lines {
		lineStart := offset
		offset += len(line) + 1
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		for _, cmd := range commands {
			col := strings.Index(line, cmd)
			if col < 0 {
				continue
			}
			// Require the command at the start of the (possibly indented)
			// line so occurrences inside strings or longer words are skipped.
			if strings.TrimLeft(line[:col], " \t") != "" {
				continue
			}
			if rest := line[col+len(cmd):]; rest != "" && rest[0] != ' ' && rest[0] != '\t' {
				continue // longer word, e.g. do_execsql_test_if_vdbe
			}
			stats.Found++
			name, sql, ok := parseCommand(text, lineStart+col+len(cmd), cmd == "do_test", &stats)
			if !ok {
				break
			}
			if strings.ContainsAny(name, "$[]\\\"") || name == "" {
				stats.GeneratedNames++
				name = fmt.Sprintf("%s-generated", base)
			}
			if n := seen[name]; n > 0 {
				seen[name] = n + 1
				name = fmt.Sprintf("%s.dup%d", name, n)
			} else {
				seen[name] = 1
			}
			stats.Extracted++
			cases = append(cases, Case{Name: name, SQL: normalizeSQL(sql)})
			break
		}
	}
	return cases, stats
}

// parseCommand reads the two argument words following a command occurrence
// that starts at text[pos]. For do_test (doTest=true), the second word is a
// TCL body that must consist of exactly one execsql/catchsql call. It
// returns ok=false (and bumps a skip counter) when the case cannot be
// extracted verbatim.
func parseCommand(text string, pos int, doTest bool, stats *Stats) (name, sql string, ok bool) {
	name, pos, _, ok = word(text, pos)
	if !ok {
		stats.SkippedForm++
		return "", "", false
	}
	var braced bool
	sql, pos, braced, ok = word(text, pos)
	if !ok || !braced {
		stats.SkippedForm++ // malformed, or SQL argument was not a brace literal
		return "", "", false
	}
	if doTest {
		sql, ok = sqlFromBody(sql)
		if !ok {
			stats.SkippedForm++
			return "", "", false
		}
	}
	if strings.ContainsAny(sql, "$[\\") {
		stats.SkippedSubst++
		return "", "", false
	}
	return name, sql, true
}

// sqlFromBody unwraps a do_test body of the exact shape
// `catchsql {SQL}` or `execsql {SQL}` (surrounding whitespace allowed,
// nothing else in the body).
func sqlFromBody(body string) (string, bool) {
	rest := strings.TrimLeft(body, " \t\n\r")
	var cmd string
	for _, c := range []string{"catchsql", "execsql"} {
		if strings.HasPrefix(rest, c) {
			cmd = c
			break
		}
	}
	if cmd == "" {
		return "", false
	}
	rest = rest[len(cmd):]
	i := 0
	for i < len(rest) && (rest[i] == ' ' || rest[i] == '\t') {
		i++
	}
	if i == 0 || i >= len(rest) || rest[i] != '{' {
		return "", false // longer word (e.g. execsql_timed), or no brace literal
	}
	sql, next, braced, ok := word(rest, i)
	if !ok || !braced {
		return "", false
	}
	if strings.TrimSpace(rest[next:]) != "" {
		return "", false // body contains more than the single call
	}
	return sql, true
}

// word consumes one TCL word starting at or after pos. It understands
// whitespace, backslash-newline continuation, brace literals with nesting,
// and quoted/bare words. braced reports that the word was a {...} literal.
// ok=false on newline/end-of-command or malformed input.
func word(text string, pos int) (val string, next int, braced, ok bool) {
	// Skip horizontal whitespace and backslash-newline continuations.
	for pos < len(text) {
		switch {
		case text[pos] == ' ' || text[pos] == '\t':
			pos++
		case text[pos] == '\\' && pos+1 < len(text) && text[pos+1] == '\n':
			pos += 2
		default:
			goto scan
		}
	}
scan:
	if pos >= len(text) || text[pos] == '\n' || text[pos] == ';' {
		return "", pos, false, false
	}
	switch text[pos] {
	case '{':
		depth := 0
		for i := pos; i < len(text); i++ {
			switch text[i] {
			case '\\':
				i++ // backslash escapes next byte for brace counting
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					return text[pos+1 : i], i + 1, true, true
				}
			}
		}
		return "", pos, false, false // unbalanced
	case '"':
		for i := pos + 1; i < len(text); i++ {
			if text[i] == '\\' {
				i++
				continue
			}
			if text[i] == '"' {
				return text[pos+1 : i], i + 1, false, true
			}
		}
		return "", pos, false, false
	default:
		i := pos
		for i < len(text) && !strings.ContainsRune(" \t\n;", rune(text[i])) {
			i++
		}
		return text[pos:i], i, false, true
	}
}

// normalizeSQL trims trailing whitespace and guarantees a single trailing
// newline, leaving everything else (leading indentation included) verbatim
// so byte offsets into the SQL stay meaningful.
func normalizeSQL(sql string) string {
	return strings.TrimRight(sql, " \t\n\r") + "\n"
}
