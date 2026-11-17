package finance

const (
	KindIn   = "in"
	KindOut  = "out"
	KindSave = "save"
	KindDraw = "draw"
)

// KindLabel 返回流水类型名称.
func KindLabel(kind string) string {
	switch kind {
	case KindIn:
		return "收入"
	case KindOut:
		return "支出"
	case KindSave:
		return "存入储蓄"
	case KindDraw:
		return "从储蓄取出"
	default:
		return kind
	}
}

func validKind(kind string) bool {
	switch kind {
	case KindIn, KindOut, KindSave, KindDraw:
		return true
	default:
		return false
	}
}
