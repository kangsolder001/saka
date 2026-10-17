// Package emailtemplates embeds the transactional email templates.
//
// Every email is a pair of files named after the template code:
//
//	<code>.html.tmpl  HTML body (html/template), wrapped by layout.html.tmpl
//	<code>.txt.tmpl   plain text body (text/template), wrapped by layout.txt.tmpl,
//	                  and defines the "subject" block
//
// Data reaches templates as {{.App.Name}} and {{.Data.<key>}} (snake_case keys).
package emailtemplates

import "embed"

// FS holds the template files.
//
//go:embed *.tmpl
var FS embed.FS
