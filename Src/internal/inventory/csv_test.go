package inventory

import (
	"errors"
	"strings"
	"testing"
)

func TestParseImport(t *testing.T) {
	raw := "编号,名称,类别,规格,单位,位置,最低库存,逐件,数量,备注\nR1,电阻,元件,10k,个,实验室/抽屉,5,否,20,常用\n"
	rows, err := ParseImport(strings.NewReader(raw))
	if err != nil || len(rows) != 1 || rows[0].Code != "R1" || !strings.Contains(rows[0].Location, "实验室") || rows[0].Qty != 20 {
		t.Fatal(err, rows)
	}
	if _, err := ParseImport(strings.NewReader("编号,名称\n")); !errors.Is(err, ErrBadInput) {
		t.Fatal(err)
	}
}

func TestCSVCell(t *testing.T) {
	if csvCell("=1+1") != "'=1+1" {
		t.Fatal(csvCell("=1+1"))
	}
}
