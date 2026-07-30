package parser_test

import (
	"path/filepath"
	"testing"

	"github.com/sqlc-dev/meyer/ast"
	"github.com/sqlc-dev/meyer/internal/testfile"
	"github.com/sqlc-dev/meyer/lexer"
	"github.com/sqlc-dev/meyer/parser"
)

// The benchmark inputs are the shapes sqlc actually sends through a parser:
// a short lookup, a join with predicates, a schema definition, and a query
// big enough that per-token costs dominate.
var benchInputs = []struct{ name, sql string }{
	{"Simple", `SELECT id, name FROM users WHERE id = ?`},
	{"Join", `
SELECT u.id, u.name, count(o.id) AS orders
  FROM users u
  LEFT JOIN orders o ON o.user_id = u.id AND o.created_at > :since
 WHERE u.active AND u.name LIKE ?1 ESCAPE '\'
 GROUP BY u.id, u.name
HAVING count(o.id) > 0
 ORDER BY orders DESC NULLS LAST
 LIMIT 10 OFFSET 20`},
	{"CreateTable", `
CREATE TABLE orders (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  total       NUMERIC(10, 2) NOT NULL DEFAULT 0 CHECK (total >= 0),
  status      TEXT NOT NULL COLLATE nocase DEFAULT 'new',
  created_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  summary     TEXT GENERATED ALWAYS AS (status || ' ' || total) STORED,
  UNIQUE (user_id, created_at) ON CONFLICT REPLACE
) STRICT`},
	{"Window", `
WITH RECURSIVE months(m) AS (
  SELECT 1 UNION ALL SELECT m + 1 FROM months WHERE m < 12
)
SELECT m,
       sum(total) OVER w AS running,
       row_number() OVER (PARTITION BY status ORDER BY created_at) AS n
  FROM orders JOIN months ON months.m = cast(strftime('%m', created_at) AS INTEGER)
WINDOW w AS (ORDER BY created_at ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW)
 ORDER BY m`},
}

func BenchmarkLex(b *testing.B) {
	for _, in := range benchInputs {
		b.Run(in.name, func(b *testing.B) {
			b.SetBytes(int64(len(in.sql)))
			for b.Loop() {
				lexer.Lex(in.sql)
			}
		})
	}
}

func BenchmarkParse(b *testing.B) {
	for _, in := range benchInputs {
		b.Run(in.name, func(b *testing.B) {
			b.SetBytes(int64(len(in.sql)))
			for b.Loop() {
				if _, err := parser.ParseString(in.sql); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkRender(b *testing.B) {
	for _, in := range benchInputs {
		stmts, err := parser.ParseString(in.sql)
		if err != nil {
			b.Fatal(err)
		}
		b.Run(in.name, func(b *testing.B) {
			b.SetBytes(int64(len(in.sql)))
			for b.Loop() {
				ast.Statements(stmts)
			}
		})
	}
}

// BenchmarkCorpus parses every accepting case of one corpus file, which is
// the closest thing here to a realistic mixed workload.
func BenchmarkCorpus(b *testing.B) {
	cases, err := testfile.Read(filepath.Join("testdata", "select1.test"))
	if err != nil {
		b.Skipf("%v", err)
	}
	var scripts []string
	var n int
	for _, c := range cases {
		if _, err := parser.ParseString(c.SQL); err != nil {
			continue
		}
		scripts = append(scripts, c.SQL)
		n += len(c.SQL)
	}
	b.SetBytes(int64(n))
	for b.Loop() {
		for _, src := range scripts {
			if _, err := parser.ParseString(src); err != nil {
				b.Fatal(err)
			}
		}
	}
}
