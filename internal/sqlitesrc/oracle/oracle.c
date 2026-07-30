/*
** oracle.c — parse-classification oracle for the meyer test corpus.
**
** Reads a SQL script from the file named in argv[1] (or stdin), splits it
** into statements the same way the sqlite3 shell does (incremental
** sqlite3_complete checks at each candidate ";"), and prepares every
** statement independently against an empty in-memory database with
** sqlite3_prepare_v2. Nothing is ever stepped/executed, so results do not
** depend on earlier statements having run.
**
** Output: one line per non-empty statement:
**
**   stmt <offset> ok
**   stmt <offset> ok <tail>
**   stmt <offset> err <rc> <erroff> <tail> <message to end of line>
**
** <offset>  byte offset of the statement within the input script
** <rc>      sqlite3_prepare_v2 result code
** <erroff>  sqlite3_error_offset relative to the whole script (-1 if unknown)
** <tail>    pzTail relative to the whole script: how far the parser got
**
** <tail> matters because SQLite can stop short of the end of a statement,
** in which case the text past it was never parsed and the line says nothing
** about it. Two ways in:
**
**   - a grammar action fails in the middle -- sqlite3BeginTrigger raising
**     "no such table" at the trigger_decl reduce, say -- and
**     sqlite3RunParser abandons the rest of the statement;
**
**   - the split above handed prepare more than one statement, because
**     sqlite3_complete's scanner is cruder than the tokenizer and missed
**     the boundary. prepare then parses the first and leaves the rest.
**
** The second is why "ok" lines can carry a <tail> too. Most do not: a
** prepare that consumed everything it was given omits the field.
**
** SQLite error messages never contain newlines, so the line format is safe.
** (An error message can still span lines when the offending token does --
** an unterminated string, say -- because the token text is interpolated.)
**
** With -batch, scripts are read from stdin as a stream of records, each a
** decimal byte count on its own line followed by exactly that many bytes.
** The results of each record are followed by a line reading "==". This
** exists so that a caller checking many thousands of small inputs does not
** pay to start a process for each one.
**
** Each record still gets a brand new connection. Preparing a statement is
** not supposed to change anything, but sqlite3Pragma runs at prepare time,
** so a "PRAGMA writable_schema=ON" in one script would otherwise decide
** whether a later one may write to sqlite_master. Reusing the connection
** also perturbs sqlite3_error_offset. Opening :memory: is cheap next to
** starting a process, which is the cost this mode exists to avoid. A new
** connection is not enough on its own -- see resetGlobals.
*/
#include "sqlite3.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static char *readAll(FILE *in, size_t *pn){
  size_t cap = 1 << 16, n = 0;
  char *buf = malloc(cap);
  if( buf==0 ) return 0;
  for(;;){
    if( n==cap ){
      cap *= 2;
      char *nb = realloc(buf, cap);
      if( nb==0 ){ free(buf); return 0; }
      buf = nb;
    }
    size_t got = fread(buf+n, 1, cap-n, in);
    n += got;
    if( got==0 ) break;
  }
  buf[n>=cap ? cap-1 : n] = 0;
  *pn = n;
  return buf;
}

/* Classify every statement of one script, writing the result lines to
** stdout. sql must be NUL terminated at sql[n]. */
static void runScript(sqlite3 *db, char *sql, size_t n){
  size_t pos = 0;
  while( pos<n ){
    /* Find the end of the next statement: the first ";" at which the prefix
    ** is a complete statement per sqlite3_complete (the shell's algorithm).
    ** If no such ";" exists, the remainder of the input is one statement. */
    size_t end = n;                 /* one past the last byte of the stmt */
    for(size_t i=pos; i<n; i++){
      if( sql[i]==';' ){
        char saved = sql[i+1];
        sql[i+1] = 0;
        int complete = sqlite3_complete(sql+pos);
        sql[i+1] = saved;
        if( complete ){ end = i+1; break; }
      }
    }
    size_t len = end-pos;
    char *stmtText = malloc(len+1);
    if( stmtText==0 ){ fprintf(stderr, "out of memory\n"); exit(2); }
    memcpy(stmtText, sql+pos, len);
    stmtText[len] = 0;

    sqlite3_stmt *pStmt = 0;
    const char *zTail = 0;
    int rc = sqlite3_prepare_v2(db, stmtText, (int)len, &pStmt, &zTail);
    if( rc==SQLITE_OK && pStmt==0 ){
      /* Nothing but whitespace and comments: no statement, nothing to say.
      ** Asking prepare rather than scanning for whitespace here is the
      ** point -- deciding "is this a statement?" with a second, cruder
      ** tokenizer gets the quirks wrong, and every one of them shows up as
      ** a phantom disagreement. A lone vertical tab is CC_ILLEGAL, not
      ** space; a "/*" with nothing after the star is TK_SLASH TK_STAR, not
      ** an unterminated comment. */
    }else if( rc==SQLITE_OK ){
      size_t tail = zTail ? (size_t)(zTail-stmtText) : len;
      if( tail>=len ){
        printf("stmt %zu ok\n", pos);
      }else{
        printf("stmt %zu ok %zu\n", pos, pos+tail);
      }
    }else{
      int erroff = sqlite3_error_offset(db);
      size_t tail = zTail ? (size_t)(zTail-stmtText) : len;
      if( erroff>=0 ) erroff += (int)pos;
      if( tail>len ) tail = len;
      printf("stmt %zu err %d %d %zu %s\n", pos, rc, erroff, pos+tail,
             sqlite3_errmsg(db));
    }
    sqlite3_finalize(pStmt);
    free(stmtText);
    pos = end;
  }
}

/* Undo the process-global state a prepared PRAGMA can leave behind.
**
** Most pragmas only touch their own connection, but the two heap limits are
** process-wide, and sqlite3Pragma applies them while preparing. One
** "PRAGMA hard_heap_limit=6874" in an input therefore starves every later
** connection in the process: sqlite3_open starts returning SQLITE_NOMEM
** with a flat heap. SQLite's own fuzz corpus contains exactly that, and it
** took out the whole batch run. 0 means "no limit". */
static void resetGlobals(void){
  sqlite3_hard_heap_limit64(0);
  sqlite3_soft_heap_limit64(0);
}

/* Write the fuzz inputs held in zPath's "xsql" table to stdout, framed the
** way -batch expects them. Used to seed differential testing with SQLite's
** own historical parser crashers. */
static int dumpSeeds(const char *zPath){
  sqlite3 *db = 0;
  sqlite3_stmt *pStmt = 0;
  int rc = sqlite3_open_v2(zPath, &db, SQLITE_OPEN_READONLY, 0);
  if( rc!=SQLITE_OK ){
    fprintf(stderr, "cannot open %s: %s\n", zPath, sqlite3_errmsg(db));
    sqlite3_close(db);
    return 2;
  }
  rc = sqlite3_prepare_v2(db, "SELECT sqltext FROM xsql", -1, &pStmt, 0);
  if( rc!=SQLITE_OK ){
    fprintf(stderr, "%s: %s\n", zPath, sqlite3_errmsg(db));
    sqlite3_close(db);
    return 2;
  }
  while( sqlite3_step(pStmt)==SQLITE_ROW ){
    const void *pText = sqlite3_column_blob(pStmt, 0);
    int n = sqlite3_column_bytes(pStmt, 0);
    if( pText==0 ) continue;
    printf("%d\n", n);
    fwrite(pText, 1, (size_t)n, stdout);
  }
  sqlite3_finalize(pStmt);
  sqlite3_close(db);
  return 0;
}

int main(int argc, char **argv){
  sqlite3 *db = 0;

  if( argc>2 && strcmp(argv[1], "-seeds")==0 ){
    return dumpSeeds(argv[2]);
  }

  if( argc>1 && strcmp(argv[1], "-batch")==0 ){
    char line[64];
    while( fgets(line, sizeof(line), stdin) ){
      long want = strtol(line, 0, 10);
      if( want<0 ){ fprintf(stderr, "bad record length\n"); return 2; }
      char *sql = malloc((size_t)want+1);
      if( sql==0 ){ fprintf(stderr, "out of memory\n"); return 2; }
      if( want>0 && fread(sql, 1, (size_t)want, stdin)!=(size_t)want ){
        fprintf(stderr, "short record\n");
        return 2;
      }
      sql[want] = 0;
      if( sqlite3_open(":memory:", &db)!=SQLITE_OK ){
        fprintf(stderr, "cannot open :memory:\n");
        return 2;
      }
      runScript(db, sql, (size_t)want);
      resetGlobals();
      sqlite3_close(db);
      printf("==\n");
      fflush(stdout);
      free(sql);
    }
    return 0;
  }

  if( sqlite3_open(":memory:", &db)!=SQLITE_OK ){
    fprintf(stderr, "cannot open :memory:\n");
    return 2;
  }

  FILE *in = stdin;
  if( argc>1 ){
    in = fopen(argv[1], "rb");
    if( in==0 ){ fprintf(stderr, "cannot open %s\n", argv[1]); return 2; }
  }
  size_t n = 0;
  char *sql = readAll(in, &n);
  if( sql==0 ){ fprintf(stderr, "out of memory\n"); return 2; }
  runScript(db, sql, n);
  sqlite3_close(db);
  free(sql);
  return 0;
}
