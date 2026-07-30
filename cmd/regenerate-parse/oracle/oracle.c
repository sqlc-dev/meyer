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
**   stmt <offset> err <rc> <erroff> <message to end of line>
**
** <offset>  byte offset of the statement within the input script
** <rc>      sqlite3_prepare_v2 result code
** <erroff>  sqlite3_error_offset relative to the whole script (-1 if unknown)
**
** SQLite error messages never contain newlines, so the line format is safe.
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

/* True if the range [z, z+n) holds nothing but whitespace and SQL comments —
** i.e. preparing it would yield no statement. */
static int onlyWhitespace(const char *z, size_t n){
  size_t i = 0;
  while( i<n ){
    char c = z[i];
    if( c==' ' || c=='\t' || c=='\n' || c=='\r' || c=='\f' || c=='\v' ){
      i++;
    }else if( c=='-' && i+1<n && z[i+1]=='-' ){
      while( i<n && z[i]!='\n' ) i++;
    }else if( c=='/' && i+1<n && z[i+1]=='*' ){
      i += 2;
      while( i+1<n && !(z[i]=='*' && z[i+1]=='/') ) i++;
      i = (i+1<n) ? i+2 : n;
    }else{
      return 0;
    }
  }
  return 1;
}

int main(int argc, char **argv){
  FILE *in = stdin;
  if( argc>1 ){
    in = fopen(argv[1], "rb");
    if( in==0 ){ fprintf(stderr, "cannot open %s\n", argv[1]); return 2; }
  }
  size_t n = 0;
  char *sql = readAll(in, &n);
  if( sql==0 ){ fprintf(stderr, "out of memory\n"); return 2; }

  sqlite3 *db = 0;
  if( sqlite3_open(":memory:", &db)!=SQLITE_OK ){
    fprintf(stderr, "cannot open :memory:\n");
    return 2;
  }

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
    if( onlyWhitespace(sql+pos, len) ){ pos = end; continue; }

    char *stmtText = malloc(len+1);
    if( stmtText==0 ){ fprintf(stderr, "out of memory\n"); return 2; }
    memcpy(stmtText, sql+pos, len);
    stmtText[len] = 0;

    sqlite3_stmt *pStmt = 0;
    int rc = sqlite3_prepare_v2(db, stmtText, (int)len, &pStmt, 0);
    if( rc==SQLITE_OK ){
      printf("stmt %zu ok\n", pos);
    }else{
      int erroff = sqlite3_error_offset(db);
      if( erroff>=0 ) erroff += (int)pos;
      printf("stmt %zu err %d %d %s\n", pos, rc, erroff, sqlite3_errmsg(db));
    }
    sqlite3_finalize(pStmt);
    free(stmtText);
    pos = end;
  }

  sqlite3_close(db);
  free(sql);
  return 0;
}
