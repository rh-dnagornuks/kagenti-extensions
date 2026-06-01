package edit

import (
	"fmt"
	"strings"

	"github.com/kagenti/kagenti-extensions/authbridge/cmd/abctl/apiclient"
)

// FenceMarker delimits the active pipeline subtree (above) from the
// commented templates reference (below) inside the abctl edit tempfile.
// The save path strips everything from this line onward before applying.
//
// The exact bytes matter: detection is a literal line match. Keep them
// in sync with templates_test.go and configmap.go's fence-stripping.
const FenceMarker = "# === ABCTL TEMPLATES BELOW (stripped on save) ==="

// templatesBanner is the prose shown immediately below FenceMarker,
// telling the operator how to use the reference.
const templatesBanner = `# Reference: every plugin in the catalog. Copy a block above the fence,
# strip the leading "# " from each line, then adjust indentation to fit
# your inbound: or outbound: chain (typically 6-space "- name:" indent).
# This entire section is removed before kubectl apply.`

// RenderTemplates returns a fence marker followed by a commented YAML
// template block per plugin in the catalog. Returns nil for an empty
// catalog so callers can append unconditionally.
//
// Every emitted line starts with "#" — the templates section is pure
// comments. If the operator deletes the fence marker by accident, the
// templates still parse as comment-only YAML (no semantic effect),
// which is the safe-fallback the plan committed to.
func RenderTemplates(plugins []apiclient.PluginCatalogEntry) []byte {
	if len(plugins) == 0 {
		return nil
	}
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(FenceMarker)
	b.WriteString("\n")
	b.WriteString(templatesBanner)
	b.WriteString("\n")
	for _, p := range plugins {
		renderPluginTemplate(&b, p)
	}
	return []byte(b.String())
}

func renderPluginTemplate(b *strings.Builder, p apiclient.PluginCatalogEntry) {
	b.WriteString("\n# --- ")
	b.WriteString(p.Name)
	b.WriteString(" ---\n")
	if p.Description != "" {
		// Description is single-line (Go struct tags can't carry newlines)
		// so a one-line "# <description>" comment is sufficient.
		b.WriteString("# ")
		b.WriteString(p.Description)
		b.WriteString("\n")
	}

	// Split fields into required vs optional to render required first —
	// required-on-top makes the operator's eye land on the must-fill slots.
	var required, optional []apiclient.PluginFieldEntry
	for _, f := range p.Fields {
		if f.Required {
			required = append(required, f)
		} else {
			optional = append(optional, f)
		}
	}
	if len(required) > 0 {
		names := make([]string, len(required))
		for i, f := range required {
			names[i] = f.Name
		}
		b.WriteString("# Required: ")
		b.WriteString(strings.Join(names, ", "))
		b.WriteString("\n")
	}

	b.WriteString("#       - name: ")
	b.WriteString(p.Name)
	b.WriteString("\n")
	if len(p.Fields) == 0 {
		b.WriteString("#         # (no configurable fields)\n")
		return
	}
	b.WriteString("#         config:\n")
	for _, f := range required {
		renderField(b, f)
	}
	for _, f := range optional {
		renderField(b, f)
	}
}

func renderField(b *strings.Builder, f apiclient.PluginFieldEntry) {
	b.WriteString("#           ")
	b.WriteString(f.Name)
	b.WriteString(": ")
	b.WriteString(placeholderFor(f))

	var notes []string
	if f.Required {
		notes = append(notes, "required")
	}
	if f.Default != "" {
		notes = append(notes, fmt.Sprintf("default=%s", f.Default))
	}
	if len(f.Enum) > 0 {
		notes = append(notes, "enum="+strings.Join(f.Enum, "|"))
	}
	if f.Description != "" {
		notes = append(notes, f.Description)
	}
	if len(notes) > 0 {
		b.WriteString("  # ")
		b.WriteString(strings.Join(notes, "; "))
	}
	b.WriteString("\n")
}

// placeholderFor picks the YAML placeholder that goes after the field
// name. Documented defaults take priority for primitive types so the
// operator sees the actual fallback inline; otherwise an empty value
// matching the field's type.
func placeholderFor(f apiclient.PluginFieldEntry) string {
	if f.Default != "" && f.Type != "object" {
		// Quote strings so the line is valid YAML when uncommented.
		if f.Type == "string" {
			return fmt.Sprintf("%q", f.Default)
		}
		return f.Default
	}
	switch f.Type {
	case "string":
		return `""`
	case "int":
		return "0"
	case "bool":
		return "false"
	case "[]string":
		return "[]"
	case "object":
		return "{}"
	}
	return `""`
}
