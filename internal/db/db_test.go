package db

import (
	"strings"
	"testing"

	"github.com/OL486/minidb/internal/sql"
)

func TestDBEndToEnd(t *testing.T) {
	d := New()
	if _, err := d.Exec("CREATE TABLE users (id INT, name TEXT, age INT)"); err != nil {
		t.Fatalf("Exec CREATE failed: %v", err)
	}
	r, err := d.Exec("INSERT INTO users VALUES (1, 'Alice', 30), (2, 'Bob', NULL)")
	if err != nil {
		t.Fatalf("Exec INSERT failed: %v", err)
	}
	if r.RowsAffected != 2 {
		t.Errorf("RowsAffected = %d, want 2", r.RowsAffected)
	}
	rows, cols, err := d.Query("SELECT * FROM users WHERE age IS NULL")
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if len(cols) != 3 || len(rows) != 1 || rows[0][1].Str != "Bob" {
		t.Errorf("Query = %v %v, want 1 row Bob", cols, rows)
	}
	rows, _, err = d.Query("SELECT COUNT(*) FROM users")
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if len(rows) != 1 || rows[0][0].Int != 2 {
		t.Errorf("COUNT(*) = %v, want [[2]]", rows)
	}
}

func TestDBInsertTypeCheck(t *testing.T) {
	d := New()
	_, _ = d.Exec("CREATE TABLE t (a INT, b FLOAT, c TEXT, e BOOL)")
	cases := []string{
		"INSERT INTO t VALUES ('x', 1.0, 's', TRUE)",  // INT 列给了字符串
		"INSERT INTO t VALUES (1, 'x', 's', TRUE)",    // FLOAT 列给了字符串
		"INSERT INTO t VALUES (1, 1.0, 2, TRUE)",      // TEXT 列给了数字
		"INSERT INTO t VALUES (1, 1.0, 's', 1)",       // BOOL 列给了数字
		"INSERT INTO t VALUES (1, 1.0)",               // 值太少
		"INSERT INTO t VALUES (1, 1.0, 's', TRUE, 1)", // 值太多
		"INSERT INTO t (nope) VALUES (1)",             // 列不存在
		"INSERT INTO t (a, a) VALUES (1, 2)",          // 列重复
		"INSERT INTO nope VALUES (1)",                 // 表不存在
	}
	for _, src := range cases {
		if _, err := d.Exec(src); err == nil {
			t.Errorf("Exec(%q) should fail", src)
		}
	}
	// INT 插进 FLOAT 列要自动提升；NULL 任何列都接受。
	if _, err := d.Exec("INSERT INTO t VALUES (1, 2, NULL, NULL)"); err != nil {
		t.Errorf("Exec int-into-float / NULL should pass: %v", err)
	}
	rows, _, err := d.Query("SELECT b FROM t")
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if rows[0][0].Kind != sql.KindFloat || rows[0][0].Float != 2.0 {
		t.Errorf("b = %+v, want Float 2.0 (promoted)", rows[0][0])
	}
}

func TestDBErrors(t *testing.T) {
	d := New()
	if _, err := d.Exec("CREATE TABLE t (a INT)"); err != nil {
		t.Fatalf("Exec failed: %v", err)
	}
	if _, err := d.Exec("CREATE TABLE t (a INT)"); err == nil {
		t.Error("duplicate table should fail")
	}
	if _, _, err := d.Query("SELECT * FROM nope"); err == nil {
		t.Error("query unknown table should fail")
	}
	if _, err := d.Exec("SELECT * FROM t"); err == nil {
		t.Error("Exec SELECT should fail (use Query)")
	}
	if _, _, err := d.Query("CREATE TABLE t2 (a INT)"); err == nil {
		t.Error("Query CREATE should fail")
	}
	if _, err := d.Exec("SELECT * FROM"); err == nil {
		t.Error("parse error should fail, not panic")
	}
	if _, _, err := d.Query(""); err == nil {
		t.Error("empty query should fail")
	}
}

func TestDBTablesAndSchema(t *testing.T) {
	d := New()
	_, _ = d.Exec("CREATE TABLE b (x INT)")
	_, _ = d.Exec("CREATE TABLE a (y TEXT, z BOOL)")
	if got := d.Tables(); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("Tables() = %v, want [a b]", got)
	}
	cols, ok := d.Schema("a")
	if !ok || len(cols) != 2 || cols[0].Name != "y" || cols[1].Type != "BOOL" {
		t.Errorf("Schema(a) = %v %v", cols, ok)
	}
	if _, ok := d.Schema("nope"); ok {
		t.Error("Schema(nope) should not be found")
	}
}

func TestDBExplain(t *testing.T) {
	d := New()
	_, _ = d.Exec("CREATE TABLE t (a INT)")
	plan, err := d.Explain("SELECT a FROM t WHERE a > 1")
	if err != nil {
		t.Fatalf("Explain failed: %v", err)
	}
	if !strings.Contains(plan, "SeqScan(table=t)") || !strings.Contains(plan, "Filter") {
		t.Errorf("Explain = %q", plan)
	}
}

func TestFormatRows(t *testing.T) {
	out := FormatRows([]string{"id", "name"}, [][]sql.Value{
		{sql.IntVal(1), sql.StrVal("a")},
		{sql.IntVal(2), sql.Null()},
	})
	want := "id | name\n1 | a\n2 | NULL\n(2 rows)\n"
	if out != want {
		t.Errorf("FormatRows =\n%q\nwant\n%q", out, want)
	}
	out = FormatRows([]string{"id"}, nil)
	if out != "id\n(0 rows)\n" {
		t.Errorf("FormatRows empty =\n%q", out)
	}
}
