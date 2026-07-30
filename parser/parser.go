// Package parser implements a hand-written recursive descent parser for the
// SQLite SQL dialect.
//
// This is the milestone-1 skeleton: the public API and error type are final,
// but parsing is not implemented yet — every input currently returns an
// error, and the corpus harness in parser_test.go tracks implementation
// progress via the todo metadata (see CLAUDE.md for the workflow).
package parser

import (
	"context"
	"fmt"
	"io"

	"github.com/sqlc-dev/meyer/ast"
)

// Error is the error type returned for inputs meyer rejects. Message
// matches SQLite's parser wording (e.g. `near "FROM": syntax error`);
// Offset is the byte offset of the error in the input, or -1 if unknown.
type Error struct {
	Message string
	Offset  int
}

func (e *Error) Error() string { return e.Message }

// Parse reads SQL from r and returns one ast.Stmt per statement.
func Parse(ctx context.Context, r io.Reader) ([]ast.Stmt, error) {
	if _, err := io.ReadAll(r); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("meyer: parser not implemented")
}
