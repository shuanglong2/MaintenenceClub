package migrate

import (
	"errors"
	"strings"
)

// splitSQL 按语句拆开迁移脚本. 字符串, 美元引号和注释里的分号不会被切开.
func splitSQL(src string) ([]string, error) {
	var out []string
	var b strings.Builder
	flush := func() {
		s := strings.TrimSpace(b.String())
		b.Reset()
		if s != "" {
			out = append(out, s)
		}
	}
	for i := 0; i < len(src); i++ {
		c := src[i]
		if c == '-' && i+1 < len(src) && src[i+1] == '-' {
			for i < len(src) && src[i] != '\n' {
				i++
			}
			b.WriteByte('\n')
			continue
		}
		if c == '/' && i+1 < len(src) && src[i+1] == '*' {
			end := strings.Index(src[i+2:], "*/")
			if end < 0 {
				return nil, errors.New("未结束的块注释")
			}
			i += 2 + end + 1
			continue
		}
		if c == '\'' {
			b.WriteByte('\'')
			i++
			closed := false
			for i < len(src) {
				b.WriteByte(src[i])
				if src[i] == '\'' {
					if i+1 < len(src) && src[i+1] == '\'' {
						i++
						b.WriteByte(src[i])
						i++
						continue
					}
					closed = true
					break
				}
				i++
			}
			if !closed {
				return nil, errors.New("未结束的字符串")
			}
			continue
		}
		if tag, ok := dollarTag(src[i:]); ok {
			closer := "$" + tag + "$"
			rest := src[i+len(closer):]
			end := strings.Index(rest, closer)
			if end < 0 {
				return nil, errors.New("未结束的美元引号")
			}
			chunk := src[i : i+len(closer)+end+len(closer)]
			b.WriteString(chunk)
			i += len(chunk) - 1
			continue
		}
		if c == ';' {
			flush()
			continue
		}
		b.WriteByte(c)
	}
	flush()
	return out, nil
}

func dollarTag(s string) (string, bool) {
	if len(s) < 2 || s[0] != '$' {
		return "", false
	}
	j := 1
	for j < len(s) && isTagChar(s[j]) {
		j++
	}
	if j >= len(s) || s[j] != '$' {
		return "", false
	}
	if j > 1 && s[1] >= '0' && s[1] <= '9' {
		return "", false
	}
	return s[1:j], true
}

func isTagChar(c byte) bool {
	return c == '_' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')
}
