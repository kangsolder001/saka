package mail

import (
	"bytes"
	"errors"
	"fmt"
	htmltemplate "html/template"
	"io/fs"
	"sort"
	"strings"
	texttemplate "text/template"
)

const (
	htmlSuffix = ".html.tmpl"
	textSuffix = ".txt.tmpl"
	layoutName = "layout"
)

// AppInfo is the product information every template can use as {{.App.*}}.
type AppInfo struct {
	Name string
}

// Rendered is the output of rendering one template.
type Rendered struct {
	Subject string
	HTML    string
	Text    string
}

type compiled struct {
	html *htmltemplate.Template
	text *texttemplate.Template
}

// Renderer turns a template code plus data into subject, HTML and text. All
// templates are parsed once at startup so a broken template stops the
// process at boot instead of failing jobs later.
type Renderer struct {
	app       AppInfo
	templates map[string]compiled
}

// NewRenderer parses every template found in fsys. Each <code>.html.tmpl
// needs a matching <code>.txt.tmpl that defines the "subject" block, and
// layout.html.tmpl / layout.txt.tmpl wrap the content of every email.
func NewRenderer(fsys fs.FS, app AppInfo) (*Renderer, error) {
	names, err := templateNames(fsys)
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, errors.New("no email templates found")
	}

	r := &Renderer{app: app, templates: make(map[string]compiled, len(names))}
	for _, name := range names {
		h, err := htmltemplate.New(name).Option("missingkey=error").
			ParseFS(fsys, layoutName+htmlSuffix, name+htmlSuffix)
		if err != nil {
			return nil, fmt.Errorf("parse html template %q: %w", name, err)
		}
		t, err := texttemplate.New(name).Option("missingkey=error").
			ParseFS(fsys, layoutName+textSuffix, name+textSuffix)
		if err != nil {
			return nil, fmt.Errorf("parse text template %q: %w", name, err)
		}
		if t.Lookup("subject") == nil {
			return nil, fmt.Errorf("text template %q must define a \"subject\" block", name)
		}
		r.templates[name] = compiled{html: h, text: t}
	}
	return r, nil
}

// templateNames lists template codes (every <code>.html.tmpl except layout)
// and checks that each has a text counterpart.
func templateNames(fsys fs.FS) ([]string, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("read templates: %w", err)
	}
	var names []string
	for _, e := range entries {
		file := e.Name()
		if !strings.HasSuffix(file, htmlSuffix) {
			continue
		}
		name := strings.TrimSuffix(file, htmlSuffix)
		if name == layoutName {
			continue
		}
		if _, err := fs.Stat(fsys, name+textSuffix); err != nil {
			return nil, fmt.Errorf("template %q has no %s%s counterpart", name, name, textSuffix)
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// Has reports whether a template with this code exists.
func (r *Renderer) Has(name string) bool {
	_, ok := r.templates[name]
	return ok
}

// Names returns the available template codes, sorted.
func (r *Renderer) Names() []string {
	names := make([]string, 0, len(r.templates))
	for name := range r.templates {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ErrUnknownTemplate is returned for a template code that does not exist.
var ErrUnknownTemplate = errors.New("unknown email template")

// Render executes the template. Referencing a key missing from data is an
// error (missingkey=error), so typos and forgotten values surface in tests.
func (r *Renderer) Render(name string, data map[string]any) (Rendered, error) {
	c, ok := r.templates[name]
	if !ok {
		return Rendered{}, fmt.Errorf("%w: %q", ErrUnknownTemplate, name)
	}
	view := struct {
		App  AppInfo
		Data map[string]any
	}{App: r.app, Data: data}

	var html, text, subject bytes.Buffer
	if err := c.html.ExecuteTemplate(&html, layoutName, view); err != nil {
		return Rendered{}, fmt.Errorf("render html %q: %w", name, err)
	}
	if err := c.text.ExecuteTemplate(&text, layoutName, view); err != nil {
		return Rendered{}, fmt.Errorf("render text %q: %w", name, err)
	}
	if err := c.text.ExecuteTemplate(&subject, "subject", view); err != nil {
		return Rendered{}, fmt.Errorf("render subject %q: %w", name, err)
	}

	return Rendered{
		// Collapse any whitespace (including line breaks) so a value can never
		// break out of the Subject header.
		Subject: strings.Join(strings.Fields(subject.String()), " "),
		HTML:    html.String(),
		Text:    strings.TrimSpace(text.String()) + "\n",
	}, nil
}
