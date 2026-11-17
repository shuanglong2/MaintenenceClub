package logs

const (
	kindWork       = "work"
	kindRepair     = "repair"
	kindMaintain   = "maintain"
	statusAccepted = "accepted"
	statusDoing    = "doing"
	statusReady    = "ready"
	statusDone     = "done"
)

// KindLabel 返回日志类型名称.
func KindLabel(kind string) string {
	switch kind {
	case kindWork:
		return "工作日志"
	case kindRepair:
		return "维修记录"
	case kindMaintain:
		return "保养记录"
	default:
		return kind
	}
}

// StatusLabel 返回报修状态名称.
func StatusLabel(status string) string {
	switch status {
	case statusAccepted:
		return "受理"
	case statusDoing:
		return "处理中"
	case statusReady:
		return "待取"
	case statusDone:
		return "完成"
	default:
		return status
	}
}

// TargetLabel 返回维修对象名称.
func TargetLabel(target string) string {
	switch target {
	case "item":
		return "器材"
	case "computer":
		return "电脑"
	case "other":
		return "其他"
	default:
		return target
	}
}

func validKind(kind string) bool {
	switch kind {
	case kindWork, kindRepair, kindMaintain:
		return true
	default:
		return false
	}
}

func validStatus(status string) bool {
	switch status {
	case statusAccepted, statusDoing, statusReady, statusDone:
		return true
	default:
		return false
	}
}

func validTarget(target string) bool {
	switch target {
	case "item", "computer", "other":
		return true
	default:
		return false
	}
}
