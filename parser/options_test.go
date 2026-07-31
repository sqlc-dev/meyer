package parser_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/sqlc-dev/meyer/ast"
	"github.com/sqlc-dev/meyer/internal/roundtrip"
	"github.com/sqlc-dev/meyer/parser"
)

// The expectations in this file are the oracle's, like every other error
// expectation in the package, but they need an oracle the corpus tooling
// does not build: SQLITE_ENABLE_UPDATE_DELETE_LIMIT selects grammar rules,
// so defining it while compiling the released amalgamation changes nothing
// -- the amalgamation ships a parse.c that Lemon already generated without
// them. The build that produced these lines was, from the pinned release's
// two artifacts:
//
//	cc -o lemon sqlite-src-<v>/tool/lemon.c
//	./lemon -DSQLITE_ENABLE_UPDATE_DELETE_LIMIT -S src/parse.y
//
// with the resulting parse.c spliced into sqlite3.c over the region the
// amalgamation marks "Begin file parse.c", and the whole compiled with
// -DSQLITE_ENABLE_UPDATE_DELETE_LIMIT. Lemon assigns the same token numbers
// either way -- the generated parse.h is byte-identical -- so the splice is
// sound. cmd/difftest run against that build, with the option on, agreed
// with meyer on 228,652 mutations of the whole corpus.

// udlAccepted is SQL that a build with SQLITE_ENABLE_UPDATE_DELETE_LIMIT
// parses and the pinned build rejects. Every case reaches SQLite's semantic
// layer there ("no such table: t1"), which is how the oracle reports a
// statement that parsed.
var udlAccepted = []string{
	`DELETE FROM t1 ORDER BY x`,
	`DELETE FROM t1 WHERE x=1 ORDER BY x`,
	`DELETE FROM t1 WHERE x>0 LIMIT 5`,
	`DELETE FROM t1 WHERE x>0 ORDER BY x LIMIT 5 OFFSET 2`,
	`DELETE FROM t1 LIMIT 2, 3`,
	`DELETE FROM t1 INDEXED BY i1 WHERE x=1 LIMIT 1`,
	`DELETE FROM t1 AS a WHERE a.x=1 ORDER BY a.x LIMIT 1`,
	`DELETE FROM t1 ORDER BY x COLLATE nocase DESC NULLS LAST LIMIT 1`,
	`WITH c(x) AS (SELECT 1) DELETE FROM t1 WHERE x IN (SELECT x FROM c) LIMIT 1`,
	`UPDATE t1 SET y=1 LIMIT 5`,
	`UPDATE t1 SET y=1 WHERE x=1 ORDER BY x`,
	`UPDATE OR REPLACE t1 SET y=1 WHERE x=1 ORDER BY x DESC LIMIT 5 OFFSET 2`,
	`UPDATE t1 SET y=1 FROM t2 WHERE t1.x=t2.x LIMIT 1`,
	// where_opt_ret comes before orderby_opt, so RETURNING precedes LIMIT.
	`UPDATE t1 SET y=1 WHERE x=1 RETURNING x, y, '|' LIMIT 5`,
	`UPDATE t1 SET (a,b)=(SELECT 1,2) WHERE x=1 ORDER BY x LIMIT 1`,
}

// udlRejected is SQL both builds reject, with the message and offset the
// UDL build reports. The clauses are only ever the tail of a top-level
// UPDATE or DELETE: OFFSET needs its LIMIT, RETURNING belongs to
// where_opt_ret and so cannot follow one, and trigger_cmd never grew either
// clause, whatever the build.
var udlRejected = []struct {
	sql    string
	msg    string
	offset int
}{
	{`DELETE FROM t1 WHERE x=1 OFFSET 2`, `near "OFFSET": syntax error`, 25},
	{`UPDATE t1 SET y=1 WHERE x=1 OFFSET 2`, `near "OFFSET": syntax error`, 28},
	{`DELETE FROM t1 LIMIT 1, 2 OFFSET 3`, `near "OFFSET": syntax error`, 26},
	{`UPDATE t1 SET y=1 LIMIT 5 RETURNING x`, `near "RETURNING": syntax error`, 26},
	{`DELETE FROM t1 LIMIT 5 RETURNING x`, `near "RETURNING": syntax error`, 23},
	{
		`CREATE TRIGGER r AFTER INSERT ON t1 BEGIN DELETE FROM t1 LIMIT 1; END`,
		`near "LIMIT": syntax error`, 57,
	},
	{
		`CREATE TRIGGER r AFTER INSERT ON t1 BEGIN UPDATE t1 SET y=1 ORDER BY x LIMIT 1; END`,
		`near "ORDER": syntax error`, 60,
	},
}

// udlOff is what the pinned build makes of the same clauses: the statement
// ended at the token before, so ORDER or LIMIT is a token no rule can
// shift. These offsets are the corpus's own, from wherelimit.test.
var udlOff = []struct {
	sql    string
	msg    string
	offset int
}{
	{`DELETE FROM t1 ORDER BY x`, `near "ORDER": syntax error`, 15},
	{`DELETE FROM t1 WHERE x=1 ORDER BY x`, `near "ORDER": syntax error`, 25},
	{`DELETE FROM t1 WHERE x>0 LIMIT 5`, `near "LIMIT": syntax error`, 25},
	{`UPDATE t1 SET y=1 WHERE x=1 ORDER BY x`, `near "ORDER": syntax error`, 28},
	{`UPDATE t1 SET y=1 WHERE x=1 RETURNING x, y, '|' LIMIT 5`, `near "LIMIT": syntax error`, 48},
	{`WITH c(x) AS (SELECT 1) DELETE FROM t1 WHERE x IN (SELECT x FROM c) LIMIT 1`, `near "LIMIT": syntax error`, 68},
}

var udl = parser.Options{UpdateDeleteLimit: true}

// TestUpdateDeleteLimit checks that the option accepts the clauses SQLite
// accepts with SQLITE_ENABLE_UPDATE_DELETE_LIMIT, that the tree survives a
// round trip through the renderer, and that the same SQL is still a syntax
// error without the option -- the corpus is generated from a build without
// it, so the default has to stay where it is.
func TestUpdateDeleteLimit(t *testing.T) {
	for _, sql := range udlAccepted {
		t.Run(sql, func(t *testing.T) {
			stmts, err := udl.ParseString(sql)
			if err != nil {
				t.Fatalf("expected the input to parse with the option, got: %v", err)
			}
			if r := roundtrip.CheckWith(udl, stmts); !r.Ok {
				t.Errorf("round trip: %s\nrendered: %s", r.Reason, r.Rendered)
			}
			if _, err := parser.ParseString(sql); err == nil {
				t.Error("the default options accepted an UPDATE/DELETE LIMIT")
			}
		})
	}
}

func TestUpdateDeleteLimitErrors(t *testing.T) {
	for _, tt := range udlRejected {
		t.Run(tt.sql, func(t *testing.T) {
			checkError(t, udl, tt.sql, tt.msg, tt.offset)
		})
	}
	for _, tt := range udlOff {
		t.Run(tt.sql, func(t *testing.T) {
			checkError(t, parser.Options{}, tt.sql, tt.msg, tt.offset)
		})
	}
}

func checkError(t *testing.T, opts parser.Options, sql, msg string, offset int) {
	t.Helper()
	_, err := opts.ParseString(sql)
	var pe *parser.Error
	if !errors.As(err, &pe) {
		t.Fatalf("expected %q, got %v", msg, err)
	}
	if pe.Message != msg {
		t.Errorf("message:\n  got:  %s\n  want: %s", pe.Message, msg)
	}
	if pe.Offset != offset {
		t.Errorf("offset: got %d, want %d", pe.Offset, offset)
	}
}

// TestUpdateDeleteLimitTree checks that the clauses land on the statement
// rather than being consumed and dropped, which accept/reject cannot see.
func TestUpdateDeleteLimitTree(t *testing.T) {
	stmt, err := udl.ParseStatement(`DELETE FROM t1 WHERE x>0 ORDER BY y DESC LIMIT 5 OFFSET 2`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	del, ok := stmt.(*ast.DeleteStmt)
	if !ok {
		t.Fatalf("parsed to %T, want *ast.DeleteStmt", stmt)
	}
	if len(del.OrderBy) != 1 || del.OrderBy[0].Order != ast.SortDesc {
		t.Errorf("ORDER BY is %+v, want one descending term", del.OrderBy)
	}
	if del.Limit == nil || del.Limit.Count == nil || del.Limit.Offset == nil {
		t.Fatalf("LIMIT is %+v, want a count and an offset", del.Limit)
	}
	if got, want := ast.String(del), `DELETE FROM t1 WHERE x > 0 ORDER BY y DESC LIMIT 5 OFFSET 2`; got != want {
		t.Errorf("rendered\n  got:  %s\n  want: %s", got, want)
	}

	// "LIMIT x, y" swaps the operands, as it does in a SELECT, and the
	// renderer has to put them back the way they were written.
	stmt, err = udl.ParseStatement(`UPDATE t1 SET y=1 LIMIT 2, 3`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	up := stmt.(*ast.UpdateStmt)
	if up.Limit == nil || !up.Limit.Comma {
		t.Fatalf("LIMIT is %+v, want the comma spelling", up.Limit)
	}
	if got, want := ast.String(up), `UPDATE t1 SET y = 1 LIMIT 2, 3`; got != want {
		t.Errorf("rendered\n  got:  %s\n  want: %s", got, want)
	}
}

// TestOptionsEntryPoints checks that the option reaches every entry point,
// and that the package-level ones are unaffected by it.
func TestOptionsEntryPoints(t *testing.T) {
	const sql = `DELETE FROM t1 LIMIT 1`
	if _, err := udl.ParseStatement(sql); err != nil {
		t.Errorf("Options.ParseStatement: %v", err)
	}
	if _, err := udl.Parse(t.Context(), strings.NewReader(sql)); err != nil {
		t.Errorf("Options.Parse: %v", err)
	}
	if _, err := parser.ParseStatement(sql); err == nil {
		t.Error("ParseStatement accepted a DELETE ... LIMIT")
	}
	if _, err := (parser.Options{}).ParseString(sql); err == nil {
		t.Error("the zero Options accepted a DELETE ... LIMIT")
	}
}
