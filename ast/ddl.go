package ast

// CreateTableStmt is CREATE TABLE, in both the column-list and the AS SELECT
// forms.
//
//	cmd ::= create_table create_table_args.
//	create_table ::= createkw temp TABLE ifnotexists nm dbnm.
//	create_table_args ::= LP columnlist conslist_opt RP table_option_set.
//	create_table_args ::= AS select.
type CreateTableStmt struct {
	Span
	Temp        bool               `json:"temp,omitempty"`
	IfNotExists bool               `json:"ifNotExists,omitempty"`
	Name        *QualifiedName     `json:"name"`
	Columns     []*ColumnDef       `json:"columns,omitempty"`
	Constraints []*TableConstraint `json:"constraints,omitempty"`
	Options     []*Ident           `json:"options,omitempty"` // WITHOUT ROWID, STRICT
	WithoutOpt  []bool             `json:"withoutOpt,omitempty"`
	Select      *SelectStmt        `json:"select,omitempty"`
}

func (*CreateTableStmt) stmtNode() {}
func (n *CreateTableStmt) Children() []Node {
	out := nodes(n.Name)
	for _, c := range n.Columns {
		out = append(out, c)
	}
	for _, c := range n.Constraints {
		out = append(out, c)
	}
	for _, o := range n.Options {
		out = append(out, o)
	}
	return append(out, nodes(n.Select)...)
}

// ColumnDef is one column of a CREATE TABLE (or of ALTER TABLE ADD COLUMN).
//
//	columnname ::= nm typetoken. / carglist ::= carglist ccons.
type ColumnDef struct {
	Span
	Name        *ColumnName         `json:"name"`
	Type        *TypeName           `json:"type,omitempty"`
	Constraints []*ColumnConstraint `json:"constraints,omitempty"`
}

func (n *ColumnDef) Children() []Node {
	out := nodes(n.Name, n.Type)
	for _, c := range n.Constraints {
		out = append(out, c)
	}
	return out
}

// ColumnName wraps the identifier naming a column, so that a ColumnDef's
// name keeps its own span distinct from the whole definition's.
type ColumnName = Ident

// ColumnConstraintKind enumerates the "ccons" alternatives.
type ColumnConstraintKind int

const (
	ColumnNull ColumnConstraintKind = iota
	ColumnNotNull
	ColumnPrimaryKey
	ColumnUnique
	ColumnCheck
	ColumnDefault
	ColumnCollate
	ColumnReferences
	ColumnGenerated
	ColumnDeferrable // a bare [NOT] DEFERRABLE clause
)

// ColumnConstraint is one column constraint. Name is the preceding
// CONSTRAINT clause, if any; SQLite attaches it to the constraint that
// follows.
//
//	ccons ::= CONSTRAINT nm. / NULL onconf. / NOT NULL onconf.
//	ccons ::= PRIMARY KEY sortorder onconf autoinc. / UNIQUE onconf.
//	ccons ::= CHECK LP expr RP. / DEFAULT .... / COLLATE ids.
//	ccons ::= REFERENCES nm eidlist_opt refargs. / defer_subclause.
//	ccons ::= [GENERATED ALWAYS] AS generated.
type ColumnConstraint struct {
	Span
	Name          *Ident               `json:"name,omitempty"`
	Kind          ColumnConstraintKind `json:"kind"`
	OnConflict    ConflictAction       `json:"onConflict,omitempty"`
	Order         SortOrder            `json:"order,omitempty"` // PRIMARY KEY ASC/DESC
	AutoIncrement bool                 `json:"autoIncrement,omitempty"`
	Expr          Expr                 `json:"expr,omitempty"` // CHECK, DEFAULT, GENERATED
	Collation     *Ident               `json:"collation,omitempty"`
	References    *ForeignKeyClause    `json:"references,omitempty"`
	GeneratedKind *Ident               `json:"generatedKind,omitempty"` // STORED / VIRTUAL
	AlwaysWord    bool                 `json:"alwaysWord,omitempty"`    // GENERATED ALWAYS spelling
	Deferrable    *DeferClause         `json:"deferrable,omitempty"`
}

func (n *ColumnConstraint) Children() []Node {
	return nodes(n.Name, n.Expr, n.Collation, n.References, n.GeneratedKind, n.Deferrable)
}

// TableConstraintKind enumerates the "tcons" alternatives.
type TableConstraintKind int

const (
	TablePrimaryKey TableConstraintKind = iota
	TableUnique
	TableCheck
	TableForeignKey
)

// TableConstraint is one table-level constraint.
//
//	tcons ::= CONSTRAINT nm.
//	tcons ::= PRIMARY KEY LP sortlist autoinc RP onconf. / UNIQUE LP sortlist RP onconf.
//	tcons ::= CHECK LP expr RP onconf.
//	tcons ::= FOREIGN KEY LP eidlist RP REFERENCES nm eidlist_opt refargs
//	          defer_subclause_opt.
type TableConstraint struct {
	Span
	Name          *Ident              `json:"name,omitempty"`
	Kind          TableConstraintKind `json:"kind"`
	Columns       []*OrderingTerm     `json:"columns,omitempty"` // PRIMARY KEY / UNIQUE
	AutoIncrement bool                `json:"autoIncrement,omitempty"`
	OnConflict    ConflictAction      `json:"onConflict,omitempty"`
	Expr          Expr                `json:"expr,omitempty"` // CHECK
	FKColumns     []*IndexedColumn    `json:"fkColumns,omitempty"`
	References    *ForeignKeyClause   `json:"references,omitempty"`
	Deferrable    *DeferClause        `json:"deferrable,omitempty"`
}

func (n *TableConstraint) Children() []Node {
	out := nodes(n.Name)
	for _, c := range n.Columns {
		out = append(out, c)
	}
	out = append(out, nodes(n.Expr)...)
	for _, c := range n.FKColumns {
		out = append(out, c)
	}
	return append(out, nodes(n.References, n.Deferrable)...)
}

// IndexedColumn is one entry of an "eidlist": a column name with the COLLATE
// and ASC/DESC decorations SQLite still parses for historical schemas.
//
//	eidlist ::= eidlist COMMA nm collate sortorder.
type IndexedColumn struct {
	Span
	Name    *Ident    `json:"name"`
	Collate bool      `json:"collate,omitempty"`
	Order   SortOrder `json:"order,omitempty"`
}

func (n *IndexedColumn) Children() []Node { return nodes(n.Name) }

// ForeignKeyAction is the action of an ON DELETE / ON UPDATE clause.
type ForeignKeyAction int

const (
	FKNoAction ForeignKeyAction = iota
	FKSetNull
	FKSetDefault
	FKCascade
	FKRestrict
)

// ForeignKeyClause is a REFERENCES clause and its arguments.
//
//	ccons ::= REFERENCES nm eidlist_opt refargs.
//	refarg ::= MATCH nm. / ON INSERT refact. / ON DELETE refact. / ON UPDATE refact.
type ForeignKeyClause struct {
	Span
	Table   *Ident           `json:"table"`
	Columns []*IndexedColumn `json:"columns,omitempty"`
	Args    []*ForeignKeyArg `json:"args,omitempty"`
}

func (n *ForeignKeyClause) Children() []Node {
	out := nodes(n.Table)
	for _, c := range n.Columns {
		out = append(out, c)
	}
	for _, a := range n.Args {
		out = append(out, a)
	}
	return out
}

// ForeignKeyArg is one refarg: a MATCH clause or an ON <event> action.
type ForeignKeyArg struct {
	Span
	Match  *Ident           `json:"match,omitempty"`
	Event  string           `json:"event,omitempty"` // INSERT, DELETE, UPDATE
	Action ForeignKeyAction `json:"action,omitempty"`
}

func (n *ForeignKeyArg) Children() []Node { return nodes(n.Match) }

// DeferClause is "[NOT] DEFERRABLE [INITIALLY DEFERRED|IMMEDIATE]".
//
//	defer_subclause ::= NOT DEFERRABLE init_deferred_pred_opt.
//	defer_subclause ::= DEFERRABLE init_deferred_pred_opt.
type DeferClause struct {
	Span
	Not               bool `json:"not,omitempty"`
	InitiallyDeferred bool `json:"initiallyDeferred,omitempty"`
	HasInitially      bool `json:"hasInitially,omitempty"`
}

func (n *DeferClause) Children() []Node { return nil }

// CreateIndexStmt is CREATE [UNIQUE] INDEX.
//
//	cmd ::= createkw uniqueflag INDEX ifnotexists nm dbnm ON nm LP sortlist RP
//	        where_opt.
type CreateIndexStmt struct {
	Span
	Unique      bool            `json:"unique,omitempty"`
	IfNotExists bool            `json:"ifNotExists,omitempty"`
	Name        *QualifiedName  `json:"name"`
	Table       *Ident          `json:"table"`
	Columns     []*OrderingTerm `json:"columns"`
	Where       Expr            `json:"where,omitempty"`
}

func (*CreateIndexStmt) stmtNode() {}
func (n *CreateIndexStmt) Children() []Node {
	out := nodes(n.Name, n.Table)
	for _, c := range n.Columns {
		out = append(out, c)
	}
	return append(out, nodes(n.Where)...)
}

// CreateViewStmt is CREATE [TEMP] VIEW.
//
//	cmd ::= createkw temp VIEW ifnotexists nm dbnm eidlist_opt AS select.
type CreateViewStmt struct {
	Span
	Temp        bool             `json:"temp,omitempty"`
	IfNotExists bool             `json:"ifNotExists,omitempty"`
	Name        *QualifiedName   `json:"name"`
	Columns     []*IndexedColumn `json:"columns,omitempty"`
	Select      *SelectStmt      `json:"select"`
}

func (*CreateViewStmt) stmtNode() {}
func (n *CreateViewStmt) Children() []Node {
	out := nodes(n.Name)
	for _, c := range n.Columns {
		out = append(out, c)
	}
	return append(out, nodes(n.Select)...)
}

// TriggerTime is BEFORE, AFTER or INSTEAD OF.
type TriggerTime int

const (
	TriggerBefore TriggerTime = iota
	TriggerAfter
	TriggerInsteadOf
)

// CreateTriggerStmt is CREATE [TEMP] TRIGGER.
//
//	cmd ::= createkw trigger_decl BEGIN trigger_cmd_list END.
//	trigger_decl ::= temp TRIGGER ifnotexists nm dbnm trigger_time
//	                 trigger_event ON fullname foreach_clause when_clause.
type CreateTriggerStmt struct {
	Span
	Temp        bool           `json:"temp,omitempty"`
	IfNotExists bool           `json:"ifNotExists,omitempty"`
	Name        *QualifiedName `json:"name"`
	Time        TriggerTime    `json:"time,omitempty"`
	HasTime     bool           `json:"hasTime,omitempty"`
	Event       string         `json:"event"` // DELETE, INSERT, UPDATE
	UpdateOf    []*Ident       `json:"updateOf,omitempty"`
	Table       *QualifiedName `json:"table"`
	ForEachRow  bool           `json:"forEachRow,omitempty"`
	When        Expr           `json:"when,omitempty"`
	Body        []Stmt         `json:"body"`
}

func (*CreateTriggerStmt) stmtNode() {}
func (n *CreateTriggerStmt) Children() []Node {
	out := nodes(n.Name)
	for _, u := range n.UpdateOf {
		out = append(out, u)
	}
	out = append(out, nodes(n.Table, n.When)...)
	for _, s := range n.Body {
		out = append(out, s)
	}
	return out
}

// CreateVirtualTableStmt is CREATE VIRTUAL TABLE ... USING module(args).
// Module arguments are kept as their raw source text, exactly as SQLite does
// (the "anylist" production accepts any balanced token sequence).
//
//	create_vtab ::= createkw VIRTUAL TABLE ifnotexists nm dbnm USING nm.
//	cmd ::= create_vtab LP vtabarglist RP.
type CreateVirtualTableStmt struct {
	Span
	IfNotExists bool           `json:"ifNotExists,omitempty"`
	Name        *QualifiedName `json:"name"`
	Module      *Ident         `json:"module"`
	HasArgs     bool           `json:"hasArgs,omitempty"`
	Args        []string       `json:"args,omitempty"`
}

func (*CreateVirtualTableStmt) stmtNode() {}
func (n *CreateVirtualTableStmt) Children() []Node {
	return nodes(n.Name, n.Module)
}

// AlterAction enumerates the ALTER TABLE forms of the pinned build.
type AlterAction int

const (
	AlterRenameTable AlterAction = iota
	AlterRenameColumn
	AlterAddColumn
	AlterDropColumn
	AlterDropConstraint
	AlterDropNotNull
	AlterSetNotNull
	AlterAddConstraintCheck
)

// AlterTableStmt is ALTER TABLE in all the forms the pinned build accepts.
//
//	cmd ::= ALTER TABLE fullname RENAME TO nm.
//	cmd ::= alter_add carglist. (ALTER TABLE ... ADD [COLUMN] nm typetoken)
//	cmd ::= ALTER TABLE fullname DROP kwcolumn_opt nm.
//	cmd ::= ALTER TABLE fullname RENAME kwcolumn_opt nm TO nm.
//	cmd ::= ALTER TABLE fullname DROP CONSTRAINT nm.
//	cmd ::= ALTER TABLE fullname ALTER kwcolumn_opt nm DROP NOT NULL.
//	cmd ::= ALTER TABLE fullname ALTER kwcolumn_opt nm SET NOT NULL onconf.
//	cmd ::= ALTER TABLE fullname ADD [CONSTRAINT nm] CHECK LP expr RP onconf.
type AlterTableStmt struct {
	Span
	Table          *QualifiedName `json:"table"`
	Action         AlterAction    `json:"action"`
	ColumnKeyword  bool           `json:"columnKeyword,omitempty"`
	Column         *Ident         `json:"column,omitempty"`
	NewName        *Ident         `json:"newName,omitempty"`
	ConstraintName *Ident         `json:"constraintName,omitempty"`
	ColumnDef      *ColumnDef     `json:"columnDef,omitempty"`
	Expr           Expr           `json:"expr,omitempty"`
	OnConflict     ConflictAction `json:"onConflict,omitempty"`
}

func (*AlterTableStmt) stmtNode() {}
func (n *AlterTableStmt) Children() []Node {
	return nodes(n.Table, n.Column, n.NewName, n.ConstraintName, n.ColumnDef, n.Expr)
}

// DropTableStmt is DROP TABLE.
//
//	cmd ::= DROP TABLE ifexists fullname.
type DropTableStmt struct {
	Span
	IfExists bool           `json:"ifExists,omitempty"`
	Name     *QualifiedName `json:"name"`
}

func (*DropTableStmt) stmtNode()          {}
func (n *DropTableStmt) Children() []Node { return nodes(n.Name) }

// DropViewStmt is DROP VIEW.
//
//	cmd ::= DROP VIEW ifexists fullname.
type DropViewStmt struct {
	Span
	IfExists bool           `json:"ifExists,omitempty"`
	Name     *QualifiedName `json:"name"`
}

func (*DropViewStmt) stmtNode()          {}
func (n *DropViewStmt) Children() []Node { return nodes(n.Name) }

// DropIndexStmt is DROP INDEX.
//
//	cmd ::= DROP INDEX ifexists fullname.
type DropIndexStmt struct {
	Span
	IfExists bool           `json:"ifExists,omitempty"`
	Name     *QualifiedName `json:"name"`
}

func (*DropIndexStmt) stmtNode()          {}
func (n *DropIndexStmt) Children() []Node { return nodes(n.Name) }

// DropTriggerStmt is DROP TRIGGER.
//
//	cmd ::= DROP TRIGGER ifexists fullname.
type DropTriggerStmt struct {
	Span
	IfExists bool           `json:"ifExists,omitempty"`
	Name     *QualifiedName `json:"name"`
}

func (*DropTriggerStmt) stmtNode()          {}
func (n *DropTriggerStmt) Children() []Node { return nodes(n.Name) }
