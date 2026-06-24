package compliance

import (
	"encoding/json"
	"fmt"
	"strings"
)

// RenderMarkdown renders r as a human-readable Markdown document.
func RenderMarkdown(r Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s — %s\n\n", strings.ToUpper(r.Regime), r.Doc)
	fmt.Fprintf(&b, "_Generated %s_\n\n", r.GeneratedAt.Format("2006-01-02 15:04:05 MST"))

	for _, sec := range r.Sections {
		fmt.Fprintf(&b, "## %s\n\n", sec.Title)
		fmt.Fprintf(&b, "**Status:** `%s`\n\n", sec.Status)
		if sec.Body != "" {
			fmt.Fprintf(&b, "%s\n\n", sec.Body)
		}
		if len(sec.Rows) > 0 {
			b.WriteString("| Obligation | Status | Evidence | Source |\n")
			b.WriteString("|---|---|---|---|\n")
			for _, row := range sec.Rows {
				fmt.Fprintf(&b, "| %s | `%s` | %s | %s |\n",
					row.Obligation, row.Status, row.Evidence, row.Source)
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

// RenderJSON renders r as indented JSON.
func RenderJSON(r Report) ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}
