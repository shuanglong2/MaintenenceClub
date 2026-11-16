package docs

import "testing"

func TestMerge(t *testing.T) {
	base := "a\nb\nc"
	got := Merge(base, "A\nb\nc", "a\nb\nC")
	if got != "A\nb\nC" {
		t.Fatalf("%q", got)
	}
	conflict := Merge(base, "a\nB\nc", "a\nb\nC")
	if conflict == "a\nB\nC" {
		t.Fatal("同一段两边都改了, 应留下冲突标记")
	}
	if diff := DiffText("a\nb", "a\nc"); diff != " a\n-b\n+c" {
		t.Fatalf("%q", diff)
	}
}
