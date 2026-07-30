package token

// keywords maps the upper-case spelling of every SQLite keyword to its token
// kind. Transcribed from aKeywordTable[] in tool/mkkeywordhash.c, resolved
// for the feature set of the pinned build (see parser/testdata/README.md):
// every keyword whose mask is enabled by default is present. WITHIN is
// omitted because it is gated on SQLITE_ENABLE_ORDERED_SET_AGGREGATES, which
// the pinned oracle build does not define.
var keywords = map[string]Kind{
	"ABORT":             ABORT,
	"ACTION":            ACTION,
	"ADD":               ADD,
	"AFTER":             AFTER,
	"ALL":               ALL,
	"ALTER":             ALTER,
	"ALWAYS":            ALWAYS,
	"ANALYZE":           ANALYZE,
	"AND":               AND,
	"AS":                AS,
	"ASC":               ASC,
	"ATTACH":            ATTACH,
	"AUTOINCREMENT":     AUTOINCR,
	"BEFORE":            BEFORE,
	"BEGIN":             BEGIN,
	"BETWEEN":           BETWEEN,
	"BY":                BY,
	"CASCADE":           CASCADE,
	"CASE":              CASE,
	"CAST":              CAST,
	"CHECK":             CHECK,
	"COLLATE":           COLLATE,
	"COLUMN":            COLUMNKW,
	"COMMIT":            COMMIT,
	"CONFLICT":          CONFLICT,
	"CONSTRAINT":        CONSTRAINT,
	"CREATE":            CREATE,
	"CROSS":             JOIN_KW,
	"CURRENT":           CURRENT,
	"CURRENT_DATE":      CTIME_KW,
	"CURRENT_TIME":      CTIME_KW,
	"CURRENT_TIMESTAMP": CTIME_KW,
	"DATABASE":          DATABASE,
	"DEFAULT":           DEFAULT,
	"DEFERRED":          DEFERRED,
	"DEFERRABLE":        DEFERRABLE,
	"DELETE":            DELETE,
	"DESC":              DESC,
	"DETACH":            DETACH,
	"DISTINCT":          DISTINCT,
	"DO":                DO,
	"DROP":              DROP,
	"END":               END,
	"EACH":              EACH,
	"ELSE":              ELSE,
	"ESCAPE":            ESCAPE,
	"EXCEPT":            EXCEPT,
	"EXCLUSIVE":         EXCLUSIVE,
	"EXCLUDE":           EXCLUDE,
	"EXISTS":            EXISTS,
	"EXPLAIN":           EXPLAIN,
	"FAIL":              FAIL,
	"FILTER":            FILTER,
	"FIRST":             FIRST,
	"FOLLOWING":         FOLLOWING,
	"FOR":               FOR,
	"FOREIGN":           FOREIGN,
	"FROM":              FROM,
	"FULL":              JOIN_KW,
	"GENERATED":         GENERATED,
	"GLOB":              LIKE_KW,
	"GROUP":             GROUP,
	"GROUPS":            GROUPS,
	"HAVING":            HAVING,
	"IF":                IF,
	"IGNORE":            IGNORE,
	"IMMEDIATE":         IMMEDIATE,
	"IN":                IN,
	"INDEX":             INDEX,
	"INDEXED":           INDEXED,
	"INITIALLY":         INITIALLY,
	"INNER":             JOIN_KW,
	"INSERT":            INSERT,
	"INSTEAD":           INSTEAD,
	"INTERSECT":         INTERSECT,
	"INTO":              INTO,
	"IS":                IS,
	"ISNULL":            ISNULL,
	"JOIN":              JOIN,
	"KEY":               KEY,
	"LAST":              LAST,
	"LEFT":              JOIN_KW,
	"LIKE":              LIKE_KW,
	"LIMIT":             LIMIT,
	"MATCH":             MATCH,
	"MATERIALIZED":      MATERIALIZED,
	"NATURAL":           JOIN_KW,
	"NO":                NO,
	"NOT":               NOT,
	"NOTHING":           NOTHING,
	"NOTNULL":           NOTNULL,
	"NULL":              NULL,
	"NULLS":             NULLS,
	"OF":                OF,
	"OFFSET":            OFFSET,
	"ON":                ON,
	"OR":                OR,
	"ORDER":             ORDER,
	"OTHERS":            OTHERS,
	"OUTER":             JOIN_KW,
	"OVER":              OVER,
	"PARTITION":         PARTITION,
	"PLAN":              PLAN,
	"PRAGMA":            PRAGMA,
	"PRECEDING":         PRECEDING,
	"PRIMARY":           PRIMARY,
	"QUERY":             QUERY,
	"RAISE":             RAISE,
	"RANGE":             RANGE,
	"RECURSIVE":         RECURSIVE,
	"REFERENCES":        REFERENCES,
	"REGEXP":            LIKE_KW,
	"REINDEX":           REINDEX,
	"RELEASE":           RELEASE,
	"RENAME":            RENAME,
	"REPLACE":           REPLACE,
	"RESTRICT":          RESTRICT,
	"RETURNING":         RETURNING,
	"RIGHT":             JOIN_KW,
	"ROLLBACK":          ROLLBACK,
	"ROW":               ROW,
	"ROWS":              ROWS,
	"SAVEPOINT":         SAVEPOINT,
	"SELECT":            SELECT,
	"SET":               SET,
	"TABLE":             TABLE,
	"TEMP":              TEMP,
	"TEMPORARY":         TEMP,
	"THEN":              THEN,
	"TIES":              TIES,
	"TO":                TO,
	"TRANSACTION":       TRANSACTION,
	"TRIGGER":           TRIGGER,
	"UNBOUNDED":         UNBOUNDED,
	"UNION":             UNION,
	"UNIQUE":            UNIQUE,
	"UPDATE":            UPDATE,
	"USING":             USING,
	"VACUUM":            VACUUM,
	"VALUES":            VALUES,
	"VIEW":              VIEW,
	"VIRTUAL":           VIRTUAL,
	"WHEN":              WHEN,
	"WHERE":             WHERE,
	"WINDOW":            WINDOW,
	"WITH":              WITH,
	"WITHOUT":           WITHOUT,
}

// keywordName is the reverse of keywords, used by Kind.String. Kinds shared
// by several spellings (JOIN_KW, LIKE_KW, CTIME_KW, TEMP) keep the SQLite
// token name rather than one arbitrary spelling.
var keywordName = func() map[Kind]string {
	m := map[Kind]string{
		JOIN_KW: "JOIN_KW", LIKE_KW: "LIKE_KW", CTIME_KW: "CTIME_KW",
		TEMP: "TEMP", AUTOINCR: "AUTOINCR", COLUMNKW: "COLUMNKW",
	}
	for name, k := range keywords {
		if _, taken := m[k]; !taken {
			m[k] = name
		}
	}
	return m
}()

// fallback is SQLite's "%fallback ID" set from parse.y: keywords that the
// LALR parser re-reads as plain identifiers whenever no shift action exists
// for the keyword itself. Resolved for the pinned build's feature set:
// SQLITE_OMIT_COMPOUND_SELECT is off (so EXCEPT/INTERSECT/UNION do not fall
// back), SQLITE_OMIT_WINDOWFUNC is off (so the frame keywords do),
// SQLITE_OMIT_GENERATED_COLUMNS is off, and
// SQLITE_ENABLE_ORDERED_SET_AGGREGATES is off (WITHIN is not a keyword).
//
// WINDOW, OVER and FILTER are deliberately absent: their keyword-versus-
// identifier ambiguity is resolved in the tokenizer instead (see lexer).
var fallback = map[Kind]bool{
	ABORT: true, ACTION: true, AFTER: true, ANALYZE: true, ASC: true,
	ATTACH: true, BEFORE: true, BEGIN: true, BY: true, CASCADE: true,
	CAST: true, COLUMNKW: true, CONFLICT: true, DATABASE: true,
	DEFERRED: true, DESC: true, DETACH: true, DO: true, EACH: true,
	END: true, EXCLUSIVE: true, EXPLAIN: true, FAIL: true, FOR: true,
	IGNORE: true, IMMEDIATE: true, INITIALLY: true, INSTEAD: true,
	LIKE_KW: true, MATCH: true, NO: true, PLAN: true, QUERY: true,
	KEY: true, OF: true, OFFSET: true, PRAGMA: true, RAISE: true,
	RECURSIVE: true, RELEASE: true, REPLACE: true, RESTRICT: true,
	ROW: true, ROWS: true, ROLLBACK: true, SAVEPOINT: true, TEMP: true,
	TRIGGER: true, VACUUM: true, VIEW: true, VIRTUAL: true, WITH: true,
	WITHOUT: true, NULLS: true, FIRST: true, LAST: true,
	CURRENT: true, FOLLOWING: true, PARTITION: true, PRECEDING: true,
	RANGE: true, UNBOUNDED: true, EXCLUDE: true, GROUPS: true,
	OTHERS: true, TIES: true, GENERATED: true, ALWAYS: true,
	MATERIALIZED: true, REINDEX: true, RENAME: true, CTIME_KW: true,
	IF: true,
}

// maxKeywordLen is the length of CURRENT_TIMESTAMP, the longest keyword.
const maxKeywordLen = 17

// Lookup returns the keyword kind for an identifier spelling, or ID if it is
// not a keyword. Matching is ASCII case-insensitive, as in keywordCode().
//
// The upper-casing is done into a stack array rather than with
// strings.ToUpper, because this runs once per identifier token and the
// compiler turns m[string(b[:n])] into an allocation-free lookup.
func Lookup(name string) Kind {
	if len(name) > maxKeywordLen {
		return ID
	}
	var buf [maxKeywordLen]byte
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}
		buf[i] = c
	}
	if k, ok := keywords[string(buf[:len(name)])]; ok {
		return k
	}
	return ID
}

// CanFallback reports whether k is in SQLite's %fallback ID set.
func CanFallback(k Kind) bool { return fallback[k] }

// The token classes declared in parse.y. Each corresponds to a
// %token_class directive, extended with the %fallback set: a keyword that
// falls back to ID satisfies any class containing ID.
//
//	%token_class id   ID|INDEXED.
//	%token_class ids  ID|STRING.
//	%token_class idj  ID|INDEXED|JOIN_KW.
//	nm ::= idj. / nm ::= STRING.

// IsPlainID reports whether k is TK_ID, or a keyword that becomes one
// through %fallback. It is not one of parse.y's token classes: it is the
// bare ID terminal, which a few rules use directly.
//
//	generated ::= LP expr RP ID.
func IsPlainID(k Kind) bool { return k == ID || fallback[k] }

// IsID reports whether k satisfies the "id" class (ID|INDEXED).
func IsID(k Kind) bool { return k == ID || k == INDEXED || fallback[k] }

// IsIDS reports whether k satisfies the "ids" class (ID|STRING).
func IsIDS(k Kind) bool { return k == ID || k == STRING || fallback[k] }

// IsIDJ reports whether k satisfies the "idj" class (ID|INDEXED|JOIN_KW).
func IsIDJ(k Kind) bool {
	return k == ID || k == INDEXED || k == JOIN_KW || fallback[k]
}

// IsName reports whether k satisfies the "nm" production (idj|STRING), the
// name production used for tables, columns, indexes and the like.
func IsName(k Kind) bool { return IsIDJ(k) || k == STRING }
