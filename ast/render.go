package ast

import "strings"

// This file is meyer's SQL renderer. It is not a formatter and makes no
// fidelity promises beyond re-parseability: rendering a tree and parsing the
// result must yield the same tree, ignoring byte spans and the Raw fields
// that quote the original source. PLAN.md uses that round trip as a property
// test, because accept/reject conformance alone cannot see a dropped clause
// or a mis-associated operator.
//
// Parenthesisation needs no precedence logic, because the parser keeps
// explicit parentheses as ParenExpr nodes: a tree that needed brackets to be
// written down came from brackets in the first place.

// String renders n as SQL.
func String(n Node) string {
	var b strings.Builder
	write(&b, n)
	return b.String()
}

// Statements renders a whole script, one statement per line.
func Statements(stmts []Stmt) string {
	var b strings.Builder
	for _, s := range stmts {
		write(&b, s)
		b.WriteString(";\n")
	}
	return b.String()
}

func (n *SelectStmt) String() string             { return String(n) }
func (n *InsertStmt) String() string             { return String(n) }
func (n *UpdateStmt) String() string             { return String(n) }
func (n *DeleteStmt) String() string             { return String(n) }
func (n *CreateTableStmt) String() string        { return String(n) }
func (n *CreateIndexStmt) String() string        { return String(n) }
func (n *CreateViewStmt) String() string         { return String(n) }
func (n *CreateTriggerStmt) String() string      { return String(n) }
func (n *CreateVirtualTableStmt) String() string { return String(n) }
func (n *AlterTableStmt) String() string         { return String(n) }
func (n *ExplainStmt) String() string            { return String(n) }

// w is a small writer wrapper that tracks whether a separating space is
// needed, so callers can emit tokens without worrying about spacing. Spacing
// is never optional: "a--b" would be a comment and "1-1" would still lex as
// two tokens only by luck.
type w struct{ b *strings.Builder }

func (x w) kw(s string) {
	if x.b.Len() > 0 {
		if c := x.b.String()[x.b.Len()-1]; c != ' ' && c != '(' {
			x.b.WriteByte(' ')
		}
	}
	x.b.WriteString(s)
}

func (x w) raw(s string) { x.b.WriteString(s) }

func write(b *strings.Builder, n Node) {
	x := w{b}
	switch t := n.(type) {

	// ---- expressions ----

	case *Ident:
		x.kw(t.Raw)
	case *QualifiedName:
		if t.Schema != nil {
			x.kw(t.Schema.Raw)
			x.raw(".")
			x.raw(t.Name.Raw)
			return
		}
		x.kw(t.Name.Raw)
	case *QualifiedRef:
		for i, p := range t.Parts {
			if i == 0 {
				x.kw(p.Raw)
			} else {
				x.raw("." + p.Raw)
			}
		}
	case *Star:
		if t.Table != nil {
			x.kw(t.Table.Raw)
			x.raw(".*")
			return
		}
		x.kw("*")
	case *Literal:
		x.kw(t.Raw)
	case *BindParam:
		x.kw(t.Raw)
	case *ParenExpr:
		x.kw("(")
		write(b, t.X)
		x.raw(")")
	case *UnaryExpr:
		x.kw(t.Op.String())
		write(b, t.X)
	case *BinaryExpr:
		write(b, t.X)
		x.kw(t.Op.String())
		write(b, t.Y)
	case *CollateExpr:
		write(b, t.X)
		x.kw("COLLATE")
		x.kw(t.Name.Raw)
	case *LikeExpr:
		write(b, t.X)
		if t.Not {
			x.kw("NOT")
		}
		x.kw(t.Op.Raw)
		write(b, t.Y)
		if t.Escape != nil {
			x.kw("ESCAPE")
			write(b, t.Escape)
		}
	case *IsExpr:
		write(b, t.X)
		x.kw("IS")
		if t.Not {
			x.kw("NOT")
		}
		if t.Distinct {
			x.kw("DISTINCT FROM")
		}
		write(b, t.Y)
	case *NullCheckExpr:
		write(b, t.X)
		x.kw(t.Test.String())
	case *BetweenExpr:
		write(b, t.X)
		if t.Not {
			x.kw("NOT")
		}
		x.kw("BETWEEN")
		write(b, t.Lo)
		x.kw("AND")
		write(b, t.Hi)
	case *InExpr:
		write(b, t.X)
		if t.Not {
			x.kw("NOT")
		}
		x.kw("IN")
		switch {
		case t.Select != nil:
			x.kw("(")
			write(b, t.Select)
			x.raw(")")
		case t.Parens:
			x.kw("(")
			writeExprs(b, t.List)
			x.raw(")")
		default:
			write(b, t.Table)
			if t.HasArgs {
				x.raw("(")
				writeExprs(b, t.Args)
				x.raw(")")
			}
		}
	case *CaseExpr:
		x.kw("CASE")
		if t.Operand != nil {
			write(b, t.Operand)
		}
		for _, when := range t.Whens {
			write(b, when)
		}
		if t.Else != nil {
			x.kw("ELSE")
			write(b, t.Else)
		}
		x.kw("END")
	case *CaseWhen:
		x.kw("WHEN")
		write(b, t.When)
		x.kw("THEN")
		write(b, t.Then)
	case *CastExpr:
		x.kw("CAST")
		x.kw("(")
		write(b, t.X)
		x.kw("AS")
		if t.Type != nil {
			write(b, t.Type)
		}
		x.raw(")")
	case *TypeName:
		x.kw(t.Name)
		if len(t.Args) > 0 {
			x.raw("(" + strings.Join(t.Args, ",") + ")")
		}
	case *FuncCall:
		x.kw(t.Name.Raw)
		x.raw("(")
		switch {
		case t.Star:
			x.raw("*")
		default:
			if t.Distinct {
				x.kw("DISTINCT")
			}
			if t.All {
				x.kw("ALL")
			}
			writeExprs(b, t.Args)
			if len(t.OrderBy) > 0 {
				x.kw("ORDER BY")
				writeOrdering(b, t.OrderBy)
			}
		}
		x.raw(")")
		if t.Filter != nil {
			x.kw("FILTER (WHERE")
			write(b, t.Filter)
			x.raw(")")
		}
		if t.Over != nil {
			x.kw("OVER")
			if t.Over.NameOnly {
				x.kw(t.Over.Base.Raw)
			} else {
				x.kw("(")
				write(b, t.Over)
				x.raw(")")
			}
		}
	case *ExistsExpr:
		x.kw("EXISTS (")
		write(b, t.Select)
		x.raw(")")
	case *SubqueryExpr:
		x.kw("(")
		write(b, t.Select)
		x.raw(")")
	case *VectorExpr:
		x.kw("(")
		writeExprs(b, t.List)
		x.raw(")")
	case *RaiseExpr:
		x.kw("RAISE (")
		x.kw(t.Action)
		if t.Message != nil {
			x.raw(",")
			write(b, t.Message)
		}
		x.raw(")")

	// ---- select ----

	case *SelectStmt:
		if t.With != nil {
			write(b, t.With)
		}
		for i, core := range t.Cores {
			if i > 0 {
				x.kw(t.Ops[i-1].Op)
				if t.Ops[i-1].All {
					x.kw("ALL")
				}
			}
			write(b, core)
		}
	case *SelectQuery:
		x.kw("SELECT")
		switch t.Distinct {
		case DistinctDistinct:
			x.kw("DISTINCT")
		case DistinctAll:
			x.kw("ALL")
		}
		for i, c := range t.Columns {
			if i > 0 {
				x.raw(",")
			}
			write(b, c)
		}
		if len(t.From) > 0 {
			x.kw("FROM")
			writeFrom(b, t.From)
		}
		if t.Where != nil {
			x.kw("WHERE")
			write(b, t.Where)
		}
		if len(t.GroupBy) > 0 {
			x.kw("GROUP BY")
			writeExprs(b, t.GroupBy)
		}
		if t.Having != nil {
			x.kw("HAVING")
			write(b, t.Having)
		}
		if len(t.Windows) > 0 {
			x.kw("WINDOW")
			for i, wd := range t.Windows {
				if i > 0 {
					x.raw(",")
				}
				x.kw(wd.Name.Raw)
				x.kw("AS (")
				write(b, wd)
				x.raw(")")
			}
		}
		if len(t.OrderBy) > 0 {
			x.kw("ORDER BY")
			writeOrdering(b, t.OrderBy)
		}
		if t.Limit != nil {
			write(b, t.Limit)
		}
	case *ValuesClause:
		x.kw("VALUES")
		for i, row := range t.Rows {
			if i > 0 {
				x.raw(",")
			}
			x.kw("(")
			writeExprs(b, row)
			x.raw(")")
		}
	case *ResultColumn:
		write(b, t.Expr)
		if t.Alias != nil {
			if t.HasAs {
				x.kw("AS")
			}
			x.kw(t.Alias.Raw)
		}
	case *Limit:
		x.kw("LIMIT")
		if t.Comma {
			write(b, t.Offset)
			x.raw(",")
			write(b, t.Count)
			return
		}
		write(b, t.Count)
		if t.Offset != nil {
			x.kw("OFFSET")
			write(b, t.Offset)
		}
	case *OrderingTerm:
		write(b, t.Expr)
		switch t.Order {
		case SortAsc:
			x.kw("ASC")
		case SortDesc:
			x.kw("DESC")
		}
		switch t.Nulls {
		case NullsFirst:
			x.kw("NULLS FIRST")
		case NullsLast:
			x.kw("NULLS LAST")
		}
	case *TableRef:
		writeTableRef(b, t)
	case *With:
		x.kw("WITH")
		if t.Recursive {
			x.kw("RECURSIVE")
		}
		for i, cte := range t.CTEs {
			if i > 0 {
				x.raw(",")
			}
			write(b, cte)
		}
	case *CTE:
		x.kw(t.Name.Raw)
		if len(t.Columns) > 0 {
			x.kw("(")
			for i, c := range t.Columns {
				if i > 0 {
					x.raw(",")
				}
				x.kw(c.Raw)
			}
			x.raw(")")
		}
		x.kw("AS")
		switch t.Materialized {
		case MaterializedYes:
			x.kw("MATERIALIZED")
		case MaterializedNo:
			x.kw("NOT MATERIALIZED")
		}
		x.kw("(")
		write(b, t.Select)
		x.raw(")")
	case *WindowDef:
		if t.Base != nil {
			x.kw(t.Base.Raw)
		}
		if len(t.Partition) > 0 {
			x.kw("PARTITION BY")
			writeExprs(b, t.Partition)
		}
		if len(t.OrderBy) > 0 {
			x.kw("ORDER BY")
			writeOrdering(b, t.OrderBy)
		}
		writeFrame(b, t)

	// ---- DML ----

	case *InsertStmt:
		writeInsert(b, t)
	case *UpdateStmt:
		writeUpdate(b, t)
	case *DeleteStmt:
		writeDelete(b, t)
	case *SetPair:
		if t.Paren {
			x.kw("(")
			writeIdents(b, t.Columns)
			x.raw(")")
		} else {
			x.kw(t.Columns[0].Raw)
		}
		x.kw("=")
		write(b, t.Value)
	case *Upsert:
		x.kw("ON CONFLICT")
		if t.Target != nil {
			x.kw("(")
			writeOrdering(b, t.Target)
			x.raw(")")
			if t.TargetWhere != nil {
				x.kw("WHERE")
				write(b, t.TargetWhere)
			}
		}
		x.kw("DO")
		if t.DoNothing {
			x.kw("NOTHING")
			return
		}
		x.kw("UPDATE SET")
		for i, s := range t.Set {
			if i > 0 {
				x.raw(",")
			}
			write(b, s)
		}
		if t.Where != nil {
			x.kw("WHERE")
			write(b, t.Where)
		}

	// ---- DDL ----

	case *CreateTableStmt:
		writeCreateTable(b, t)
	case *ColumnDef:
		x.kw(t.Name.Raw)
		if t.Type != nil {
			write(b, t.Type)
		}
		for _, c := range t.Constraints {
			write(b, c)
		}
	case *ColumnConstraint:
		writeColumnConstraint(b, t)
	case *TableConstraint:
		writeTableConstraint(b, t)
	case *IndexedColumn:
		x.kw(t.Name.Raw)
		if t.Collation != nil {
			x.kw("COLLATE")
			x.kw(t.Collation.Raw)
		}
		switch t.Order {
		case SortAsc:
			x.kw("ASC")
		case SortDesc:
			x.kw("DESC")
		}
	case *ForeignKeyClause:
		x.kw("REFERENCES")
		x.kw(t.Table.Raw)
		if len(t.Columns) > 0 {
			x.kw("(")
			for i, c := range t.Columns {
				if i > 0 {
					x.raw(",")
				}
				write(b, c)
			}
			x.raw(")")
		}
		for _, a := range t.Args {
			write(b, a)
		}
	case *ForeignKeyArg:
		if t.Match != nil {
			x.kw("MATCH")
			x.kw(t.Match.Raw)
			return
		}
		x.kw("ON")
		x.kw(t.Event)
		switch t.Action {
		case FKSetNull:
			x.kw("SET NULL")
		case FKSetDefault:
			x.kw("SET DEFAULT")
		case FKCascade:
			x.kw("CASCADE")
		case FKRestrict:
			x.kw("RESTRICT")
		default:
			x.kw("NO ACTION")
		}
	case *DeferClause:
		if t.Not {
			x.kw("NOT")
		}
		x.kw("DEFERRABLE")
		if t.HasInitially {
			x.kw("INITIALLY")
			if t.InitiallyDeferred {
				x.kw("DEFERRED")
			} else {
				x.kw("IMMEDIATE")
			}
		}
	case *CreateIndexStmt:
		x.kw("CREATE")
		if t.Unique {
			x.kw("UNIQUE")
		}
		x.kw("INDEX")
		if t.IfNotExists {
			x.kw("IF NOT EXISTS")
		}
		write(b, t.Name)
		x.kw("ON")
		x.kw(t.Table.Raw)
		x.kw("(")
		writeOrdering(b, t.Columns)
		x.raw(")")
		if t.Where != nil {
			x.kw("WHERE")
			write(b, t.Where)
		}
	case *CreateViewStmt:
		x.kw("CREATE")
		if t.Temp {
			x.kw("TEMP")
		}
		x.kw("VIEW")
		if t.IfNotExists {
			x.kw("IF NOT EXISTS")
		}
		write(b, t.Name)
		if len(t.Columns) > 0 {
			x.kw("(")
			for i, c := range t.Columns {
				if i > 0 {
					x.raw(",")
				}
				write(b, c)
			}
			x.raw(")")
		}
		x.kw("AS")
		write(b, t.Select)
	case *CreateTriggerStmt:
		writeCreateTrigger(b, t)
	case *CreateVirtualTableStmt:
		x.kw("CREATE VIRTUAL TABLE")
		if t.IfNotExists {
			x.kw("IF NOT EXISTS")
		}
		write(b, t.Name)
		x.kw("USING")
		x.kw(t.Module.Raw)
		if t.HasArgs {
			x.raw("(" + strings.Join(t.Args, ",") + ")")
		}
	case *AlterTableStmt:
		writeAlterTable(b, t)
	case *DropTableStmt:
		writeDrop(b, "TABLE", t.IfExists, t.Name)
	case *DropViewStmt:
		writeDrop(b, "VIEW", t.IfExists, t.Name)
	case *DropIndexStmt:
		writeDrop(b, "INDEX", t.IfExists, t.Name)
	case *DropTriggerStmt:
		writeDrop(b, "TRIGGER", t.IfExists, t.Name)

	// ---- everything else ----

	case *BeginStmt:
		x.kw("BEGIN")
		if t.HasType {
			switch t.Type {
			case TxnDeferred:
				x.kw("DEFERRED")
			case TxnImmediate:
				x.kw("IMMEDIATE")
			case TxnExclusive:
				x.kw("EXCLUSIVE")
			}
		}
		writeTransOpt(b, t.HasTransaction, t.Name)
	case *CommitStmt:
		x.kw(t.Keyword)
		writeTransOpt(b, t.HasTransaction, t.Name)
	case *RollbackStmt:
		x.kw("ROLLBACK")
		writeTransOpt(b, t.HasTransaction, t.Name)
		if t.Savepoint != nil {
			x.kw("TO")
			if t.SavepointKeyword {
				x.kw("SAVEPOINT")
			}
			x.kw(t.Savepoint.Raw)
		}
	case *SavepointStmt:
		x.kw("SAVEPOINT")
		x.kw(t.Name.Raw)
	case *ReleaseStmt:
		x.kw("RELEASE")
		if t.SavepointKeyword {
			x.kw("SAVEPOINT")
		}
		x.kw(t.Name.Raw)
	case *AttachStmt:
		x.kw("ATTACH")
		if t.HasDatabase {
			x.kw("DATABASE")
		}
		write(b, t.File)
		x.kw("AS")
		write(b, t.Name)
		if t.Key != nil {
			x.kw("KEY")
			write(b, t.Key)
		}
	case *DetachStmt:
		x.kw("DETACH")
		if t.HasDatabase {
			x.kw("DATABASE")
		}
		write(b, t.Name)
	case *PragmaStmt:
		x.kw("PRAGMA")
		write(b, t.Name)
		if t.HasValue {
			if t.Paren {
				x.raw("(" + t.Value + ")")
			} else {
				x.kw("=")
				x.kw(t.Value)
			}
		}
	case *VacuumStmt:
		x.kw("VACUUM")
		if t.Schema != nil {
			x.kw(t.Schema.Raw)
		}
		if t.Into != nil {
			x.kw("INTO")
			write(b, t.Into)
		}
	case *AnalyzeStmt:
		x.kw("ANALYZE")
		if t.Name != nil {
			write(b, t.Name)
		}
	case *ReindexStmt:
		x.kw("REINDEX")
		if t.Name != nil {
			write(b, t.Name)
		}
	case *ExplainStmt:
		x.kw("EXPLAIN")
		if t.QueryPlan {
			x.kw("QUERY PLAN")
		}
		write(b, t.Stmt)
	}
}

// writeTransOpt renders "trans_opt". The keyword can appear without a name,
// so it is tracked separately.
func writeTransOpt(b *strings.Builder, hasKeyword bool, name *Ident) {
	if !hasKeyword {
		return
	}
	x := w{b}
	x.kw("TRANSACTION")
	if name != nil {
		x.kw(name.Raw)
	}
}

func writeDrop(b *strings.Builder, kind string, ifExists bool, name *QualifiedName) {
	x := w{b}
	x.kw("DROP")
	x.kw(kind)
	if ifExists {
		x.kw("IF EXISTS")
	}
	write(b, name)
}

func writeExprs(b *strings.Builder, list []Expr) {
	for i, e := range list {
		if i > 0 {
			b.WriteString(",")
		}
		write(b, e)
	}
}

func writeIdents(b *strings.Builder, list []*Ident) {
	x := w{b}
	for i, e := range list {
		if i > 0 {
			x.raw(",")
		}
		x.kw(e.Raw)
	}
}

func writeOrdering(b *strings.Builder, list []*OrderingTerm) {
	for i, t := range list {
		if i > 0 {
			b.WriteString(",")
		}
		write(b, t)
	}
}

func writeFrom(b *strings.Builder, list []*TableRef) {
	x := w{b}
	for _, t := range list {
		if t.Join != nil {
			if t.Join.Type&JoinComma != 0 {
				x.raw(",")
			} else {
				for _, word := range t.Join.Words {
					x.kw(word)
				}
			}
		}
		writeTableRef(b, t)
	}
}

func writeTableRef(b *strings.Builder, t *TableRef) {
	x := w{b}
	switch {
	case t.Select != nil:
		x.kw("(")
		write(b, t.Select)
		x.raw(")")
	case t.List != nil:
		x.kw("(")
		writeFrom(b, t.List)
		x.raw(")")
	default:
		write(b, t.Name)
		if t.HasArgs {
			x.raw("(")
			writeExprs(b, t.Args)
			x.raw(")")
		}
	}
	if t.Alias != nil {
		if t.HasAs {
			x.kw("AS")
		}
		x.kw(t.Alias.Raw)
	}
	if t.IndexedBy != nil {
		x.kw("INDEXED BY")
		x.kw(t.IndexedBy.Raw)
	}
	if t.NotIndexed {
		x.kw("NOT INDEXED")
	}
	if t.On != nil {
		x.kw("ON")
		write(b, t.On)
	}
	if len(t.Using) > 0 {
		x.kw("USING (")
		writeIdents(b, t.Using)
		x.raw(")")
	}
}

func writeFrame(b *strings.Builder, n *WindowDef) {
	x := w{b}
	switch n.Frame {
	case FrameRange:
		x.kw("RANGE")
	case FrameRows:
		x.kw("ROWS")
	case FrameGroups:
		x.kw("GROUPS")
	default:
		return
	}
	if n.EndBound != nil {
		x.kw("BETWEEN")
		writeBound(b, n.StartBound)
		x.kw("AND")
		writeBound(b, n.EndBound)
	} else {
		writeBound(b, n.StartBound)
	}
	switch n.Exclude {
	case ExcludeNoOthers:
		x.kw("EXCLUDE NO OTHERS")
	case ExcludeCurrentRow:
		x.kw("EXCLUDE CURRENT ROW")
	case ExcludeGroup:
		x.kw("EXCLUDE GROUP")
	case ExcludeTies:
		x.kw("EXCLUDE TIES")
	}
}

func writeBound(b *strings.Builder, bound *FrameBound) {
	x := w{b}
	switch bound.Type {
	case BoundUnboundedPreceding:
		x.kw("UNBOUNDED PRECEDING")
	case BoundUnboundedFollowing:
		x.kw("UNBOUNDED FOLLOWING")
	case BoundCurrentRow:
		x.kw("CURRENT ROW")
	case BoundPreceding:
		write(b, bound.Expr)
		x.kw("PRECEDING")
	case BoundFollowing:
		write(b, bound.Expr)
		x.kw("FOLLOWING")
	}
}

func writeInsert(b *strings.Builder, t *InsertStmt) {
	x := w{b}
	if t.With != nil {
		write(b, t.With)
	}
	if t.Replace {
		x.kw("REPLACE")
	} else {
		x.kw("INSERT")
		writeOrConflict(b, t.OrConflict)
	}
	x.kw("INTO")
	write(b, t.Table)
	if t.Alias != nil {
		x.kw("AS")
		x.kw(t.Alias.Raw)
	}
	if len(t.Columns) > 0 {
		x.kw("(")
		writeIdents(b, t.Columns)
		x.raw(")")
	}
	if t.DefaultValues {
		x.kw("DEFAULT VALUES")
	} else {
		write(b, t.Select)
	}
	for _, u := range t.Upserts {
		write(b, u)
	}
	writeReturning(b, t.Returning)
}

func writeUpdate(b *strings.Builder, t *UpdateStmt) {
	x := w{b}
	if t.With != nil {
		write(b, t.With)
	}
	x.kw("UPDATE")
	writeOrConflict(b, t.OrConflict)
	write(b, t.Table)
	if t.Alias != nil {
		x.kw("AS")
		x.kw(t.Alias.Raw)
	}
	writeIndexedBy(b, t.IndexedBy, t.NotIndexed)
	x.kw("SET")
	for i, s := range t.Set {
		if i > 0 {
			x.raw(",")
		}
		write(b, s)
	}
	if len(t.From) > 0 {
		x.kw("FROM")
		writeFrom(b, t.From)
	}
	if t.Where != nil {
		x.kw("WHERE")
		write(b, t.Where)
	}
	writeReturning(b, t.Returning)
}

func writeDelete(b *strings.Builder, t *DeleteStmt) {
	x := w{b}
	if t.With != nil {
		write(b, t.With)
	}
	x.kw("DELETE FROM")
	write(b, t.Table)
	if t.Alias != nil {
		x.kw("AS")
		x.kw(t.Alias.Raw)
	}
	writeIndexedBy(b, t.IndexedBy, t.NotIndexed)
	if t.Where != nil {
		x.kw("WHERE")
		write(b, t.Where)
	}
	writeReturning(b, t.Returning)
}

func writeIndexedBy(b *strings.Builder, by *Ident, notIndexed bool) {
	x := w{b}
	if by != nil {
		x.kw("INDEXED BY")
		x.kw(by.Raw)
	}
	if notIndexed {
		x.kw("NOT INDEXED")
	}
}

func writeReturning(b *strings.Builder, cols []*ResultColumn) {
	if len(cols) == 0 {
		return
	}
	x := w{b}
	x.kw("RETURNING")
	for i, c := range cols {
		if i > 0 {
			x.raw(",")
		}
		write(b, c)
	}
}

func writeOrConflict(b *strings.Builder, a ConflictAction) {
	if a == ConflictDefault {
		return
	}
	x := w{b}
	x.kw("OR")
	x.kw(a.String())
}

func writeOnConflict(b *strings.Builder, a ConflictAction) {
	if a == ConflictDefault {
		return
	}
	x := w{b}
	x.kw("ON CONFLICT")
	x.kw(a.String())
}

func writeCreateTable(b *strings.Builder, t *CreateTableStmt) {
	x := w{b}
	x.kw("CREATE")
	if t.Temp {
		x.kw("TEMP")
	}
	x.kw("TABLE")
	if t.IfNotExists {
		x.kw("IF NOT EXISTS")
	}
	write(b, t.Name)
	if t.Select != nil {
		x.kw("AS")
		write(b, t.Select)
		return
	}
	x.kw("(")
	for i, c := range t.Columns {
		if i > 0 {
			x.raw(",")
		}
		write(b, c)
	}
	for _, c := range t.Constraints {
		x.raw(",")
		write(b, c)
	}
	x.raw(")")
	for i, opt := range t.Options {
		if i > 0 {
			x.raw(",")
		}
		if t.WithoutOpt[i] {
			x.kw("WITHOUT")
		}
		x.kw(opt.Raw)
	}
}

func writeColumnConstraint(b *strings.Builder, t *ColumnConstraint) {
	x := w{b}
	if t.Name != nil {
		x.kw("CONSTRAINT")
		x.kw(t.Name.Raw)
	}
	switch t.Kind {
	case ColumnNull:
		x.kw("NULL")
		writeOnConflict(b, t.OnConflict)
	case ColumnNotNull:
		x.kw("NOT NULL")
		writeOnConflict(b, t.OnConflict)
	case ColumnPrimaryKey:
		x.kw("PRIMARY KEY")
		switch t.Order {
		case SortAsc:
			x.kw("ASC")
		case SortDesc:
			x.kw("DESC")
		}
		writeOnConflict(b, t.OnConflict)
		if t.AutoIncrement {
			x.kw("AUTOINCREMENT")
		}
	case ColumnUnique:
		x.kw("UNIQUE")
		writeOnConflict(b, t.OnConflict)
	case ColumnCheck:
		x.kw("CHECK (")
		write(b, t.Expr)
		x.raw(")")
	case ColumnDefault:
		x.kw("DEFAULT")
		write(b, t.Expr)
	case ColumnCollate:
		x.kw("COLLATE")
		x.kw(t.Collation.Raw)
	case ColumnReferences:
		write(b, t.References)
	case ColumnGenerated:
		if t.AlwaysWord {
			x.kw("GENERATED ALWAYS")
		}
		x.kw("AS (")
		write(b, t.Expr)
		x.raw(")")
		if t.GeneratedKind != nil {
			x.kw(t.GeneratedKind.Raw)
		}
	case ColumnDeferrable:
		write(b, t.Deferrable)
	}
}

func writeTableConstraint(b *strings.Builder, t *TableConstraint) {
	x := w{b}
	if t.Name != nil {
		x.kw("CONSTRAINT")
		x.kw(t.Name.Raw)
	}
	switch t.Kind {
	case TablePrimaryKey:
		x.kw("PRIMARY KEY (")
		writeOrdering(b, t.Columns)
		if t.AutoIncrement {
			x.kw("AUTOINCREMENT")
		}
		x.raw(")")
		writeOnConflict(b, t.OnConflict)
	case TableUnique:
		x.kw("UNIQUE (")
		writeOrdering(b, t.Columns)
		x.raw(")")
		writeOnConflict(b, t.OnConflict)
	case TableCheck:
		x.kw("CHECK (")
		write(b, t.Expr)
		x.raw(")")
		writeOnConflict(b, t.OnConflict)
	case TableForeignKey:
		x.kw("FOREIGN KEY (")
		for i, c := range t.FKColumns {
			if i > 0 {
				x.raw(",")
			}
			write(b, c)
		}
		x.raw(")")
		write(b, t.References)
		if t.Deferrable != nil {
			write(b, t.Deferrable)
		}
	}
}

func writeCreateTrigger(b *strings.Builder, t *CreateTriggerStmt) {
	x := w{b}
	x.kw("CREATE")
	if t.Temp {
		x.kw("TEMP")
	}
	x.kw("TRIGGER")
	if t.IfNotExists {
		x.kw("IF NOT EXISTS")
	}
	write(b, t.Name)
	if t.HasTime {
		switch t.Time {
		case TriggerBefore:
			x.kw("BEFORE")
		case TriggerAfter:
			x.kw("AFTER")
		case TriggerInsteadOf:
			x.kw("INSTEAD OF")
		}
	}
	x.kw(t.Event)
	if len(t.UpdateOf) > 0 {
		x.kw("OF")
		writeIdents(b, t.UpdateOf)
	}
	x.kw("ON")
	write(b, t.Table)
	if t.ForEachRow {
		x.kw("FOR EACH ROW")
	}
	if t.When != nil {
		x.kw("WHEN")
		write(b, t.When)
	}
	x.kw("BEGIN")
	for _, s := range t.Body {
		write(b, s)
		x.raw(";")
	}
	x.kw("END")
}

func writeAlterTable(b *strings.Builder, t *AlterTableStmt) {
	x := w{b}
	x.kw("ALTER TABLE")
	write(b, t.Table)
	column := func() {
		if t.ColumnKeyword {
			x.kw("COLUMN")
		}
	}
	switch t.Action {
	case AlterRenameTable:
		x.kw("RENAME TO")
		x.kw(t.NewName.Raw)
	case AlterRenameColumn:
		x.kw("RENAME")
		column()
		x.kw(t.Column.Raw)
		x.kw("TO")
		x.kw(t.NewName.Raw)
	case AlterAddColumn:
		x.kw("ADD")
		column()
		write(b, t.ColumnDef)
	case AlterDropColumn:
		x.kw("DROP")
		column()
		x.kw(t.Column.Raw)
	case AlterDropConstraint:
		x.kw("DROP CONSTRAINT")
		x.kw(t.ConstraintName.Raw)
	case AlterDropNotNull:
		x.kw("ALTER")
		column()
		x.kw(t.Column.Raw)
		x.kw("DROP NOT NULL")
	case AlterSetNotNull:
		x.kw("ALTER")
		column()
		x.kw(t.Column.Raw)
		x.kw("SET NOT NULL")
		writeOnConflict(b, t.OnConflict)
	case AlterAddConstraintCheck:
		x.kw("ADD")
		if t.ConstraintName != nil {
			x.kw("CONSTRAINT")
			x.kw(t.ConstraintName.Raw)
		}
		x.kw("CHECK (")
		write(b, t.Expr)
		x.raw(")")
		writeOnConflict(b, t.OnConflict)
	}
}

// The remaining Stringers exist for the dump renderer and for error
// messages: an enum that prints as a number makes a snapshot diff unreadable.

func (k LiteralKind) String() string {
	return name(int(k), "NULL", "INTEGER", "FLOAT", "STRING", "BLOB",
		"CURRENT_DATE", "CURRENT_TIME", "CURRENT_TIMESTAMP")
}

func (t NullTest) String() string { return name(int(t), "ISNULL", "NOTNULL", "NOT NULL") }

func (k ParamKind) String() string {
	return name(int(k), "?", "?NNN", ":name", "@name", "$name")
}

func (q QuoteStyle) String() string {
	if q == QuoteNone {
		return "none"
	}
	return string(rune(q))
}

func (d Distinct) String() string { return name(int(d), "none", "DISTINCT", "ALL") }

func (o SortOrder) String() string { return name(int(o), "default", "ASC", "DESC") }

func (o NullsOrder) String() string {
	return name(int(o), "default", "NULLS FIRST", "NULLS LAST")
}

func (m Materialized) String() string {
	return name(int(m), "unspecified", "MATERIALIZED", "NOT MATERIALIZED")
}

func (f FrameType) String() string { return name(int(f), "none", "RANGE", "ROWS", "GROUPS") }

func (b FrameBoundType) String() string {
	return name(int(b), "UNBOUNDED PRECEDING", "PRECEDING", "CURRENT ROW",
		"FOLLOWING", "UNBOUNDED FOLLOWING")
}

func (e FrameExclude) String() string {
	return name(int(e), "none", "EXCLUDE NO OTHERS", "EXCLUDE CURRENT ROW",
		"EXCLUDE GROUP", "EXCLUDE TIES")
}

func (k ColumnConstraintKind) String() string {
	return name(int(k), "NULL", "NOT NULL", "PRIMARY KEY", "UNIQUE", "CHECK",
		"DEFAULT", "COLLATE", "REFERENCES", "GENERATED", "DEFERRABLE")
}

func (k TableConstraintKind) String() string {
	return name(int(k), "PRIMARY KEY", "UNIQUE", "CHECK", "FOREIGN KEY")
}

func (a ForeignKeyAction) String() string {
	return name(int(a), "NO ACTION", "SET NULL", "SET DEFAULT", "CASCADE", "RESTRICT")
}

func (a AlterAction) String() string {
	return name(int(a), "RENAME TO", "RENAME COLUMN", "ADD COLUMN", "DROP COLUMN",
		"DROP CONSTRAINT", "DROP NOT NULL", "SET NOT NULL", "ADD CHECK")
}

func (t TriggerTime) String() string { return name(int(t), "BEFORE", "AFTER", "INSTEAD OF") }

func (t TransactionType) String() string {
	return name(int(t), "DEFERRED", "IMMEDIATE", "EXCLUSIVE")
}

// String spells a join out the way sqlite3JoinType's bitmask reads.
func (t JoinType) String() string {
	var parts []string
	for _, bit := range []struct {
		mask JoinType
		name string
	}{
		{JoinComma, ","}, {JoinNatural, "NATURAL"}, {JoinLeft, "LEFT"},
		{JoinRight, "RIGHT"}, {JoinOuter, "OUTER"}, {JoinInner, "INNER"},
		{JoinCross, "CROSS"},
	} {
		if t&bit.mask != 0 {
			parts = append(parts, bit.name)
		}
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, "|")
}

func name(i int, names ...string) string {
	if i < 0 || i >= len(names) {
		return "?"
	}
	return names[i]
}
