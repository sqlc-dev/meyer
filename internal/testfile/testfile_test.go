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
				{Offset: 24, RC: 1, ErrOffset: -1, Message: "no such table: t1"},
			},
		},
		{
			Name: "a-1.2",
			SQL:  "SELEC 1;\n",
			Results: []StmtResult{
				{Offset: 0, RC: 1, ErrOffset: 0, Message: `near "SELEC": syntax error`},
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
	tests := []struct {
		name    string
		results []StmtResult
		wantOK  bool
		wantMsg string
	}{
		{"all ok", []StmtResult{{OK: true}, {OK: true}}, true, ""},
		{"semantic only", []StmtResult{{RC: 1, ErrOffset: -1, Message: "no such table: t1"}}, true, ""},
		{
			"syntax after semantic",
			[]StmtResult{
				{Offset: 0, RC: 1, ErrOffset: -1, Message: "no such table: t1"},
				{Offset: 20, RC: 1, ErrOffset: 21, Message: `near "WHERE": syntax error`},
			},
			false, `near "WHERE": syntax error`,
		},
		{"unrecognized token", []StmtResult{{RC: 1, ErrOffset: 3, Message: `unrecognized token: "0x"`}}, false, `unrecognized token: "0x"`},
		{"incomplete", []StmtResult{{RC: 1, ErrOffset: -1, Message: "incomplete input"}}, false, "incomplete input"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Case{Name: "x", SQL: "irrelevant\n", Results: tt.results}
			exp := c.Expected()
			if exp.OK != tt.wantOK || exp.Message != tt.wantMsg {
				t.Fatalf("Expected() = %+v, want ok=%v msg=%q", exp, tt.wantOK, tt.wantMsg)
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
