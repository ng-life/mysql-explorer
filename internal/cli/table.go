package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/charmbracelet/x/term"
	"github.com/muesli/termenv"
)

// Each output uses its own renderer: redirected data must not inherit a global
// stdout color profile, and concurrent callers must not change global styling.
func tableRenderer(out io.Writer) (*lipgloss.Renderer, int) {
	r := lipgloss.NewRenderer(out)
	width := 0
	f, ok := out.(*os.File)
	if ok && term.IsTerminal(f.Fd()) {
		width, _, _ = term.GetSize(f.Fd())
	} else {
		r.SetColorProfile(termenv.Ascii)
	}
	if _, disabled := os.LookupEnv("NO_COLOR"); disabled {
		r.SetColorProfile(termenv.Ascii)
	}
	return r, width
}

func renderTable(out io.Writer, headers []string, rows [][]any) string {
	r, width := tableRenderer(out)
	return styledTable(r, width, headers, rows)
}

func styledTable(r *lipgloss.Renderer, width int, headers []string, rows [][]any) string {
	if len(headers) == 0 {
		return r.NewStyle().Foreground(lipgloss.Color("8")).Render("（无数据）")
	}
	safeHeaders := make([]string, len(headers))
	values := make([][]string, len(rows))
	minimum := len(headers) + 1
	for i, h := range headers {
		safeHeaders[i] = cell(h)
		minimum += max(2, lipgloss.Width(safeHeaders[i])) + 2
	}
	for i, row := range rows {
		values[i] = make([]string, len(headers))
		for j := range headers {
			if j < len(row) {
				values[i][j] = cell(row[j])
			} else {
				values[i][j] = "NULL"
			}
		}
	}
	// Wide result sets become one field/value table per record on narrow terminals.
	// Column names remain complete, and wrapped cells retain their full contents.
	if width > 0 && width < minimum && len(headers) > 1 {
		blocks := []string{}
		if len(rows) == 0 {
			fields := [][]string{}
			for _, h := range safeHeaders {
				fields = append(fields, []string{h, ""})
			}
			blocks = append(blocks, makeTable(r, width, []string{"字段", "值"}, fields).Render())
		} else {
			for i, row := range values {
				fields := [][]string{}
				for j, h := range safeHeaders {
					fields = append(fields, []string{h, row[j]})
				}
				blocks = append(blocks, r.NewStyle().Bold(true).Foreground(lipgloss.Color("6")).Render(fmt.Sprintf("记录 %d", i+1))+"\n"+makeTable(r, width, []string{"字段", "值"}, fields).Render())
			}
		}
		if len(rows) == 0 {
			blocks = append(blocks, "（无数据）")
		}
		return strings.Join(blocks, "\n\n")
	}
	t := makeTable(r, width, safeHeaders, values)
	result := t.Render()
	if len(rows) == 0 {
		result += "\n" + r.NewStyle().Foreground(lipgloss.Color("8")).Render("（无数据）")
	}
	return result
}

func makeTable(r *lipgloss.Renderer, width int, headers []string, rows [][]string) *table.Table {
	base := r.NewStyle().Padding(0, 1)
	widths := tableColumnWidths(width, headers, rows)
	t := table.New().Headers(headers...).Rows(rows...).Border(lipgloss.RoundedBorder()).
		BorderStyle(r.NewStyle().Foreground(lipgloss.Color("8"))).Wrap(true).
		StyleFunc(func(row, col int) lipgloss.Style {
			s := base
			if len(widths) > col {
				s = s.Width(widths[col])
			}
			if row == table.HeaderRow {
				return s.Bold(true).Foreground(lipgloss.Color("6"))
			}
			if row >= 0 && row < len(rows) && col < len(rows[row]) && (rows[row][col] == "NULL" || rows[row][col] == `""`) {
				return s.Foreground(lipgloss.Color("8")).Italic(true)
			}
			return s
		})
	if len(widths) > 0 {
		total := len(headers) + 1
		for _, w := range widths {
			total += w
		}
		t.Width(total)
	}

	return t
}

func tableFooter(out io.Writer, count int, hasMore bool) string {
	r, _ := tableRenderer(out)
	return r.NewStyle().Foreground(lipgloss.Color("8")).Render(fmt.Sprintf("行数: %d · has_more: %t", count, hasMore))
}

// Reserve enough room for complete headers, then share remaining terminal cells
// according to content needs. Fixed widths prevent library header truncation.
func tableColumnWidths(width int, headers []string, rows [][]string) []int {
	if width <= 0 {
		return nil
	}
	widths := make([]int, len(headers))
	desired := make([]int, len(headers))
	total := len(headers) + 1
	for i, h := range headers {
		widths[i] = max(2, lipgloss.Width(h)) + 2
		desired[i] = widths[i]
		for _, row := range rows {
			if i < len(row) {
				desired[i] = max(desired[i], lipgloss.Width(row[i])+2)
			}
		}
		total += widths[i]
	}
	for remaining := width - total; remaining > 0; remaining-- {
		selected := -1
		for i := range widths {
			if desired[i] > widths[i] && (selected < 0 || desired[i]-widths[i] > desired[selected]-widths[selected]) {
				selected = i
			}
		}
		if selected < 0 {
			break
		}
		widths[selected]++
	}
	return widths
}
