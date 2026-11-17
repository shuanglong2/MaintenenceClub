package files

import "testing"

func TestDisguisedUpload(t *testing.T) {
	if _, ok := allowedExt("photo.jpg", "application/x-msdownload"); ok {
		t.Fatal("内容类型与扩展名不符")
	}
	if _, ok := allowedExt("run.exe", "application/octet-stream"); ok {
		t.Fatal("不允许的扩展名")
	}
	if ext, ok := allowedExt("note.txt", "text/plain; charset=utf-8"); !ok || ext != ".txt" {
		t.Fatalf("%s %v", ext, ok)
	}
}
