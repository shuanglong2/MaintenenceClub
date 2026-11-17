package finance

import (
	"bytes"
	"encoding/csv"
)

func writeCSV(items []Entry) ([]byte, error) {
	var buf bytes.Buffer
	buf.Write([]byte{0xEF, 0xBB, 0xBF})
	w := csv.NewWriter(&buf)
	if err := w.Write([]string{"日期", "类型", "金额", "分类", "备注", "经手人", "作废"}); err != nil {
		return nil, err
	}
	for _, item := range items {
		voided := ""
		if item.Voided {
			voided = "是"
		}
		if err := w.Write([]string{
			item.DateText(), KindLabel(item.Kind), yuan(item.Cents), csvCell(item.Category), csvCell(item.Note), csvCell(item.ActorName), voided,
		}); err != nil {
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
