// Package dump renders an ast.Node as a stable, human-reviewable tree.
//
// It serves two purposes described in PLAN.md, both standing in for the
// upstream parse-tree goldens SQLite cannot produce:
//
//   - Committed AST snapshots for a curated slice of the corpus, reviewed by
//     humans in diffs but never authoritative over the accept/reject oracle.
//   - The comparison key for the round-trip property, where a tree is
//     rendered back to SQL, re-parsed, and the two trees must match.
//
// The renderer is reflective rather than a type switch, so it never falls
// behind the node set: a field added to a node shows up in the dump, and in
// any snapshot diff, without anyone having to remember.
package dump

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/sqlc-dev/meyer/ast"
)

// Options controls which source-fidelity detail the dump carries.
type Options struct {
	// Positions includes each node's byte span.
	Positions bool
	// Raw includes the Raw fields that hold verbatim source text.
	Raw bool
}

// Snapshot is the option set for committed snapshots: everything.
var Snapshot = Options{Positions: true, Raw: true}

// Structure is the option set for comparing two trees for equality of shape.
// It drops spans and Raw text, which are exactly the things meyer's renderer
// declines to promise: re-parsing rendered SQL moves every offset and may
// respell any token.
var Structure = Options{}

// Node renders one node.
func Node(n ast.Node, opts Options) string {
	var b strings.Builder
	d := dumper{b: &b, opts: opts}
	d.value(reflect.ValueOf(n), 0)
	b.WriteByte('\n')
	return b.String()
}

// Stmts renders a whole script.
func Stmts(stmts []ast.Stmt, opts Options) string {
	var b strings.Builder
	d := dumper{b: &b, opts: opts}
	for _, s := range stmts {
		d.value(reflect.ValueOf(s), 0)
		b.WriteByte('\n')
	}
	return b.String()
}

type dumper struct {
	b    *strings.Builder
	opts Options
}

var spanType = reflect.TypeOf(ast.Span{})

func (d *dumper) indent(depth int) {
	d.b.WriteString(strings.Repeat("  ", depth))
}

func (d *dumper) value(v reflect.Value, depth int) {
	switch v.Kind() {
	case reflect.Interface, reflect.Pointer:
		if v.IsNil() {
			d.b.WriteString("nil")
			return
		}
		if v.Kind() == reflect.Interface {
			d.value(v.Elem(), depth)
			return
		}
		d.strct(v.Elem(), v.Type().Elem().Name(), depth)
	case reflect.Struct:
		d.strct(v, v.Type().Name(), depth)
	case reflect.Slice:
		if v.IsNil() || v.Len() == 0 {
			d.b.WriteString("[]")
			return
		}
		d.b.WriteString("[\n")
		for i := 0; i < v.Len(); i++ {
			d.indent(depth + 1)
			d.value(v.Index(i), depth+1)
			d.b.WriteString("\n")
		}
		d.indent(depth)
		d.b.WriteString("]")
	case reflect.String:
		fmt.Fprintf(d.b, "%q", v.String())
	case reflect.Bool:
		fmt.Fprintf(d.b, "%v", v.Bool())
	default:
		// Enum-typed fields (SortOrder, ConflictAction, ...) all implement
		// Stringer, so a snapshot diff names what changed rather than
		// showing a number that has to be looked up.
		if s, ok := v.Interface().(fmt.Stringer); ok {
			fmt.Fprintf(d.b, "%s(%s)", v.Type().Name(), s)
			return
		}
		fmt.Fprintf(d.b, "%v", v.Interface())
	}
}

func (d *dumper) strct(v reflect.Value, name string, depth int) {
	if v.Type() == spanType {
		s := v.Interface().(ast.Span)
		fmt.Fprintf(d.b, "%d:%d", s.Start, s.Stop)
		return
	}
	fields := d.fields(v)
	if len(fields) == 0 {
		d.b.WriteString(name + "{}")
		return
	}
	d.b.WriteString(name + "{\n")
	for _, f := range fields {
		d.indent(depth + 1)
		d.b.WriteString(f.name + ": ")
		d.value(f.value, depth+1)
		d.b.WriteString("\n")
	}
	d.indent(depth)
	d.b.WriteString("}")
}

type field struct {
	name  string
	value reflect.Value
}

// fields selects the fields worth printing: zero values are omitted so that
// a dump shows what a statement said rather than what it did not.
func (d *dumper) fields(v reflect.Value) []field {
	t := v.Type()
	var out []field
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if !sf.IsExported() {
			continue
		}
		fv := v.Field(i)
		if sf.Type == spanType {
			if !d.opts.Positions {
				continue
			}
			out = append(out, field{"Span", fv})
			continue
		}
		if sf.Name == "Raw" && !d.opts.Raw {
			continue
		}
		if fv.IsZero() {
			continue
		}
		out = append(out, field{sf.Name, fv})
	}
	return out
}
