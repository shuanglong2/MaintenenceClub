// Package mdown 渲染 Markdown, 并清掉不安全的 HTML.
package mdown

import (
	"bytes"
	"html/template"
	"regexp"
	"sync"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
)

var (
	once sync.Once
	md   goldmark.Markdown
	pol  *bluemonday.Policy
)

func prepare() {
	once.Do(func() {
		md = goldmark.New(
			goldmark.WithExtensions(extension.GFM),
			goldmark.WithParserOptions(parser.WithAutoHeadingID()),
		)
		pol = bluemonday.UGCPolicy()
		pol.AllowRelativeURLs(true)
		pol.AllowAttrs("id").Matching(regexp.MustCompile(`^[\p{L}\p{N}\-_]+$`)).OnElements("h1", "h2", "h3", "h4", "h5", "h6")
		pol.AllowElements("input")
		pol.AllowAttrs("type", "checked", "disabled").OnElements("input")
	})
}

// Render 先渲染再清洗. 调用方可以把结果直接写进页面.
func Render(src string) template.HTML {
	prepare()
	var buf bytes.Buffer
	if err := md.Convert([]byte(src), &buf); err != nil {
		return ""
	}
	return template.HTML(pol.Sanitize(buf.String()))
}
