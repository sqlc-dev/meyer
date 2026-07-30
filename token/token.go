// Package token defines the lexical tokens of the SQLite SQL dialect.
//
// Kind mirrors the TK_* token codes SQLite's tokenizer produces
// (src/tokenize.c) and the grammar consumes (src/parse.y). Names match the
// SQLite spelling so grammar attributions in the parser read directly
// against parse.y; where SQLite's name differs from the keyword (TK_COLUMNKW
// for COLUMN, TK_AUTOINCR for AUTOINCREMENT) the SQLite name is kept.
package token

// Kind identifies the lexical class of a token.
type Kind int

// The token kinds. The ordering carries no meaning (unlike SQLite's
// generated codes, which are ordered to shrink the LALR tables).
const (
	EOF Kind = iota // end of input; also the synthetic tokens SQLite appends

	// Tokenizer-only kinds. SPACE and COMMENT never reach the parser.
	SPACE
	COMMENT
	ILLEGAL // "unrecognized token"

	// Literals and names.
	ID
	STRING
	INTEGER
	FLOAT
	BLOB
	VARIABLE
	QNUMBER // numeric literal containing '_' digit separators

	// Punctuation and operators.
	LP     // (
	RP     // )
	SEMI   // ;
	COMMA  // ,
	DOT    // .
	PLUS   // +
	MINUS  // -
	STAR   // *
	SLASH  // /
	REM    // %
	EQ     // = or ==
	NE     // != or <>
	LT     // <
	LE     // <=
	GT     // >
	GE     // >=
	LSHIFT // <<
	RSHIFT // >>
	BITAND // &
	BITOR  // |
	BITNOT // ~
	CONCAT // ||
	PTR    // -> or ->>

	// Keywords, in the order of SQLite's keyword table.
	ABORT
	ACTION
	ADD
	AFTER
	ALL
	ALTER
	ALWAYS
	ANALYZE
	AND
	AS
	ASC
	ATTACH
	AUTOINCR
	BEFORE
	BEGIN
	BETWEEN
	BY
	CASCADE
	CASE
	CAST
	CHECK
	COLLATE
	COLUMNKW
	COMMIT
	CONFLICT
	CONSTRAINT
	CREATE
	CURRENT
	CTIME_KW // CURRENT_DATE, CURRENT_TIME, CURRENT_TIMESTAMP
	DATABASE
	DEFAULT
	DEFERRED
	DEFERRABLE
	DELETE
	DESC
	DETACH
	DISTINCT
	DO
	DROP
	END
	EACH
	ELSE
	ESCAPE
	EXCEPT
	EXCLUSIVE
	EXCLUDE
	EXISTS
	EXPLAIN
	FAIL
	FILTER
	FIRST
	FOLLOWING
	FOR
	FOREIGN
	FROM
	GENERATED
	GROUP
	GROUPS
	HAVING
	IF
	IGNORE
	IMMEDIATE
	IN
	INDEX
	INDEXED
	INITIALLY
	INSERT
	INSTEAD
	INTERSECT
	INTO
	IS
	ISNULL
	JOIN
	JOIN_KW // CROSS, FULL, INNER, LEFT, NATURAL, OUTER, RIGHT
	KEY
	LAST
	LIKE_KW // GLOB, LIKE, REGEXP
	LIMIT
	MATCH
	MATERIALIZED
	NO
	NOT
	NOTHING
	NOTNULL
	NULL
	NULLS
	OF
	OFFSET
	ON
	OR
	ORDER
	OTHERS
	OVER
	PARTITION
	PLAN
	PRAGMA
	PRECEDING
	PRIMARY
	QUERY
	RAISE
	RANGE
	RECURSIVE
	REFERENCES
	REINDEX
	RELEASE
	RENAME
	REPLACE
	RESTRICT
	RETURNING
	ROLLBACK
	ROW
	ROWS
	SAVEPOINT
	SELECT
	SET
	TABLE
	TEMP
	THEN
	TIES
	TO
	TRANSACTION
	TRIGGER
	UNBOUNDED
	UNION
	UNIQUE
	UPDATE
	USING
	VACUUM
	VALUES
	VIEW
	VIRTUAL
	WHEN
	WHERE
	WINDOW
	WITH
	WITHOUT
)

var kindNames = map[Kind]string{
	EOF: "EOF", SPACE: "SPACE", COMMENT: "COMMENT", ILLEGAL: "ILLEGAL",
	ID: "ID", STRING: "STRING", INTEGER: "INTEGER", FLOAT: "FLOAT",
	BLOB: "BLOB", VARIABLE: "VARIABLE", QNUMBER: "QNUMBER",
	LP: "LP", RP: "RP", SEMI: "SEMI", COMMA: "COMMA", DOT: "DOT",
	PLUS: "PLUS", MINUS: "MINUS", STAR: "STAR", SLASH: "SLASH", REM: "REM",
	EQ: "EQ", NE: "NE", LT: "LT", LE: "LE", GT: "GT", GE: "GE",
	LSHIFT: "LSHIFT", RSHIFT: "RSHIFT", BITAND: "BITAND", BITOR: "BITOR",
	BITNOT: "BITNOT", CONCAT: "CONCAT", PTR: "PTR",
}

// String returns the SQLite TK_* name of the kind, without the prefix.
func (k Kind) String() string {
	if s, ok := kindNames[k]; ok {
		return s
	}
	if s, ok := keywordName[k]; ok {
		return s
	}
	return "Kind(?)"
}

// Token is one lexical token. Pos and End are byte offsets into the input
// the token was lexed from; Text is the corresponding source slice.
type Token struct {
	Kind Kind   `json:"kind"`
	Text string `json:"text"`
	Pos  int    `json:"pos"`
	End  int    `json:"end"`
}
