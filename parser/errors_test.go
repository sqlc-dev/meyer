package parser_test

import (
	"errors"
	"testing"

	"github.com/sqlc-dev/meyer/parser"
)

// errorCases pins meyer's rejection behaviour against SQLite's. Every
// expectation here was taken from the pinned oracle, not written by hand;
// most of the entries are regressions cmd/difftest turned up.
var errorCases = []struct {
	sql    string
	msg    string // empty means the input must be accepted
	offset int    // -1 when SQLite reports no position
}{
	// indexed_opt shifts NOT before it can know INDEXED follows.
	{`SELECT * FROM t1 NOT NOT INDEXED`, `near "NOT": syntax error`, 21},
	{`DELETE FROM t NOT NOT INDEXED`, `near "NOT": syntax error`, 18},
	{`SELECT * FROM t NOT`, `incomplete input`, -1},
	// the BETWEEN lower bound absorbs OR but stops at a top-level AND.
	{`SELECT 1 BETWEEN 2 OR 3`, `incomplete input`, -1},
	{`SELECT 1 BETWEEN 2 OR 3 AND 4`, `incomplete input`, -1},
	{`SELECT 1 BETWEEN 2 AND 3 AND 4`, "", -1},
	{`SELECT 1 BETWEEN 2 IS 3 AND 4`, "", -1},
	// a comma between table constraints must be followed by one.
	{`CREATE TABLE t(a,PRIMARY KEY(a),)`, `near ")": syntax error`, 32},
	{`CREATE TABLE t(a,PRIMARY KEY(a) UNIQUE(a))`, "", -1},
	// table_option_set recurses on an empty prefix, so it may start with a comma.
	{`CREATE TABLE t(a),STRICT`, "", -1},
	{`CREATE TABLE t(a),`, `incomplete input`, -1},
	{`CREATE TABLE t(a) STRICT,`, `incomplete input`, -1},
	{`CREATE TABLE t(a),,STRICT`, `near ",": syntax error`, 18},
	// wqas shifts NOT before it can know MATERIALIZED follows.
	{`WITH t(a) AS NOT (SELECT 1) SELECT 1`, `near "(": syntax error`, 17},
	// a register reference errors from a grammar action, which does not stop
	// the parser until it has finished with the token it was handed: a syntax
	// error at that same token wins, one at the next never happens.
	{`SELECT #1`, `near "#1": syntax error`, -1},
	{`SELECT #1 #1`, `near "#1": syntax error`, 10},
	{`SELECT #1 FROM t`, `near "#1": syntax error`, 7},
	{`SELECT 1+#1 #2`, `near "#2": syntax error`, 12},
	{`ALTER TABLE t1 ADD CONSTRAINT c1 CHECK( x>#2 )ON ;`, `near "#2": syntax error`, 42},
	// NOT begins a defer_subclause wherever one is allowed, so it is shifted
	// before the parser can know DEFERRABLE does not follow.
	{`CREATE TABLE c(a NOT LEFT)`, `near "LEFT": syntax error`, 21},
	{`CREATE TABLE c(a REFERENCES p NOT LEFT)`, `near "LEFT": syntax error`, 34},
	{`CREATE TABLE c(a, FOREIGN KEY(a) REFERENCES p NOT LEFT)`, `near "LEFT": syntax error`, 50},
	// DEFAULT takes a bare term, so the dot is not a qualified reference.
	{"CREATE TABLE t(a, b DEFAULT 'xyzzy'. c)", `near ".": syntax error`, 35},
	// adjacent literals are an alias only in a result column.
	{`SELECT f('a' 'b')`, `near "'b'": syntax error`, 13},
	{`SELECT 'a' 'b'`, "", -1},
	// OFFSET falls back to an identifier only where it has no shift action.
	{`SELECT 1 LIMIT 2 offset`, `incomplete input`, -1},
	{`SELECT 1 offset`, "", -1},
	// RAISE, CAST and CTIME_KW are keywords in expression position.
	{`SELECT raise`, `incomplete input`, -1},
	{`SELECT cast`, `incomplete input`, -1},
	{`SELECT current_date(1)`, `near "(": syntax error`, 19},
	// VALUES takes no ORDER BY: oneselect hangs it off the SELECT form only.
	{`VALUES(1) ORDER BY 1`, `near "ORDER": syntax error`, 10},
	// the pinned build has no UPDATE/DELETE ORDER BY or LIMIT.
	{`UPDATE t SET a=1 LIMIT 1`, `near "LIMIT": syntax error`, 17},
	{`DELETE FROM t ORDER BY x`, `near "ORDER": syntax error`, 14},
	{`INSERT INTO t (SELECT 1)`, `near "SELECT": syntax error`, 15},
	// columnlist is not optional.
	{`CREATE TABLE t(PRIMARY KEY(a))`, `near "PRIMARY": syntax error`, 15},
	{`CREATE TABLE t()`, `near ")": syntax error`, 15},
	{`SELECT 1 NOT 2`, `near "2": syntax error`, 13},
	{`SELECT 1_x`, `unrecognized token: "1_x"`, 7},
	{`SELECT 1_`, `unrecognized token: "1_"`, -1},
	{`SELECT`, `incomplete input`, -1},
	{`SELECT 'unterminated`, `unrecognized token: "'unterminated"`, 7},
	{`SELECT a FROM`, `incomplete input`, -1},
	{`SELECT (1;)`, `near ";": syntax error`, 9},
	{`SELECT 1 IS NOT DISTINCT FROM 2`, "", -1},
	{`SELECT a COLLATE`, `incomplete input`, -1},
}

// TestErrors checks that meyer reproduces SQLite's rejections exactly: the
// same verdict, the same message, and the same byte offset wherever
// sqlite3_error_offset reported one.
//
// The corpus covers this thinly -- fewer than three hundred of its 20,971
// cases are rejections -- so this table carries the corners that matter, and
// cmd/difftest hunts for more against a live SQLite build.
func TestErrors(t *testing.T) {
	for _, tt := range errorCases {
		t.Run(tt.sql, func(t *testing.T) {
			_, err := parser.ParseString(tt.sql)
			if tt.msg == "" {
				if err != nil {
					t.Fatalf("expected the input to parse, got: %v", err)
				}
				return
			}
			var pe *parser.Error
			if !errors.As(err, &pe) {
				t.Fatalf("expected %q, got %v", tt.msg, err)
			}
			if pe.Message != tt.msg {
				t.Errorf("message:\n  got:  %s\n  want: %s", pe.Message, tt.msg)
			}
			if tt.offset >= 0 && pe.Offset != tt.offset {
				t.Errorf("offset: got %d, want %d", pe.Offset, tt.offset)
			}
		})
	}
}
