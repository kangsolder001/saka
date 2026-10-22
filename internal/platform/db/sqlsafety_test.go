package db_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSQL_NoStatementIsBuiltFromStrings backs the security review (spec
// §11.3): every statement handed to Exec, Query, QueryRow or SendBatch in
// production code is a string literal or a package constant (what sqlc
// generates), never something assembled at run time. User input can only
// reach the database as a bound parameter.
func TestSQL_NoStatementIsBuiltFromStrings(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	require.NoError(t, err)

	// Reviewed exceptions: statement text built from something other than user
	// input. Keyed by file, with the reason.
	allowed := map[string]string{
		// "SET ROLE " + the configured role, quoted by pgx.Identifier.Sanitize
		// and restricted by config to ^[a-z_][a-z0-9_]{0,62}$.
		"internal/platform/db/db.go": "SET ROLE with a sanitized identifier from config",
	}

	fset := token.NewFileSet()
	type parsed struct {
		path string
		file *ast.File
	}
	var files []parsed
	consts := map[string]map[string]bool{} // directory -> package-level const names
	for _, top := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, top), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			if strings.HasPrefix(rel, "internal/platform/dbtest/") { // test helper: builds CREATE DATABASE names it generated itself
				return nil
			}
			f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			dir := filepath.ToSlash(filepath.Dir(rel))
			if consts[dir] == nil {
				consts[dir] = map[string]bool{}
			}
			for _, decl := range f.Decls {
				if gd, ok := decl.(*ast.GenDecl); ok && gd.Tok == token.CONST {
					for _, spec := range gd.Specs {
						for _, name := range spec.(*ast.ValueSpec).Names {
							consts[dir][name.Name] = true
						}
					}
				}
			}
			files = append(files, parsed{path: rel, file: f})
			return nil
		})
		require.NoError(t, err)
	}

	statementMethods := map[string]bool{"Exec": true, "Query": true, "QueryRow": true, "SendBatch": true}
	var checked int
	var violations []string
	for _, f := range files {
		dir := filepath.ToSlash(filepath.Dir(f.path))
		ast.Inspect(f.file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) < 2 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !statementMethods[sel.Sel.Name] {
				return true
			}
			// Only calls shaped like (ctx, "statement", args...): skip look-alikes such as
			// url.Values.Query() (no args) or Redis commands.
			if ctx, ok := call.Args[0].(*ast.Ident); !ok || !strings.HasPrefix(strings.ToLower(ctx.Name), "ctx") {
				return true
			}
			checked++
			switch stmt := call.Args[1].(type) {
			case *ast.BasicLit:
				if stmt.Kind == token.STRING {
					return true
				}
			case *ast.Ident:
				if consts[dir][stmt.Name] {
					return true
				}
			}
			if _, ok := allowed[f.path]; ok {
				return true
			}
			violations = append(violations, f.path+": "+fset.Position(call.Pos()).String())
			return true
		})
	}

	require.Empty(t, violations, "statements must be literals or constants; pass values as parameters")
	require.Greater(t, checked, 20, "the scan found too few statements: it is probably looking at the wrong files")
}
