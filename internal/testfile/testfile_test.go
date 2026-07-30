package testfile

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	cases := []Case{
		{
			Name: "a-1.1",
			SQL:  "CREATE TABLE t1(a int);\nSELECT * FROM t1;\n",
			Results: []StmtResult{
				{Offset: 0, OK: true},
				{Offset: 24, RC: 1, ErrOffset: -1, Tail: 42, Message: "no such table: t1"},
			},
		},
		{
			Name: "a-1.2",
			SQL:  "SELEC 1;\n",
			Results: []StmtResult{
				{Offset: 0, RC: 1, ErrOffset: 0, Tail: 5, Message: `near "SELEC": syntax error`},
			},
		},
	}
	path := filepath.Join(t.TempDir(), "a.test")
	if err := Write(path, cases); err != nil {
		t.Fatal(err)
	}
	got, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, cases) {
		t.Fatalf("round trip mismatch:\ngot:  %#v\nwant: %#v", got, cases)
	}
}

func TestExpected(t *testing.T) {
	// The SQL is 62 bytes long, so that a Tail short of that marks a
	// statement the parser abandoned part-way through.
	const sql = "CREATE TRIGGER r1 AFTER INSERT ON t1 BEGIN SELECT * FROM; END;"
	tests := []struct {
		name          string
		results       []StmtResult
		wantOK        bool
		wantMsg       string
		wantUnreached []Range
	}{
		{"all ok", []StmtResult{{OK: true}, {OK: true}}, true, "", nil},
		{
			"semantic only",
			[]StmtResult{{RC: 1, ErrOffset: -1, Tail: len(sql), Message: "no such table: t1"}},
			true, "", nil,
		},
		{
			"syntax after semantic",
			[]StmtResult{
				{Offset: 0, RC: 1, ErrOffset: -1, Tail: 20, Message: "no such table: t1"},
				{Offset: 20, RC: 1, ErrOffset: 21, Tail: 21, Message: `near "WHERE": syntax error`},
			},
			false, `near "WHERE": syntax error`, nil,
		},
		{
			"unrecognized token",
			[]StmtResult{{RC: 1, ErrOffset: 3, Tail: 3, Message: `unrecognized token: "0x"`}},
			false, `unrecognized token: "0x"`, nil,
		},
		{
			"incomplete",
			[]StmtResult{{RC: 1, ErrOffset: -1, Tail: len(sql), Message: "incomplete input"}},
			false, "incomplete input", nil,
		},
		{
			// sqlite3BeginTrigger fails at the trigger_decl reduce, so
			// everything after BEGIN is never parsed.
			"semantic failure part-way through a statement",
			[]StmtResult{{Offset: 0, RC: 1, ErrOffset: -1, Tail: 42, Message: "no such table: main.t1"}},
			true, "", []Range{{Start: 42, End: len(sql) + 1}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Case{Name: "x", SQL: sql, Results: tt.results}
			exp := c.Expected()
			if exp.OK != tt.wantOK || exp.Message != tt.wantMsg {
				t.Fatalf("Expected() = %+v, want ok=%v msg=%q", exp, tt.wantOK, tt.wantMsg)
			}
			if !reflect.DeepEqual(exp.Unreached, tt.wantUnreached) {
				t.Fatalf("Unreached = %+v, want %+v", exp.Unreached, tt.wantUnreached)
			}
			for _, r := range tt.wantUnreached {
				if !exp.IsUnreached(r.Start) || !exp.IsUnreached(r.End-1) || exp.IsUnreached(r.End) {
					t.Fatalf("IsUnreached disagrees with %+v", r)
				}
			}
			if exp.IsUnreached(-1) {
				t.Fatal("IsUnreached(-1) should be false")
			}
		})
	}
}

func TestWriteRejectsMarkerCollision(t *testing.T) {
	err := Write(filepath.Join(t.TempDir(), "x.test"), []Case{{
		Name: "bad",
		SQL:  "SELECT 1;\n----\nSELECT 2;\n",
		Results: []StmtResult{
			{OK: true},
		},
	}})
	if err == nil {
		t.Fatal("expected marker-collision error, got nil")
	}
}

func TestMetadataMissingFile(t *testing.T) {
	m, err := ReadMetadata(filepath.Join(t.TempDir(), "nope.metadata.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Todo) != 0 {
		t.Fatalf("expected empty metadata, got %+v", m)
	}
}
