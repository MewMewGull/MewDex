package evaluation

import (
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"
)

func reviewText(value string) string {
	value = strings.NewReplacer("|", "\\|", "\n", " ", "\r", " ", "`", "\\`").Replace(html.EscapeString(value))
	runes := []rune(value)
	if len(runes) > 500 {
		value = string(runes[:500]) + "…"
	}
	return value
}

func (r *runner) writeReview(report Report) error {
	var text strings.Builder
	fmt.Fprintf(&text, "# %s\n\nRun `%s`\n\nSuite `%s`\n\nProject configuration `%s` revision %d\n\nComplete %t. Passed %t.\n", reviewText(report.Suite.Name), report.RunID, report.SuiteHash, report.Harness.Version, report.Harness.Revision, report.Complete, report.Passed)
	if report.Runtime != nil {
		fmt.Fprintf(&text, "\nRuntime %s %s over %s\n", reviewText(report.Runtime.Adapter), reviewText(report.Runtime.Version), reviewText(report.Runtime.Transport))
	}
	if report.Error != "" {
		fmt.Fprintf(&text, "\n## Run requires inspection\n\n%s\n", reviewText(report.Error))
	}
	for _, item := range report.Cases {
		fmt.Fprintf(&text, "\n## %s · %s\n\nConversation `%s`. State %s. Archived %t.\n", reviewText(item.Model.ID), reviewText(item.CaseID), item.SessionID, item.State, item.Archived)
		if item.Trace != "" {
			fmt.Fprintf(&text, "\n[Saved trace](%s)\n", item.Trace)
		}
		if item.Grade == nil {
			continue
		}
		text.WriteString("\n| Check | Passed | Expected | Observed |\n|---|---|---|---|\n")
		for _, check := range item.Grade.Checks {
			fmt.Fprintf(&text, "| %s | %t | %s | %s |\n", reviewText(check.Name), check.Passed, reviewText(check.Expected), reviewText(check.Observed))
		}
	}
	text.WriteString("\n## Review changes\n\nCompare failed checks with their saved traces and configuration revision. Review a specific instruction, tool or routing change in the project configuration editor, then rerun the same suite in a new output directory and compare the results.\n")
	return os.WriteFile(filepath.Join(r.output, "review.md"), []byte(text.String()), 0600)
}
