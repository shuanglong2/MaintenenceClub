package migrate

import "testing"

func TestSplitSQL(t *testing.T) {
	got, err := splitSQL("CREATE TABLE a (id int);\nCREATE TABLE b (id int);\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("%#v", got)
	}

	got, err = splitSQL("INSERT INTO a VALUES ('a;b');")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "INSERT INTO a VALUES ('a;b')" {
		t.Fatalf("%#v", got)
	}

	got, err = splitSQL("SELECT 'it''s;fine';\nSELECT 2")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("%#v", got)
	}

	fn := "CREATE FUNCTION f() RETURNS void AS $$\nBEGIN\n  PERFORM 1;\nEND;\n$$ LANGUAGE plpgsql;"
	got, err = splitSQL(fn)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("%#v", got)
	}

	got, err = splitSQL("SELECT $1;\nSELECT $tag$ a; b $tag$;\nSELECT 1;")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0] != "SELECT $1" {
		t.Fatalf("%#v", got)
	}

	got, err = splitSQL("-- skip;\n/* ; */\nSELECT 1;")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "SELECT 1" {
		t.Fatalf("%#v", got)
	}

	if _, err = splitSQL("SELECT 'abc"); err == nil {
		t.Fatal("应报告未结束的字符串")
	}
	if _, err = splitSQL("SELECT $tag$ abc"); err == nil {
		t.Fatal("应报告未结束的美元引号")
	}
}
