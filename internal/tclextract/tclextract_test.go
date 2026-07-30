package tclextract

import "testing"

const sample = `# 2001 September 15
# comment do_execsql_test not-a-test {SELECT 1}
do_execsql_test select1-1.1 {
  CREATE TABLE t1(a int);
  SELECT * FROM t1;
} {}

do_catchsql_test select1-1.2 {SELEC 1} {1 {near "SELEC": syntax error}}

do_execsql_test select1-1.3 {
  SELECT $var FROM t1;
} {}

foreach x {1 2} {
  do_execsql_test select1-2.$x {
    SELECT 1;
  } {1}
}

do_execsql_test select1-1.4 "SELECT 2" {2}

do_execsql_test select1-1.1 {
  SELECT 'duplicate name';
} {}

do_test tokenize-1.1 {
  catchsql {SELECT * FROM sqlite_master WHERE}
} {1 {near "WHERE": syntax error}}

do_test tokenize-1.2 {
  set v [execsql {SELECT 1}]
} {1}
`

func TestFile(t *testing.T) {
	cases, stats := File("select1", []byte(sample))

	var names []string
	for _, c := range cases {
		names = append(names, c.Name)
	}
	want := []string{"select1-1.1", "select1-1.2", "select1-generated", "select1-1.1.dup1", "tokenize-1.1"}
	if len(names) != len(want) {
		t.Fatalf("got cases %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("got cases %v, want %v", names, want)
		}
	}

	if cases[0].SQL != "\n  CREATE TABLE t1(a int);\n  SELECT * FROM t1;\n" {
		t.Fatalf("unexpected SQL for first case: %q", cases[0].SQL)
	}
	if cases[1].SQL != "SELEC 1\n" {
		t.Fatalf("unexpected SQL for catchsql case: %q", cases[1].SQL)
	}

	// select1-1.3 has $var (substitution); the quoted-word case is not a
	// brace literal; tokenize-1.2's body is more than a single execsql call;
	// the foreach body case has a literal SQL block with a dynamic name and
	// is kept under a generated name.
	if stats.SkippedSubst != 1 || stats.SkippedForm != 2 || stats.GeneratedNames != 1 {
		t.Fatalf("stats = %+v", stats)
	}
	if stats.Found != 8 || stats.Extracted != 5 {
		t.Fatalf("stats = %+v", stats)
	}

	if cases[4].SQL != "SELECT * FROM sqlite_master WHERE\n" {
		t.Fatalf("unexpected SQL for do_test case: %q", cases[4].SQL)
	}
}
