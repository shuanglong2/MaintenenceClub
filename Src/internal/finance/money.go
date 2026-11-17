package finance

import (
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Row 是汇总用的一笔流水. 作废的不计入.
type Row struct {
	Kind     string
	Cents    int64
	Month    string
	Category string
	Voided   bool
}

// Sum 是一组流水按四种类型加总的结果.
type Sum struct {
	Key  string
	In   int64
	Out  int64
	Save int64
	Draw int64
}

// Cash 是这组流水对手头的影响: 收入 - 支出 - 存入储蓄 + 从储蓄取出.
func (s Sum) Cash() int64 { return s.In - s.Out - s.Save + s.Draw }

// Savings 是这组流水对储蓄的影响: 存入 - 取出.
func (s Sum) Savings() int64 { return s.Save - s.Draw }

// BalanceOf 返回手头和储蓄. 作废记录不计入.
func BalanceOf(rows []Row) (cash, savings int64) {
	for _, row := range rows {
		if row.Voided || row.Cents <= 0 {
			continue
		}
		switch row.Kind {
		case KindIn:
			cash += row.Cents
		case KindOut:
			cash -= row.Cents
		case KindSave:
			cash -= row.Cents
			savings += row.Cents
		case KindDraw:
			cash += row.Cents
			savings -= row.Cents
		}
	}
	return cash, savings
}

// ByMonth 按月份汇总, 月份格式为 YYYY-MM.
func ByMonth(rows []Row) []Sum {
	return group(rows, func(row Row) string { return row.Month })
}

// ByCategory 按分类汇总.
func ByCategory(rows []Row) []Sum {
	return group(rows, func(row Row) string { return row.Category })
}

func group(rows []Row, key func(Row) string) []Sum {
	bucket := map[string]*Sum{}
	var keys []string
	for _, row := range rows {
		if row.Voided || row.Cents <= 0 {
			continue
		}
		name := key(row)
		sum := bucket[name]
		if sum == nil {
			sum = &Sum{Key: name}
			bucket[name] = sum
			keys = append(keys, name)
		}
		switch row.Kind {
		case KindIn:
			sum.In += row.Cents
		case KindOut:
			sum.Out += row.Cents
		case KindSave:
			sum.Save += row.Cents
		case KindDraw:
			sum.Draw += row.Cents
		}
	}
	sort.Strings(keys)
	out := make([]Sum, 0, len(keys))
	for _, name := range keys {
		out = append(out, *bucket[name])
	}
	return out
}

// ParseYuan 把 "元" 解析成分. 拒绝负数, 零和多于两位小数.
func ParseYuan(text string) (int64, error) {
	text = strings.TrimSpace(text)
	if text == "" || strings.ContainsAny(text, "+-, \t") || strings.HasPrefix(text, ".") {
		return 0, errInput("金额不正确")
	}
	whole, frac, found := strings.Cut(text, ".")
	if strings.Contains(frac, ".") || whole == "" || !digits(whole) {
		return 0, errInput("金额不正确")
	}
	if found && (frac == "" || len(frac) > 2 || !digits(frac)) {
		return 0, errInput("金额最多两位小数")
	}
	yuan, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, errInput("金额不正确")
	}
	var part int64
	if found {
		if len(frac) == 1 {
			frac += "0"
		}
		part, err = strconv.ParseInt(frac, 10, 64)
		if err != nil {
			return 0, errInput("金额不正确")
		}
	}
	if yuan > (1<<62)/100 {
		return 0, errInput("金额不正确")
	}
	cents := yuan*100 + part
	if cents <= 0 {
		return 0, errInput("金额要大于 0")
	}
	return cents, nil
}

func digits(value string) bool {
	if value == "" || utf8.RuneCountInString(value) != len(value) {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
