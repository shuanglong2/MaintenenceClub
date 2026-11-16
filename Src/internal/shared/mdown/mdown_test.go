package mdown

import (
	"strings"
	"testing"
)

func TestRenderKeepsMarkdown(t *testing.T) {
	html := string(Render("# 标题\n\n| 名称 | 数量 |\n| --- | --- |\n| 烙铁 | 2 |\n\n- [ ] 校准\n\n![探头](/files/7)\n"))
	if !strings.Contains(html, "标题") || !strings.Contains(html, "<table>") || !strings.Contains(html, "校准") {
		t.Fatal(html)
	}
	if !strings.Contains(html, `src="/files/7"`) {
		t.Fatal(html)
	}
	if !strings.Contains(html, `id="`) {
		t.Fatal(html)
	}
}

func TestRenderStripsXSS(t *testing.T) {
	src := strings.Join([]string{
		`<script>alert(1)</script>`,
		`<img src=x onerror=alert(1)>`,
		`[点我](javascript:alert(1))`,
		`<iframe src="https://evil.example"></iframe>`,
		`<svg onload=alert(1)></svg>`,
		`![x](data:text/html,hi)`,
	}, "\n\n")
	html := strings.ToLower(string(Render(src)))
	for _, bad := range []string{"<script", "onerror", "javascript:", "<iframe", "onload", "data:text/html"} {
		if strings.Contains(html, bad) {
			t.Fatalf("残留 %s\n%s", bad, html)
		}
	}
}
