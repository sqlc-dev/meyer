// Package testfile reads and writes meyer's consolidated corpus files.
//
// A corpus file (parser/testdata/<name>.test) holds many cases:
//
//	==== <case name>
//	<SQL, verbatim, one or more lines>
//	----
//	stmt <offset> ok
//	stmt <offset> err <rc> <erroff> <message>
//
// The result lines are the raw per-statement output of the SQLite oracle
// (see cmd/regenerate-parse): every statement in the case was prepared
// independently with sqlite3_prepare_v2 against an empty in-memory database.
// Offsets are byte offsets into the case SQL; erroff is -1 when SQLite did
// not report an error position. The corpus stores this raw truth; the
// pass/fail policy for the parser under test is derived from it by
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
}

// syntaxFamily matches error messages produced by SQLite's parser/tokenizer
// proper, as opposed to post-parse semantic analysis. Statements failing with
// any other message (no such table, ambiguous column, ...) parsed
// successfully and must be accepted by meyer.
var syntaxFamily = []*regexp.Regexp{
	regexp.MustCompile(`^near ".*": syntax error$`),
	regexp.MustCompile(`^unrecognized token: ".*"$`),
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
// statement must parse.
func (c *Case) Expected() Expectation {
	for _, r := range c.Results {
		if !r.OK && IsSyntaxError(r.Message) {
			return Expectation{OK: false, Message: r.Message, Offset: r.ErrOffset}
		}
	}
	return Expectation{OK: true, Offset: -1}
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
	fields := strings.SplitN(strings.TrimPrefix(parts[1], "err "), " ", 3)
	if !strings.HasPrefix(parts[1], "err ") || len(fields) != 3 {
		return StmtResult{}, fmt.Errorf("malformed result line %q", line)
	}
	rc, err1 := strconv.Atoi(fields[0])
	erroff, err2 := strconv.Atoi(fields[1])
	if err1 != nil || err2 != nil {
		return StmtResult{}, fmt.Errorf("malformed result line %q", line)
	}
	return StmtResult{Offset: off, RC: rc, ErrOffset: erroff, Message: fields[2]}, nil
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
				fmt.Fprintf(&b, "stmt %d err %d %d %s\n", r.Offset, r.RC, r.ErrOffset, r.Message)
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
