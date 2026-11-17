package people

import "errors"

// 角色与权限常量. 矩阵见 Can 的测试, 对应 Plan1 第 3 节.
const (
	RoleLeader = "leader"
	RoleDeputy = "deputy"
	RoleMember = "member"

	StatusActive = "active"
	StatusExited = "exited"

	ActPeopleManage    = "people.manage"
	ActPeopleList      = "people.list"
	ActPeopleSensitive = "people.sensitive"
	ActPeopleSelf      = "people.self"
	ActAuditRead       = "audit.read"
	ActSystemManage    = "system.manage"
	ActFinanceRead     = "finance.read"
	ActFinanceWrite    = "finance.write"
	ActTaskCreate      = "task.create"
	ActTaskEditAll     = "task.edit_all"
	ActTaskEditOwn     = "task.edit_own"
	ActEventEdit       = "event.edit"
	ActEventJoin       = "event.join"
	ActLogEditAll      = "log.edit_all"
	ActLogEditOwn      = "log.edit_own"
	ActInventoryManage = "inventory.manage"
	ActInventoryUse    = "inventory.use"
	ActDocsEdit        = "docs.edit"
	ActDocsVisibility  = "docs.visibility"
)

// User 是做权限判断时需要的身份.
type User struct {
	ID      int64
	Role    string
	Finance bool
	Active  bool
}

// Resource 是被操作的对象. OwnerID 为 0 表示与具体的人无关.
type Resource struct {
	OwnerID int64
}

// Can 判断这个人能否做这件事.
func Can(user User, action string, resource Resource) bool {
	if !user.Active {
		return false
	}
	self := resource.OwnerID != 0 && resource.OwnerID == user.ID
	switch action {
	case ActPeopleManage, ActSystemManage:
		return user.Role == RoleLeader
	case ActPeopleSelf, ActTaskCreate, ActEventJoin, ActInventoryUse, ActDocsEdit:
		return true
	case ActPeopleList, ActFinanceRead, ActFinanceWrite:
		return user.Role == RoleLeader
	case ActPeopleSensitive:
		return management(user) || self
	case ActAuditRead, ActTaskEditAll, ActEventEdit, ActLogEditAll, ActInventoryManage, ActDocsVisibility:
		return management(user)
	case ActTaskEditOwn, ActLogEditOwn:
		return management(user) || self
	default:
		return false
	}
}

func management(user User) bool {
	return user.Role == RoleLeader || user.Role == RoleDeputy
}

// RoleLabel 返回页面上的职务名称.
func RoleLabel(role string) string {
	switch role {
	case RoleLeader:
		return "组长"
	case RoleDeputy:
		return "副组长"
	case RoleMember:
		return "组员"
	default:
		return role
	}
}

// ParseRole 接受中文或英文角色名.
func ParseRole(value string) (string, error) {
	switch value {
	case RoleLeader, "组长":
		return RoleLeader, nil
	case RoleDeputy, "副组长":
		return RoleDeputy, nil
	case RoleMember, "组员":
		return RoleMember, nil
	default:
		return "", errBadRole
	}
}

// StatusLabel 返回状态名称.
var errBadRole = errors.New("角色无效")

// AllowLeaderChange 保证在组组长至少还有一名.
func AllowLeaderChange(activeLeaders int, targetIsActiveLeader bool, newRole, newStatus string) error {
	if targetIsActiveLeader && activeLeaders <= 1 && (newRole != RoleLeader || newStatus != StatusActive) {
		return ErrLastLeader
	}
	return nil
}

func StatusLabel(status string) string {
	switch status {
	case StatusActive:
		return "在组"
	case StatusExited:
		return "已退出"
	default:
		return status
	}
}
