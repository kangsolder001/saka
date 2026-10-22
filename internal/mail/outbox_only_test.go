package mail_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// These tests back the first Email acceptance criterion (spec §12): every
// email goes through the outbox and nothing is sent synchronously in a
// request. They parse the source tree, so a future change that wires a Mailer
// into the API process or sends from a service fails here instead of in
// production.

const modulePath = "github.com/kangsolder001/saka/"

// repoRoot is two levels up from internal/mail.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	return root
}

// sourceFile is a parsed non-test Go file of the repository.
type sourceFile struct {
	// path is relative to the repository root, with forward slashes.
	path string
	file *ast.File
}

func (s sourceFile) dir() string { return filepath.ToSlash(filepath.Dir(s.path)) }

// productionSources parses every non-test Go file under internal/ and cmd/.
func productionSources(t *testing.T) []sourceFile {
	t.Helper()
	root := repoRoot(t)
	fset := token.NewFileSet()
	var files []sourceFile
	for _, top := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, top), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			if strings.Contains(filepath.ToSlash(path), "/mocks/") {
				return nil
			}
			f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			files = append(files, sourceFile{path: filepath.ToSlash(rel), file: f})
			return nil
		})
		require.NoError(t, err)
	}
	require.NotEmpty(t, files, "no source files found: is the test running from internal/mail?")
	return files
}

// importName returns the local name under which file imports the package
// with the given import path, or "" when it does not.
func importName(f *ast.File, importPath string) string {
	for _, imp := range f.Imports {
		if strings.Trim(imp.Path.Value, `"`) != importPath {
			continue
		}
		if imp.Name != nil {
			return imp.Name.Name
		}
		return filepath.Base(importPath)
	}
	return ""
}

// TestMailer_OnlyTheWorkerProcessBuildsOrReferencesADeliveryTransport proves
// the API process cannot send mail: the Mailer interface, its drivers and the
// job handler are referenced by cmd/worker and nothing else outside the mail
// package itself.
func TestMailer_OnlyTheWorkerProcessBuildsOrReferencesADeliveryTransport(t *testing.T) {
	delivery := map[string]bool{
		"Mailer": true, "Message": true, "NewJobHandler": true, "NewSMTPMailer": true,
		"NewLogMailer": true, "NewResendMailer": true, "SMTPMailer": true, "LogMailer": true, "ResendMailer": true,
	}
	var violations []string
	var workerUses int
	for _, src := range productionSources(t) {
		if src.dir() == "internal/mail" {
			continue
		}
		name := importName(src.file, modulePath+"internal/mail")
		if name == "" {
			continue
		}
		ast.Inspect(src.file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != name || !delivery[sel.Sel.Name] {
				return true
			}
			if src.dir() == "cmd/worker" {
				workerUses++
				return true
			}
			violations = append(violations, src.path+": mail."+sel.Sel.Name)
			return true
		})
	}
	sort.Strings(violations)
	require.Empty(t, violations, "only cmd/worker may use a mail delivery transport; services must enqueue with mail.Sender")
	require.Positive(t, workerUses, "the worker no longer builds a mailer: this guard is looking in the wrong place")
}

// TestMailer_SMTPAndHTTPDeliveryStayInsideTheMailPackage keeps the network
// clients that talk to mail providers out of every other package, so a
// handler cannot sneak in a direct send.
func TestMailer_SMTPAndHTTPDeliveryStayInsideTheMailPackage(t *testing.T) {
	var violations []string
	for _, src := range productionSources(t) {
		if src.dir() == "internal/mail" {
			continue
		}
		for _, banned := range []string{"net/smtp", "github.com/wneessen/go-mail", "github.com/resend/resend-go/v2"} {
			if importName(src.file, banned) != "" {
				violations = append(violations, src.path+" imports "+banned)
			}
		}
	}
	require.Empty(t, violations)
}

// TestMailer_SendIsOnlyCalledFromTheOutboxJobHandler checks the one place in
// the mail package that calls Mailer.Send: JobHandler.Handle, which the
// outbox worker invokes. Service.Send (what other modules call) only enqueues.
func TestMailer_SendIsOnlyCalledFromTheOutboxJobHandler(t *testing.T) {
	var callers []string
	for _, src := range productionSources(t) {
		if src.dir() != "internal/mail" {
			continue
		}
		for _, decl := range src.file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			owner := fn.Name.Name
			if fn.Recv != nil && len(fn.Recv.List) == 1 {
				owner = receiverName(fn.Recv.List[0].Type) + "." + owner
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "Send" {
					return true
				}
				// h.mailer.Send(...) is the transport; s.Mail.Send or s.enqueuer.* are not.
				if inner, ok := sel.X.(*ast.SelectorExpr); ok && inner.Sel.Name == "mailer" {
					callers = append(callers, owner)
				}
				return true
			})
		}
	}
	require.Equal(t, []string{"JobHandler.Handle"}, callers)
}

func receiverName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.StarExpr:
		return receiverName(e.X)
	case *ast.Ident:
		return e.Name
	default:
		return ""
	}
}
