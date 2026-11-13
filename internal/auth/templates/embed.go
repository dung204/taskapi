// Package templates embeds the email templates used by the auth package.
//
// Every "*.tmpl" file in this directory is compiled into the binary and
// exposed through [FS]. The file name determines how [auth.NewEmailBuilder]
// treats a template:
//
//   - "<name>.html.tmpl" is parsed with html/template as the HTML body of the
//     email called <name> (for example "verify-email.html.tmpl").
//   - "<name>.text.tmpl" or "<name>.tmpl" is parsed with text/template as the
//     plain-text body of the email called <name>.
//
// The embed pattern is "*.tmpl", so a template with any other extension, or one
// placed in a subdirectory, is not embedded. Because the files are embedded,
// a new or changed template takes effect only after rebuilding.
package templates

import "embed"

// FS holds the embedded email template files. It is read-only and flat: all
// templates live at its root.
//
//go:embed *.tmpl
var FS embed.FS
