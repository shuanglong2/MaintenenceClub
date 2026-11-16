package tasks

const (
	statusTodo      = "todo"
	statusDoing     = "doing"
	statusDone      = "done"
	statusCancelled = "cancelled"
	priorityLow     = "low"
	priorityNormal  = "normal"
	priorityHigh    = "high"
)

var (
	kinds      = []string{"科普活动", "创新竞赛", "实验室保养", "电脑软硬件维护", "其他"}
	statuses   = []string{statusTodo, statusDoing, statusDone, statusCancelled}
	priorities = []string{priorityHigh, priorityNormal, priorityLow}
)

// Kinds 是任务类型.
func Kinds() []string { return append([]string(nil), kinds...) }

// Statuses 是看板列的顺序.
func Statuses() []string { return append([]string(nil), statuses...) }

// Priorities 是优先级, 高的在前.
func Priorities() []string { return append([]string(nil), priorities...) }

// StatusLabel 返回状态名称.
func StatusLabel(status string) string {
	switch status {
	case statusTodo:
		return "待办"
	case statusDoing:
		return "进行中"
	case statusDone:
		return "完成"
	case statusCancelled:
		return "取消"
	default:
		return status
	}
}

// PriorityLabel 返回优先级名称.
func PriorityLabel(priority string) string {
	switch priority {
	case priorityLow:
		return "低"
	case priorityNormal:
		return "普通"
	case priorityHigh:
		return "高"
	default:
		return priority
	}
}

func validKind(kind string) bool {
	for _, item := range kinds {
		if item == kind {
			return true
		}
	}
	return false
}

func validStatus(status string) bool {
	for _, item := range statuses {
		if item == status {
			return true
		}
	}
	return false
}

func validPriority(priority string) bool {
	switch priority {
	case priorityLow, priorityNormal, priorityHigh:
		return true
	default:
		return false
	}
}
