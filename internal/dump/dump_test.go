package dump

import (
	"strings"
	"testing"

	"github.com/sqlc-dev/meyer/ast"
)

// sample is a small tree with one of everything the renderer has to decide
// about: a nil optional field, an empty slice, a bool, a string needing
// quoting, an enum whose zero value is meaningful, and one whose zero value
// means "absent".
func sample() ast.Node {
	return &ast.OrderingTerm{
		Span: ast.Span{Start: 7, Stop: 20},
		Expr: &ast.Literal{
			Span: ast.Span{Start: 7, Stop: 8},
			Kind: ast.LitNull, // zero value, but means NULL
			Raw:  `"x"`,
		},
		Order: ast.SortDesc,
		Nulls: ast.NullsDefault, // zero value meaning absent
	}
}

func TestSnapshotIncludesPositionsAndRaw(t *testing.T) {
	got := Node(sample(), Snapshot)
	for _, want := range []string{"Span: 7:20", "Span: 7:8", `Raw: "\"x\""`} {
		if !strings.Contains(got, want) {
			t.Errorf("Snapshot dump is missing %s:\n%s", want, got)
		}
	}
}

func TestStructureDropsPositionsAndRaw(t *testing.T) {
	got := Node(sample(), Structure)
	for _, unwanted := range []string{"Span", "Raw"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("Structure dump should not mention %s:\n%s", unwanted, got)
		}
	}
	// Everything that is not source fidelity must survive.
	for _, want := range []string{"OrderingTerm", "Literal", "SortOrder(DESC)"} {
		if !strings.Contains(got, want) {
			t.Errorf("Structure dump is missing %s:\n%s", want, got)
		}
	}
}

// An enum whose zero value names a real thing must be printed; one whose
// zero value means "absent" must not, or every dump would be mostly
// defaults.
func TestZeroValuedEnums(t *testing.T) {
	got := Node(sample(), Structure)
	if !strings.Contains(got, "LiteralKind(NULL)") {
		t.Errorf("a NULL literal must show its kind:\n%s", got)
	}
	if strings.Contains(got, "NullsOrder") {
		t.Errorf("an absent NULLS clause must not be printed:\n%s", got)
	}
}

// A typed nil in an optional field is common, and reflection makes it easy
// to dereference by accident.
func TestNilFields(t *testing.T) {
	got := Node(&ast.QualifiedName{Name: &ast.Ident{Name: "t"}}, Structure)
	if strings.Contains(got, "Schema") {
		t.Errorf("a nil Schema must be omitted:\n%s", got)
	}
	if !strings.Contains(got, `Name: "t"`) {
		t.Errorf("dump is missing the name:\n%s", got)
	}
}

func TestStmtsRendersEachStatement(t *testing.T) {
	stmts := []ast.Stmt{
		&ast.SavepointStmt{Name: &ast.Ident{Name: "a"}},
		&ast.ReleaseStmt{Name: &ast.Ident{Name: "b"}},
	}
	got := Stmts(stmts, Structure)
	if !strings.Contains(got, "SavepointStmt") || !strings.Contains(got, "ReleaseStmt") {
		t.Errorf("Stmts dropped a statement:\n%s", got)
	}
}
