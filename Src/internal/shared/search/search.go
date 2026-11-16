// Package search 把中文按相邻两个字切开, 供全文索引使用.
package search

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Tokens 把文本变成搜索词. 连续汉字取相邻二元组, 英文和数字按单词, 全部小写.
func Tokens(text string) []string {
	runes := []rune(normalize(text))
	var out []string
	for i := 0; i < len(runes); {
		r := runes[i]
		switch {
		case isHan(r):
			j := i + 1
			for j < len(runes) && isHan(runes[j]) {
				j++
			}
			chunk := runes[i:j]
			if len(chunk) == 1 {
				out = append(out, string(chunk))
			} else {
				for k := 0; k < len(chunk)-1; k++ {
					out = append(out, string(chunk[k:k+2]))
				}
			}
			i = j
		case isWord(r):
			j := i + 1
			for j < len(runes) && isWord(runes[j]) {
				j++
			}
			out = append(out, string(runes[i:j]))
			i = j
		default:
			i++
		}
	}
	return out
}

// Join 用空格把搜索词拼成一列, 写入 search_tokens.
func Join(text string) string {
	return strings.Join(Tokens(text), " ")
}

// Query 返回查询方式. 只有一个汉字时 like 非空, 调用方改用 ILIKE. 否则使用 simple 配置的 tsquery.
func Query(text string) (like, tsquery string) {
	groups := phraseGroups(text)
	if len(groups) == 1 && len(groups[0]) == 1 && utf8.RuneCountInString(groups[0][0]) == 1 && isHan([]rune(groups[0][0])[0]) {
		return groups[0][0], ""
	}
	if len(groups) == 0 {
		return "", ""
	}
	parts := make([]string, 0, len(groups))
	for _, group := range groups {
		if len(group) == 1 {
			parts = append(parts, group[0])
			continue
		}
		parts = append(parts, "("+strings.Join(group, " <-> ")+")")
	}
	return "", strings.Join(parts, " & ")
}

// Snippet 从原文里取出包含查询词的一小段.
func Snippet(text, query string) string {
	needles := Tokens(query)
	if like, _ := Query(query); like != "" {
		needles = []string{like}
	}
	best := -1
	lower := strings.ToLower(text)
	for _, needle := range needles {
		idx := strings.Index(lower, strings.ToLower(needle))
		if idx >= 0 && (best < 0 || idx < best) {
			best = idx
		}
	}
	runes := []rune(text)
	if best < 0 {
		return clipRunes(runes, 0, 80)
	}
	at := utf8.RuneCountInString(text[:best])
	start := at - 24
	if start < 0 {
		start = 0
	}
	return clipRunes(runes, start, 72)
}

// Highlight 把片段里的查询词标出来. 返回的字符串已经转义, 可以当作 HTML.
func Highlight(snippet, query string) string {
	needles := Tokens(query)
	if like, _ := Query(query); like != "" {
		needles = []string{like}
	}
	runes := []rune(snippet)
	mark := make([]bool, len(runes))
	for _, needle := range needles {
		nr := []rune(strings.ToLower(needle))
		if len(nr) == 0 || len(nr) > len(runes) {
			continue
		}
		for i := 0; i+len(nr) <= len(runes); i++ {
			if strings.EqualFold(string(runes[i:i+len(nr)]), needle) {
				for j := 0; j < len(nr); j++ {
					mark[i+j] = true
				}
			}
		}
	}
	var b strings.Builder
	open := false
	for i, r := range runes {
		if mark[i] && !open {
			b.WriteString("<mark>")
			open = true
		}
		if !mark[i] && open {
			b.WriteString("</mark>")
			open = false
		}
		writeEscaped(&b, r)
	}
	if open {
		b.WriteString("</mark>")
	}
	return b.String()
}

func phraseGroups(text string) [][]string {
	runes := []rune(normalize(text))
	var groups [][]string
	for i := 0; i < len(runes); {
		r := runes[i]
		switch {
		case isHan(r):
			j := i + 1
			for j < len(runes) && isHan(runes[j]) {
				j++
			}
			chunk := runes[i:j]
			if len(chunk) == 1 {
				groups = append(groups, []string{string(chunk)})
			} else {
				phrase := make([]string, 0, len(chunk)-1)
				for k := 0; k < len(chunk)-1; k++ {
					phrase = append(phrase, string(chunk[k:k+2]))
				}
				groups = append(groups, phrase)
			}
			i = j
		case isWord(r):
			j := i + 1
			for j < len(runes) && isWord(runes[j]) {
				j++
			}
			groups = append(groups, []string{string(runes[i:j])})
			i = j
		default:
			i++
		}
	}
	return groups
}

func normalize(text string) string {
	var b strings.Builder
	b.Grow(len(text))
	for _, r := range text {
		switch {
		case r >= 0xFF01 && r <= 0xFF5E:
			r = r - 0xFEE0
		case r == 0x3000:
			r = ' '
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

func isHan(r rune) bool {
	return unicode.Is(unicode.Han, r)
}

func isWord(r rune) bool {
	return unicode.Is(unicode.Latin, r) || unicode.IsDigit(r)
}

func clipRunes(runes []rune, start, n int) string {
	if start > len(runes) {
		start = len(runes)
	}
	end := start + n
	if end > len(runes) {
		end = len(runes)
	}
	s := string(runes[start:end])
	if start > 0 {
		s = "…" + s
	}
	if end < len(runes) {
		s += "…"
	}
	return s
}

func writeEscaped(b *strings.Builder, r rune) {
	switch r {
	case '&':
		b.WriteString("&amp;")
	case '<':
		b.WriteString("&lt;")
	case '>':
		b.WriteString("&gt;")
	case '"':
		b.WriteString("&quot;")
	default:
		b.WriteRune(r)
	}
}
