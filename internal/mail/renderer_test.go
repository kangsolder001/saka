package mail_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/mail"
	emailtemplates "github.com/kangsolder001/saka/templates/email"
)

var app = mail.AppInfo{Name: "Saka"}

func realRenderer(t *testing.T) *mail.Renderer {
	t.Helper()
	r, err := mail.NewRenderer(emailtemplates.FS, app)
	require.NoError(t, err)
	return r
}

func TestRealTemplates_AllRenderWithTheirData(t *testing.T) {
	r := realRenderer(t)
	data := map[string]map[string]any{
		"verify_email":     {"name": "Ann", "url": "https://app.example/verify?token=abc123"},
		"password_reset":   {"name": "Ann", "url": "https://app.example/reset?token=xyz789"},
		"password_changed": {"name": "Ann"},

		"email_change_confirm": {"name": "Ann", "url": "https://app.example/confirm-email-change?token=new123"},
		"email_changed_notice": {"name": "Ann", "new_email": "ann.new@example.com"},

		"invitation": {
			"inviter_name": "Ann", "organization_name": "Acme Corp", "role_label": "Admin", "expires_in_days": 7,
			"url": "https://app.example/accept-invitation?token=inv123",
		},

		"invoice_created": {
			"name": "Ann", "organization_name": "Acme Corp", "invoice_number": "INV-2026-000123", "plan_name": "Pro",
			"amount": "Rp99.000", "due_date": "8 Juni 2026", "payment_url": "https://pay.example/INV-2026-000123",
		},
		"payment_succeeded": {
			"name": "Ann", "organization_name": "Acme Corp", "invoice_number": "INV-2026-000123", "plan_name": "Pro",
			"amount": "Rp99.000", "period_end": "1 Juli 2026",
		},
	}
	require.ElementsMatch(t, []string{
		"verify_email", "password_reset", "password_changed", "email_change_confirm", "email_changed_notice", "invitation",
		"invoice_created", "payment_succeeded",
	}, r.Names(),
		"update this test when a template is added so it is rendered here")

	for name, d := range data {
		t.Run(name, func(t *testing.T) {
			out, err := r.Render(name, d)

			require.NoError(t, err)
			assert.NotEmpty(t, out.Subject)
			assert.NotContains(t, out.Subject, "\n")
			assert.Contains(t, out.HTML, "<html")
			if _, addressed := d["name"]; addressed {
				assert.Contains(t, out.HTML, "Halo Ann")
				assert.Contains(t, out.Text, "Halo Ann")
			}
			if org, ok := d["organization_name"].(string); ok {
				assert.Contains(t, out.HTML, org)
				assert.Contains(t, out.Text, org)
			}
			if _, ok := d["inviter_name"]; ok {
				assert.Contains(t, out.Subject, d["organization_name"])
				assert.Contains(t, out.Text, "Ann mengundang Anda")
			}
			for _, key := range []string{"invoice_number", "plan_name", "amount", "due_date", "period_end"} {
				if v, ok := d[key].(string); ok {
					assert.Contains(t, out.HTML, v, key)
					assert.Contains(t, out.Text, v, key)
				}
			}
			if url, ok := d["payment_url"].(string); ok {
				assert.Contains(t, out.HTML, url)
				assert.Contains(t, out.Text, url)
			}
			assert.NotContains(t, out.Text, "<p>", "text version must not contain HTML")
			assert.Contains(t, out.Text, "Saka")
			if newEmail, ok := d["new_email"].(string); ok {
				assert.Contains(t, out.HTML, newEmail)
				assert.Contains(t, out.Text, newEmail)
			}
			if url, ok := d["url"].(string); ok {
				assert.Contains(t, out.HTML, url)
				assert.Contains(t, out.Text, url)
			}
		})
	}
}

func TestRealTemplates_MissingDataIsAnError(t *testing.T) {
	r := realRenderer(t)

	_, err := r.Render("verify_email", map[string]any{"name": "Ann"}) // url missing

	require.Error(t, err)
}

func TestRender_HTMLIsEscapedButTextIsNot(t *testing.T) {
	r := realRenderer(t)

	out, err := r.Render("password_changed", map[string]any{"name": `<script>alert(1)</script> & "Bob"`})

	require.NoError(t, err)
	assert.NotContains(t, out.HTML, "<script>")
	assert.Contains(t, out.HTML, "&lt;script&gt;")
	assert.Contains(t, out.Text, `<script>alert(1)</script> & "Bob"`, "plain text is not HTML-escaped")
}

func TestRender_DangerousURLSchemeIsNeutralisedInHTML(t *testing.T) {
	r := realRenderer(t)

	out, err := r.Render("verify_email", map[string]any{"name": "Ann", "url": "javascript:alert(1)"})

	require.NoError(t, err)
	assert.NotContains(t, out.HTML, `href="javascript:`)
}

func TestRender_SubjectCannotBeSplitIntoHeaders(t *testing.T) {
	// {{.App.Name}} is in the subject; a hostile app name must not add lines.
	hostile, err := mail.NewRenderer(emailtemplates.FS, mail.AppInfo{Name: "Saka\r\nBcc: evil@example.com"})
	require.NoError(t, err)

	out, err := hostile.Render("password_changed", map[string]any{"name": "Ann"})

	require.NoError(t, err)
	assert.NotContains(t, out.Subject, "\n")
	assert.NotContains(t, out.Subject, "\r")
}

func TestRender_UnknownTemplate(t *testing.T) {
	r := realRenderer(t)

	_, err := r.Render("nope", nil)

	assert.ErrorIs(t, err, mail.ErrUnknownTemplate)
	assert.False(t, r.Has("nope"))
	assert.True(t, r.Has("verify_email"))
}

func TestRender_TextOutputIsTidy(t *testing.T) {
	r := realRenderer(t)

	out, err := r.Render("password_changed", map[string]any{"name": "Ann"})

	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(out.Text, "\n"))
	assert.False(t, strings.HasSuffix(out.Text, "\n\n"))
	assert.False(t, strings.HasPrefix(out.Text, "\n"))
}

func fakeFS(files map[string]string) fstest.MapFS {
	m := fstest.MapFS{}
	for name, content := range files {
		m[name] = &fstest.MapFile{Data: []byte(content)}
	}
	return m
}

const (
	okLayoutHTML = `{{define "layout"}}<html>{{template "content" .}}</html>{{end}}`
	okLayoutText = `{{define "layout"}}{{template "content" .}}{{end}}`
)

func TestNewRenderer_FailsFastOnBrokenTemplates(t *testing.T) {
	tests := map[string]map[string]string{
		"no templates": {
			"layout.html.tmpl": okLayoutHTML, "layout.txt.tmpl": okLayoutText,
		},
		"missing text counterpart": {
			"layout.html.tmpl": okLayoutHTML, "layout.txt.tmpl": okLayoutText,
			"a.html.tmpl": `{{define "content"}}x{{end}}{{template "layout" .}}`,
		},
		"missing subject": {
			"layout.html.tmpl": okLayoutHTML, "layout.txt.tmpl": okLayoutText,
			"a.html.tmpl": `{{define "content"}}x{{end}}{{template "layout" .}}`,
			"a.txt.tmpl":  `{{define "content"}}x{{end}}{{template "layout" .}}`,
		},
		"syntax error": {
			"layout.html.tmpl": okLayoutHTML, "layout.txt.tmpl": okLayoutText,
			"a.html.tmpl": `{{define "content"}}{{.Data.x{{end}}`,
			"a.txt.tmpl":  `{{define "subject"}}s{{end}}{{define "content"}}x{{end}}{{template "layout" .}}`,
		},
		"missing layout": {
			"a.html.tmpl": `{{define "content"}}x{{end}}`,
			"a.txt.tmpl":  `{{define "subject"}}s{{end}}{{define "content"}}x{{end}}`,
		},
	}

	for name, files := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := mail.NewRenderer(fakeFS(files), app)

			assert.Error(t, err)
		})
	}
}

func TestNewRenderer_AcceptsMinimalValidSet(t *testing.T) {
	r, err := mail.NewRenderer(fakeFS(map[string]string{
		"layout.html.tmpl": okLayoutHTML, "layout.txt.tmpl": okLayoutText,
		"a.html.tmpl": `{{define "content"}}<b>{{.Data.x}}</b>{{end}}{{template "layout" .}}`,
		"a.txt.tmpl":  `{{define "subject"}}Subject {{.Data.x}}{{end}}{{define "content"}}{{.Data.x}}{{end}}{{template "layout" .}}`,
	}), app)
	require.NoError(t, err)

	out, err := r.Render("a", map[string]any{"x": "1"})

	require.NoError(t, err)
	assert.Equal(t, "Subject 1", out.Subject)
	assert.Equal(t, "<html><b>1</b></html>", out.HTML)
	assert.Equal(t, "1\n", out.Text)
}
