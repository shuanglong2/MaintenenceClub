package people

import (
	"strings"
	"testing"
)

func TestParseImport(t *testing.T) {
	text := "姓名,登录名,角色\n张三,ZhangSan,组员\n=李四,+bad,副组长\n"
	rows, err := ParseImport(strings.NewReader(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Login != "zhangsan" || rows[1].Login != "+bad" {
		t.Fatalf("%+v", rows)
	}
	out, err := PasswordCSV([]Imported{{Name: "=张", Login: "zhang", Role: RoleMember, Password: "secret12"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(out), "\uFEFF") || !strings.Contains(string(out), "'=张") {
		t.Fatalf("%q", out)
	}
}

func TestParseImportRejectsHeader(t *testing.T) {
	if _, err := ParseImport(strings.NewReader("a,b,c\n1,2,3\n")); err == nil {
		t.Fatal("应拒绝错误表头")
	}
}
