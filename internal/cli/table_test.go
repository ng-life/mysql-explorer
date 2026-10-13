package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func plainRenderer() *lipgloss.Renderer {
	r := lipgloss.NewRenderer(&bytes.Buffer{})
	r.SetColorProfile(termenv.Ascii)
	return r
}

func TestTableChineseAlignmentAndEmpty(t *testing.T) {
	rendered := styledTable(plainRenderer(), 0, []string{"名称", "备注"}, [][]any{{"orders", "订单"}, {"客户", "客户资料"}})
	if !strings.Contains(rendered, "╭") || !strings.Contains(rendered, "订单") {
		t.Fatal(rendered)
	}
	width := lipgloss.Width(strings.Split(rendered, "\n")[0])
	for _, line := range strings.Split(rendered, "\n") {
		if lipgloss.Width(line) != width {
			t.Fatalf("misaligned %q: %d != %d", line, lipgloss.Width(line), width)
		}
	}
	rendered = styledTable(plainRenderer(), 0, []string{"名称", "备注"}, nil)
	if !strings.Contains(rendered, "名称") || !strings.Contains(rendered, "（无数据）") {
		t.Fatal(rendered)
	}
}

func TestNarrowTableKeepsAllFieldsAndValues(t *testing.T) {
	headers := []string{"订单编号", "金额", "来源服务名称", "很长的中文字段名称"}
	rendered := styledTable(plainRenderer(), 24, headers, [][]any{{"9007199254740993", "12.30", "TMS_RELEASE", "订单内容完整保留"}})
	if !strings.Contains(rendered, "记录 1") {
		t.Fatal(rendered)
	}
	// Recover each column independently: wrapped lines interleave field/value text.
	columns := [2]string{}
	for _, line := range strings.Split(rendered, "\n") {
		parts := strings.Split(line, "│")
		if len(parts) == 4 {
			for i := range columns {
				columns[i] += strings.ReplaceAll(parts[i+1], " ", "")
			}
		}
	}
	for _, value := range headers {
		if !strings.Contains(columns[0], value) {
			t.Fatalf("lost header %q: %s", value, rendered)
		}
	}
	for _, value := range []string{"9007199254740993", "12.30", "TMS_RELEASE", "订单内容完整保留"} {
		if !strings.Contains(columns[1], value) {
			t.Fatalf("lost value %q: %s", value, rendered)
		}
	}

	for _, line := range strings.Split(rendered, "\n") {
		if lipgloss.Width(line) > 24 {
			t.Fatalf("too wide: %s", line)
		}
	}
}

func TestTableColorPolicyAndJSONUnchanged(t *testing.T) {
	var out, errs bytes.Buffer
	t.Setenv("CLICOLOR_FORCE", "1")
	if code := output("table", &out, &errs, nil, []string{"字段"}, [][]any{{nil}, {""}}); code != 0 {
		t.Fatal(code)
	}
	if strings.Contains(out.String(), "\x1b") || !strings.Contains(out.String(), "NULL") {
		t.Fatal(out.String())
	}
	out.Reset()
	if code := output("json", &out, &errs, [][]any{{"9007199254740993", nil, ""}}, nil, nil); code != 0 {
		t.Fatal(code)
	}
	if out.String() != "[\n  [\n    \"9007199254740993\",\n    null,\n    \"\"\n  ]\n]\n" {
		t.Fatal(out.String())
	}
	r := lipgloss.NewRenderer(&bytes.Buffer{})
	r.SetColorProfile(termenv.ANSI256)
	if !strings.Contains(styledTable(r, 80, []string{"字段"}, [][]any{{"值"}}), "\x1b[") {
		t.Fatal("missing terminal styling")
	}
}
