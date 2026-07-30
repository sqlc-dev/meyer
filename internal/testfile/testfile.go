// Package testfile reads and writes meyer's consolidated corpus files.
//
// A corpus file (parser/testdata/<name>.test) holds many cases:
//
//	==== <case name>
//	<SQL, verbatim, one or more lines>
//	----
//	stmt <offset> ok
//	stmt <offset> err <rc> <erroff> <tail> <message>
//
// The result lines are the raw per-statement output of the SQLite oracle
// (see cmd/regenerate-parse): every statement in the case was prepared
// independently with sqlite3_prepare_v2 against an empty in-memory database.
// Offsets are byte offsets into the case SQL; erroff is -1 when SQLite did
// not report an error position, and tail is how far sqlite3_prepare_v2
// reported having got (a successful prepare always reaches the end of its
// statement, so ok lines carry no tail). The corpus stores this raw truth;
// the pass/fail policy for the parser under test is derived from it by
// (*Case).Expected, so the policy can evolve without regenerating.
//
// Each corpus file has a sidecar <name>.metadata.json:
//
//	{"todo": {"case name": true, ...}}
//
// listing cases the parser does not handle yet. The test harness skips todo
// cases by default and, when run with -check-parse, removes entries that
// have started passing.
package testfile

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// StmtResult is one raw oracle observation for a single statement.
type StmtResult struct {
	Offset    int    // byte offset of the statement within the case SQL
	OK        bool   // sqlite3_prepare_v2 returned SQLITE_OK
	RC        int    // prepare result code when !OK
	ErrOffset int    // sqlite3_error_offset within the case SQL; -1 if unknown
	Tail      int    // pzTail within the case SQL: how far the parser got
	Message   string // sqlite3_errmsg when !OK
}

// Case is a single corpus entry.
type Case struct {
	Name    string
	SQL     string // always ends with exactly one "\n"
	Results []StmtResult
}

// Expectation is the derived requirement for the parser under test.
type Expectation struct {
	OK      bool
	Message string // expected parse error message when !OK
	Offset  int    // expected error offset when !OK; -1 if unknown

	// Unreached lists byte ranges of the case SQL that SQLite's parser
	// never looked at, because a grammar action failed part-way through a
	// statement and sqlite3RunParser abandoned the rest of it. The corpus
	// says nothing about whether that text is valid SQL, so a parser under
	// test may fail inside one of these ranges without being wrong. A range
	// that reaches the end of the input includes the offset one past it,
	// which is where a parser reports running out of input.
	Unreached []Range
}

// Range is a half-open byte range of a case's SQL.
type Range struct{ Start, End int }

// IsUnreached reports whether offset falls in text SQLite's parser never
// reached. An offset of -1 (unknown) is never unreached.
func (e Expectation) IsUnreached(offset int) bool {
	if offset < 0 {
		return false
	}
	for _, r := range e.Unreached {
		if offset >= r.Start && offset < r.End {
			return true
		}
	}
	return false
}

// syntaxFamily matches error messages produced by SQLite's parser/tokenizer
// proper, as opposed to post-parse semantic analysis. Statements failing with
// any other message (no such table, ambiguous column, ...) parsed
// successfully and must be accepted by meyer.
// The (?s) flag matters: the offending token is interpolated into the
// message, and a token can contain a newline -- an unterminated string runs
// to the end of the input, newline and all.
var syntaxFamily = []*regexp.Regexp{
	regexp.MustCompile(`(?s)^near ".*": syntax error$`),
	regexp.MustCompile(`(?s)^unrecognized token: ".*"$`),
	regexp.MustCompile(`^incomplete input$`),
}

// IsSyntaxError reports whether msg is in the syntax-error family.
func IsSyntaxError(msg string) bool {
	for _, re := range syntaxFamily {
		if re.MatchString(msg) {
			return true
		}
	}
	return false
}

// Expected derives the harness expectation: the first syntax-family error in
// statement order wins (meyer is fail-fast); if there is none, every
// statement must parse, except within the ranges SQLite's own parser never
// reached.
func (c *Case) Expected() Expectation {
	var unreached []Range
	for i, r := range c.Results {
		if r.OK {
			continue
		}
		if IsSyntaxError(r.Message) {
			// Ranges collected from earlier statements still apply: a
			// parser that trips over unverified text never reaches this.
			return Expectation{
				OK: false, Message: r.Message, Offset: r.ErrOffset,
				Unreached: unreached,
			}
		}
		// A semantic failure means the statement parsed -- but only as far
		// as the parser had got when the grammar action raised it. A
		// statement runs to the start of the next one, or to the end of the
		// case for the last.
		end := len(c.SQL)
		if i+1 < len(c.Results) {
			end = c.Results[i+1].Offset
		}
		if r.Tail >= end {
			continue
		}
		if end == len(c.SQL) {
			// Running out of input is reported at the offset one past the
			// last byte, so the final range has to include it.
			end++
		}
		unreached = append(unreached, Range{Start: r.Tail, End: end})
	}
	return Expectation{OK: true, Offset: -1, Unreached: unreached}
}

const (
	caseMarker   = "==== "
	resultMarker = "----"
)

// Read parses a corpus file.
func Read(path string) ([]Case, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cases []Case
	lines := strings.SplitAfter(string(data), "\n")
	i := 0
	for i < len(lines) {
		line := lines[i]
		if strings.TrimRight(line, "\n") == "" {
			i++
			continue
		}
		if !strings.HasPrefix(line, caseMarker) {
			return nil, fmt.Errorf("%s:%d: expected %q header, got %q", path, i+1, caseMarker, line)
		}
		c := Case{Name: strings.TrimRight(line[len(caseMarker):], "\n")}
		i++
		var sql strings.Builder
		for i < len(lines) && strings.TrimRight(lines[i], "\n") != resultMarker {
			sql.WriteString(lines[i])
			i++
		}
		if i == len(lines) {
			return nil, fmt.Errorf("%s: case %q: missing %q separator", path, c.Name, resultMarker)
		}
		c.SQL = sql.String()
		i++ // skip "----"
		for i < len(lines) && strings.HasPrefix(lines[i], "stmt ") {
			r, err := parseResultLine(strings.TrimRight(lines[i], "\n"))
			if err != nil {
				return nil, fmt.Errorf("%s:%d: %w", path, i+1, err)
			}
			c.Results = append(c.Results, r)
			i++
		}
		cases = append(cases, c)
	}
	return cases, nil
}

func parseResultLine(line string) (StmtResult, error) {
	rest := strings.TrimPrefix(line, "stmt ")
	parts := strings.SplitN(rest, " ", 2)
	if len(parts) != 2 {
		return StmtResult{}, fmt.Errorf("malformed result line %q", line)
	}
	off, err := strconv.Atoi(parts[0])
	if err != nil {
		return StmtResult{}, fmt.Errorf("malformed offset in %q", line)
	}
	if parts[1] == "ok" {
		return StmtResult{Offset: off, OK: true}, nil
	}
	fields := strings.SplitN(strings.TrimPrefix(parts[1], "err "), " ", 4)
	if !strings.HasPrefix(parts[1], "err ") || len(fields) != 4 {
		return StmtResult{}, fmt.Errorf("malformed result line %q", line)
	}
	rc, err1 := strconv.Atoi(fields[0])
	erroff, err2 := strconv.Atoi(fields[1])
	tail, err3 := strconv.Atoi(fields[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return StmtResult{}, fmt.Errorf("malformed result line %q", line)
	}
	return StmtResult{
		Offset: off, RC: rc, ErrOffset: erroff, Tail: tail, Message: fields[3],
	}, nil
}

// CheckCase reports whether a case can be represented in the file format
// (SQL must not collide with the markers, messages must be single-line).
func CheckCase(c Case) error {
	return checkWritable(c)
}

// Write renders cases to path. It refuses SQL that would be ambiguous with
// the file format's markers.
func Write(path string, cases []Case) error {
	var b strings.Builder
	for _, c := range cases {
		if err := checkWritable(c); err != nil {
			return err
		}
		b.WriteString(caseMarker)
		b.WriteString(c.Name)
		b.WriteString("\n")
		b.WriteString(c.SQL)
		b.WriteString(resultMarker)
		b.WriteString("\n")
		for _, r := range c.Results {
			if r.OK {
				fmt.Fprintf(&b, "stmt %d ok\n", r.Offset)
			} else {
				fmt.Fprintf(&b, "stmt %d err %d %d %d %s\n",
					r.Offset, r.RC, r.ErrOffset, r.Tail, r.Message)
			}
		}
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func checkWritable(c Case) error {
	if c.Name == "" || strings.ContainsAny(c.Name, "\n") {
		return fmt.Errorf("invalid case name %q", c.Name)
	}
	if !strings.HasSuffix(c.SQL, "\n") {
		return fmt.Errorf("case %q: SQL must end with a newline", c.Name)
	}
	for _, line := range strings.Split(c.SQL, "\n") {
		if strings.HasPrefix(line, caseMarker) || strings.TrimRight(line, " \t") == resultMarker ||
			strings.HasPrefix(line, "==== ") {
			return fmt.Errorf("case %q: SQL line collides with file format marker: %q", c.Name, line)
		}
	}
	for _, r := range c.Results {
		if strings.Contains(r.Message, "\n") {
			return fmt.Errorf("case %q: multi-line oracle message", c.Name)
		}
	}
	return nil
}

// Metadata is the per-file sidecar tracking not-yet-implemented cases.
type Metadata struct {
	Todo map[string]bool `json:"todo,omitempty"`
}

// MetadataPath returns the sidecar path for a corpus file path.
func MetadataPath(testPath string) string {
	return strings.TrimSuffix(testPath, ".test") + ".metadata.json"
}

// ReadMetadata loads a sidecar; a missing file yields empty metadata.
func ReadMetadata(path string) (Metadata, error) {
	var m Metadata
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return m, nil
	}
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return m, fmt.Errorf("%s: %w", path, err)
	}
	return m, nil
}

// WriteMetadata saves a sidecar with stable formatting.
func WriteMetadata(path string, m Metadata) error {
	if len(m.Todo) == 0 {
		m.Todo = nil
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
