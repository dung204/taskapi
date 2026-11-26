package email

import (
	"bytes"
	"testing"
	"testing/fstest"
)

func TestLoadTemplates(t *testing.T) {
	fsys := fstest.MapFS{
		"hello.html.tmpl": {Data: []byte("<p>{{.Name}}</p>")},
		"hello.text.tmpl": {Data: []byte("hi {{.Name}}")},
		"plain.tmpl":      {Data: []byte("plain {{.Name}}")},
		"notes.txt":       {Data: []byte("ignored")},
		"dir/inner.tmpl":  {Data: []byte("ignored")},
	}

	got, err := LoadTemplates(fsys)
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}

	if len(got.HTML) != 1 || len(got.Text) != 2 {
		t.Fatalf("html = %d, text = %d, want 1 and 2", len(got.HTML), len(got.Text))
	}

	render := func(exec func(*bytes.Buffer) error) string {
		var b bytes.Buffer
		if err := exec(&b); err != nil {
			t.Fatalf("execute: %v", err)
		}
		return b.String()
	}
	data := map[string]string{"Name": "<A>"}

	if s := render(func(b *bytes.Buffer) error { return got.HTML["hello"].Execute(b, data) }); s != "<p>&lt;A&gt;</p>" {
		t.Errorf("html = %q", s)
	}
	if s := render(func(b *bytes.Buffer) error { return got.Text["hello"].Execute(b, data) }); s != "hi <A>" {
		t.Errorf("text = %q", s)
	}
	if s := render(func(b *bytes.Buffer) error { return got.Text["plain"].Execute(b, data) }); s != "plain <A>" {
		t.Errorf("plain = %q", s)
	}
}

func TestLoadTemplates_ParseError(t *testing.T) {
	_, err := LoadTemplates(fstest.MapFS{"bad.text.tmpl": {Data: []byte("{{.Name")}})
	if err == nil {
		t.Fatal("err = nil, want a parse error")
	}
}

func TestTemplates_Render(t *testing.T) {
	tmpls, err := LoadTemplates(fstest.MapFS{
		"both.html.tmpl": {Data: []byte("<p>{{.Name}}</p>")},
		"both.text.tmpl": {Data: []byte("hi {{.Name}}")},
		"text_only.tmpl": {Data: []byte("only {{.Name}}")},
		"broken.tmpl":    {Data: []byte("{{.Name.Field}}")},
	})
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	data := map[string]any{"Name": "<A>"}

	got, err := tmpls.Render("both", data)
	if err != nil {
		t.Fatalf("Render both: %v", err)
	}
	if got.HTML != "<p>&lt;A&gt;</p>" || got.Text != "hi <A>" || got.To != "" || got.Subject != "" {
		t.Errorf("both = %+v", got)
	}

	got, err = tmpls.Render("text_only", data)
	if err != nil {
		t.Fatalf("Render text_only: %v", err)
	}
	if got.HTML != "" || got.Text != "only <A>" {
		t.Errorf("text_only = %+v", got)
	}

	if _, err := tmpls.Render("unknown", data); err == nil {
		t.Error("unknown kind: err = nil, want an error")
	}

	if _, err := tmpls.Render("broken", data); err == nil {
		t.Error("failing template: err = nil, want an error")
	}
}
