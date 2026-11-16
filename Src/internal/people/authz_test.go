package people

import "testing"

func TestCanMatrix(t *testing.T) {
	leader := User{ID: 1, Role: RoleLeader, Active: true}
	deputy := User{ID: 2, Role: RoleDeputy, Active: true}
	member := User{ID: 3, Role: RoleMember, Active: true}
	finance := User{ID: 4, Role: RoleMember, Finance: true, Active: true}
	leaderFinance := User{ID: 5, Role: RoleLeader, Finance: true, Active: true}
	exited := User{ID: 6, Role: RoleLeader, Active: false}
	other := Resource{OwnerID: 9}
	ownMember := Resource{OwnerID: member.ID}
	ownFinance := Resource{OwnerID: finance.ID}

	cases := []struct {
		name     string
		user     User
		action   string
		resource Resource
		want     bool
	}{
		{"组长管理人员", leader, ActPeopleManage, other, true},
		{"副组长不能管理人员", deputy, ActPeopleManage, other, false},
		{"组员不能管理人员", member, ActPeopleManage, other, false},
		{"财务开关不能管理人员", finance, ActPeopleManage, other, false},
		{"在组的人能看名单", member, ActPeopleList, Resource{}, true},
		{"组长能看他人联系方式", leader, ActPeopleSensitive, other, true},
		{"副组长能看他人联系方式", deputy, ActPeopleSensitive, other, true},
		{"组员不能看他人联系方式", member, ActPeopleSensitive, other, false},
		{"组员能看自己的联系方式", member, ActPeopleSensitive, ownMember, true},
		{"组长能看审计", leader, ActAuditRead, Resource{}, true},
		{"副组长能看审计", deputy, ActAuditRead, Resource{}, true},
		{"组员不能看审计", member, ActAuditRead, Resource{}, false},
		{"只有组长能管系统", leader, ActSystemManage, Resource{}, true},
		{"副组长不能管系统", deputy, ActSystemManage, Resource{}, false},
		{"组长能看财务", leader, ActFinanceRead, Resource{}, true},
		{"组长默认不能改财务", leader, ActFinanceWrite, Resource{}, false},
		{"有财务开关的组长能改财务", leaderFinance, ActFinanceWrite, Resource{}, true},
		{"副组长能看财务", deputy, ActFinanceRead, Resource{}, true},
		{"副组长不能改财务", deputy, ActFinanceWrite, Resource{}, false},
		{"组员不能看财务", member, ActFinanceRead, Resource{}, false},
		{"有财务开关的组员能改财务", finance, ActFinanceWrite, Resource{}, true},
		{"组员能建任务", member, ActTaskCreate, Resource{}, true},
		{"组员不能改全部任务", member, ActTaskEditAll, Resource{}, false},
		{"组员能改自己的任务", member, ActTaskEditOwn, ownMember, true},
		{"组员不能改别人的任务", member, ActTaskEditOwn, other, false},
		{"副组长能改全部任务", deputy, ActTaskEditAll, other, true},
		{"组员不能改活动", member, ActEventEdit, Resource{}, false},
		{"组员能报名", member, ActEventJoin, Resource{}, true},
		{"组员只能改自己的日志", member, ActLogEditOwn, ownMember, true},
		{"组员不能改库存资料", member, ActInventoryManage, Resource{}, false},
		{"组员能登记领用", member, ActInventoryUse, Resource{}, true},
		{"副组长能盘点", deputy, ActInventoryManage, Resource{}, true},
		{"组员不能改可见范围", member, ActDocsVisibility, Resource{}, false},
		{"组长能改可见范围", leader, ActDocsVisibility, Resource{}, true},
		{"组员能写文档", member, ActDocsEdit, Resource{}, true},
		{"财务组员仍不能看他人联系方式", finance, ActPeopleSensitive, other, false},
		{"财务组员能看自己的联系方式", finance, ActPeopleSensitive, ownFinance, true},
		{"已退出的人不能做任何事", exited, ActPeopleList, Resource{}, false},
		{"未知权限拒绝", leader, "nope", Resource{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Can(tc.user, tc.action, tc.resource); got != tc.want {
				t.Fatalf("得到 %v", got)
			}
		})
	}
}

func TestParseRole(t *testing.T) {
	role, err := ParseRole("副组长")
	if err != nil || role != RoleDeputy {
		t.Fatalf("%s %v", role, err)
	}
	if _, err := ParseRole("老师"); err == nil {
		t.Fatal("应拒绝未知角色")
	}
}

func TestAllowLeaderChange(t *testing.T) {
	if err := AllowLeaderChange(1, true, RoleDeputy, StatusActive); err == nil {
		t.Fatal("最后一名组长不能降级")
	}
	if err := AllowLeaderChange(1, true, RoleLeader, StatusExited); err == nil {
		t.Fatal("最后一名组长不能退出")
	}
	if err := AllowLeaderChange(2, true, RoleDeputy, StatusActive); err != nil {
		t.Fatal(err)
	}
	if err := AllowLeaderChange(1, false, RoleMember, StatusExited); err != nil {
		t.Fatal(err)
	}
}
