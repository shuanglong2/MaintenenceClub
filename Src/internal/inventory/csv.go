package inventory

import (
	"bytes"
	"encoding/csv"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

// CSVHeader 是导入和导出共用的表头.
var CSVHeader = []string{"编号", "名称", "类别", "规格", "单位", "位置", "最低库存", "逐件", "数量", "备注"}

// ImportRow 是 CSV 里的一行物品.
type ImportRow struct {
	Code     string
	Name     string
	Category string
	Spec     string
	Unit     string
	Location string
	MinQty   int
	Tracked  bool
	Qty      int
	Note     string
}

// TemplateCSV 是可下载的空模板, 带表头.
func TemplateCSV() []byte {
	var buf bytes.Buffer
	buf.Write([]byte{0xEF, 0xBB, 0xBF})
	w := csv.NewWriter(&buf)
	_ = w.Write(CSVHeader)
	w.Flush()
	return buf.Bytes()
}

// ParseImport 读取物品 CSV.
func ParseImport(r io.Reader) ([]ImportRow, error) {
	raw, err := io.ReadAll(io.LimitReader(r, 1<<20))
	if err != nil {
		return nil, err
	}
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
	if !utf8.Valid(raw) {
		return nil, errText("CSV 必须是 UTF-8")
	}
	reader := csv.NewReader(bytes.NewReader(raw))
	reader.FieldsPerRecord = len(CSVHeader)
	reader.TrimLeadingSpace = true
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, errText("CSV 无法读取")
	}
	if len(rows) < 2 {
		return nil, errText("CSV 至少要有表头和一行")
	}
	for i, head := range CSVHeader {
		if strings.TrimSpace(rows[0][i]) != head {
			return nil, errText("表头必须是 " + strings.Join(CSVHeader, ","))
		}
	}
	if len(rows) > 501 {
		return nil, errText("一次最多导入 500 件")
	}
	out := make([]ImportRow, 0, len(rows)-1)
	for i, row := range rows[1:] {
		item, err := parseImportRow(row)
		if err != nil {
			return nil, errText("第 " + strconv.Itoa(i+2) + " 行: " + err.Error())
		}
		out = append(out, item)
	}
	return out, nil
}

func parseImportRow(row []string) (ImportRow, error) {
	minQty, err := strconv.Atoi(strings.TrimSpace(row[6]))
	if err != nil || minQty < 0 {
		return ImportRow{}, errText("最低库存无效")
	}
	qty, err := strconv.Atoi(strings.TrimSpace(row[8]))
	if err != nil || qty < 0 {
		return ImportRow{}, errText("数量无效")
	}
	tracked, err := parseYes(row[7])
	if err != nil {
		return ImportRow{}, err
	}
	unit := strings.TrimSpace(row[4])
	if unit == "" {
		unit = "个"
	}
	return ImportRow{
		Code: strings.TrimSpace(row[0]), Name: strings.TrimSpace(row[1]), Category: strings.TrimSpace(row[2]),
		Spec: strings.TrimSpace(row[3]), Unit: unit, Location: strings.TrimSpace(row[5]),
		MinQty: minQty, Tracked: tracked, Qty: qty, Note: strings.TrimSpace(row[9]),
	}, nil
}

func parseYes(value string) (bool, error) {
	switch strings.TrimSpace(value) {
	case "是", "1", "true", "yes":
		return true, nil
	case "否", "0", "false", "no", "":
		return false, nil
	default:
		return false, errText("逐件请填 是 或 否")
	}
}

func errText(text string) error {
	return &inputError{text}
}

type inputError struct{ text string }

func (e *inputError) Error() string { return e.text }

func (e *inputError) Unwrap() error { return ErrBadInput }
