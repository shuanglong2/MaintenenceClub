package people

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// Imported 是导入后只展示一次的账号.
type Imported struct {
	Name     string
	Login    string
	Role     string
	Password string
}

// ParseImport 读取人员 CSV. 表头是姓名, 登录名, 角色.
func ParseImport(r io.Reader) ([]Imported, error) {
	raw, err := io.ReadAll(io.LimitReader(r, 1<<20))
	if err != nil {
		return nil, err
	}
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
	if !utf8.Valid(raw) {
		return nil, fmt.Errorf("CSV 必须是 UTF-8")
	}
	reader := csv.NewReader(bytes.NewReader(raw))
	reader.FieldsPerRecord = 3
	reader.TrimLeadingSpace = true
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("CSV 无法读取: %w", err)
	}
	if len(rows) < 2 {
		return nil, fmt.Errorf("CSV 至少要有表头和一行")
	}
	if strings.TrimSpace(rows[0][0]) != "姓名" || strings.TrimSpace(rows[0][1]) != "登录名" || strings.TrimSpace(rows[0][2]) != "角色" {
		return nil, fmt.Errorf("表头必须是 姓名,登录名,角色")
	}
	if len(rows) > 201 {
		return nil, fmt.Errorf("一次最多导入 200 人")
	}
	out := make([]Imported, 0, len(rows)-1)
	for i, row := range rows[1:] {
		item := Imported{
			Name:  strings.TrimSpace(row[0]),
			Login: strings.ToLower(strings.TrimSpace(row[1])),
			Role:  strings.TrimSpace(row[2]),
		}
		if item.Name == "" || item.Login == "" || item.Role == "" {
			return nil, fmt.Errorf("第 %d 行有空字段", i+2)
		}
		if _, err := ParseRole(item.Role); err != nil {
			return nil, fmt.Errorf("第 %d 行角色无效", i+2)
		}
		out = append(out, item)
	}
	return out, nil
}

// PasswordCSV 把一次性密码写成带 BOM 的 CSV.
func PasswordCSV(rows []Imported) ([]byte, error) {
	var buf bytes.Buffer
	buf.Write([]byte{0xEF, 0xBB, 0xBF})
	w := csv.NewWriter(&buf)
	if err := w.Write([]string{"姓名", "登录名", "角色", "初始密码"}); err != nil {
		return nil, err
	}
	for _, row := range rows {
		if err := w.Write([]string{csvCell(row.Name), csvCell(row.Login), csvCell(RoleLabel(row.Role)), csvCell(row.Password)}); err != nil {
			return nil, err
		}
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}

func csvCell(value string) string {
	if value == "" {
		return value
	}
	switch value[0] {
	case '=', '+', '-', '@':
		return "'" + value
	default:
		return value
	}
}
