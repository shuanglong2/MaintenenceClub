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
	kinds = []string{
		"小家用电器维修", "实验室仪器保养", "电子设备类维修", "维修工具外借出",
		"科普活动", "创新竞赛", "实验室保养", "电脑软硬件维护", "其他",
	}
	repairKinds = []string{"小家用电器维修", "实验室仪器保养", "电子设备类维修", "维修工具外借出"}
	statuses    = []string{statusTodo, statusDoing, statusDone, statusCancelled}
	priorities  = []string{priorityHigh, priorityNormal, priorityLow}
)

// Kinds 是任务类型, 含早期记录里还在用的旧名称.
func Kinds() []string { return append([]string(nil), kinds...) }

// RepairKinds 是新建维修任务时可选的四种类型.
func RepairKinds() []string { return append([]string(nil), repairKinds...) }

// Statuses 是看板列的顺序.
func Statuses() []string { return append([]string(nil), statuses...) }

// Priorities 是优先级, 高的在前.
func Priorities() []string { return append([]string(nil), priorities...) }

// StatusLabel 返回状态名称.
func StatusLabel(status string) string {
	switch status {
	case statusTodo:
		return "待处理"
	case statusDoing:
		return "进行中"
	case statusDone:
		return "已完成"
	case statusCancelled:
		return "已放弃"
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
