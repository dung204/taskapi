package email

import (
	"bytes"
	"fmt"
	htmltmpl "html/template"
	"io/fs"
	"strings"
	texttmpl "text/template"
)

// Templates holds parsed email bodies, keyed by template name: the file name
// without its .html.tmpl, .text.tmpl or .tmpl suffix.
type Templates struct {
	HTML map[string]*htmltmpl.Template
	Text map[string]*texttmpl.Template
}

// LoadTemplates parses the template files at the root of fsys. A file ending
// in .html.tmpl becomes an HTML template; one ending in .text.tmpl or plain
// .tmpl becomes a text template. Other files and directories are ignored.
//
// Returns an error if fsys cannot be read or a template does not parse.
func LoadTemplates(fsys fs.FS) (Templates, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return Templates{}, fmt.Errorf("load templates: read dir: %w", err)
	}

	tmpls := Templates{
		HTML: make(map[string]*htmltmpl.Template),
		Text: make(map[string]*texttmpl.Template),
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()

		switch {
		case strings.HasSuffix(name, ".html.tmpl"):
			t, err := htmltmpl.ParseFS(fsys, name)
			if err != nil {
				return Templates{}, fmt.Errorf("load templates: parse %s: %w", name, err)
			}

			tmpls.HTML[strings.TrimSuffix(name, ".html.tmpl")] = t

		case strings.HasSuffix(name, ".tmpl"):
			t, err := texttmpl.ParseFS(fsys, name)
			if err != nil {
				return Templates{}, fmt.Errorf("load templates: parse %s: %w", name, err)
			}

			key := strings.TrimSuffix(strings.TrimSuffix(name, ".tmpl"), ".text")
			tmpls.Text[key] = t
		}
	}

	return tmpls, nil
}

// Render executes the HTML and text templates registered for kind with data.
// The returned [Message] has only HTML and Text set (whichever templates exist);
// the caller fills in the recipient and the subject.
//
// Returns an error if kind has neither template or a template fails to run.
func (t Templates) Render(kind Kind, data any) (Message, error) {
	htmlTmpl, hasHTML := t.HTML[string(kind)]
	textTmpl, hasText := t.Text[string(kind)]

	if !hasHTML && !hasText {
		return Message{}, fmt.Errorf("render email %s: no template", kind)
	}

	var msg Message

	if hasHTML {
		var buf bytes.Buffer
		if err := htmlTmpl.Execute(&buf, data); err != nil {
			return Message{}, fmt.Errorf("render email %s: %w", kind, err)
		}

		msg.HTML = buf.String()
	}

	if hasText {
		var buf bytes.Buffer
		if err := textTmpl.Execute(&buf, data); err != nil {
			return Message{}, fmt.Errorf("render email %s: %w", kind, err)
		}

		msg.Text = buf.String()
	}

	return msg, nil
}
