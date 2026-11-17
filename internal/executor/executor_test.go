package executor

import (
	"testing"

	"github.com/OL486/minidb/internal/planner"
	"github.com/OL486/minidb/internal/sql"
	"github.com/OL486/minidb/internal/storage"
)

func mustStore(t *testing.T) *storage.Store {
	t.Helper()
	s := storage.NewStore()
	if err := s.CreateTable("users", []sql.ColumnDef{
		{Name: "id", Type: "INT"},
		{Name: "name", Type: "TEXT"},
		{Name: "age", Type: "INT"},
	}); err != nil {
		t.Fatalf("CreateTable failed: %v", err)
	}
	tbl, _ := s.Table("users")
	rows := [][]sql.Value{
		{sql.IntVal(1), sql.StrVal("Alice"), sql.IntVal(30)},
		{sql.IntVal(2), sql.StrVal("Bob"), sql.IntVal(25)},
		{sql.IntVal(3), sql.StrVal("Carol"), sql.Null()},
		{sql.IntVal(4), sql.StrVal("Dave"), sql.IntVal(40)},
		{sql.IntVal(5), sql.StrVal("Eve"), sql.IntVal(25)},
	}
	for _, r := range rows {
		if _, err := tbl.Insert(r); err != nil {
			t.Fatalf("Insert failed: %v", err)
		}
	}
	return s
}

// runQuery 是“解析 → 规划 → 搭链 → 执行”的全链路 helper。
func runQuery(t *testing.T, s *storage.Store, src string) ([][]sql.Value, []string) {
	t.Helper()
	stmt, err := sql.Parse(src)
	if err != nil {
		t.Fatalf("Parse(%q) failed: %v", src, err)
	}
	sel := stmt.(*sql.SelectStmt)
	node, cols, err := planner.Plan(sel, s)
	if err != nil {
		t.Fatalf("Plan(%q) failed: %v", src, err)
	}
	op, err := Build(node, s)
	if err != nil {
		t.Fatalf("Build(%q) failed: %v", src, err)
	}
	rows, err := Execute(op)
	if err != nil {
		t.Fatalf("Execute(%q) failed: %v", src, err)
	}
	return rows, cols
}

func rowStrings(rows [][]sql.Value) []string {
	var out []string
	for _, r := range rows {
		s := ""
		for i, v := range r {
			if i > 0 {
				s += "|"
			}
			s += v.String()
		}
		out = append(out, s)
	}
	return out
}

func expectRows(t *testing.T, got [][]sql.Value, want []string) {
	t.Helper()
	gotStr := rowStrings(got)
	if len(gotStr) != len(want) {
		t.Fatalf("rows = %v, want %v", gotStr, want)
	}
	for i := range want {
		if gotStr[i] != want[i] {
			t.Errorf("row %d = %q, want %q", i, gotStr[i], want[i])
		}
	}
}

func TestSeqScan(t *testing.T) {
	s := mustStore(t)
	rows, cols := runQuery(t, s, "SELECT * FROM users")
	if len(cols) != 3 || cols[0] != "id" {
		t.Errorf("cols = %v", cols)
	}
	expectRows(t, rows, []string{
		"1|Alice|30",
		"2|Bob|25",
		"3|Carol|NULL",
		"4|Dave|40",
		"5|Eve|25",
	})
}

func TestFilter(t *testing.T) {
	s := mustStore(t)
	rows, _ := runQuery(t, s, "SELECT id FROM users WHERE age > 26")
	expectRows(t, rows, []string{"1", "4"})

	// NULL 的 age 不满足 > 26，也不满足 <= 26（比较结果是 NULL，当 false）。
	rows, _ = runQuery(t, s, "SELECT id FROM users WHERE age <= 26")
	expectRows(t, rows, []string{"2", "5"})

	rows, _ = runQuery(t, s, "SELECT id FROM users WHERE age IS NULL")
	expectRows(t, rows, []string{"3"})

	rows, _ = runQuery(t, s, "SELECT id FROM users WHERE age IS NOT NULL AND name = 'Bob'")
	expectRows(t, rows, []string{"2"})

	// 三值逻辑：NULL AND FALSE = FALSE，NULL OR TRUE = TRUE。
	rows, _ = runQuery(t, s, "SELECT id FROM users WHERE age IS NULL OR age > 35")
	expectRows(t, rows, []string{"3", "4"})
}

func TestFilterNonBoolError(t *testing.T) {
	s := mustStore(t)
	stmt, _ := sql.Parse("SELECT id FROM users WHERE age")
	sel := stmt.(*sql.SelectStmt)
	node, _, err := planner.Plan(sel, s)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}
	op, err := Build(node, s)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if _, err := Execute(op); err == nil {
		t.Error("WHERE age (non-boolean) should fail")
	}
}

func TestProject(t *testing.T) {
	s := mustStore(t)
	rows, cols := runQuery(t, s, "SELECT name, age FROM users WHERE id = 1")
	if len(cols) != 2 || cols[0] != "name" || cols[1] != "age" {
		t.Errorf("cols = %v", cols)
	}
	expectRows(t, rows, []string{"Alice|30"})
}

func TestSort(t *testing.T) {
	s := mustStore(t)
	rows, _ := runQuery(t, s, "SELECT id, age FROM users ORDER BY age DESC")
	expectRows(t, rows, []string{
		"4|40",
		"1|30",
		"2|25",
		"5|25",
		"3|NULL",
	})
	// DESC 时 NULL 排最后（NULL 最小，反向后最大）；同 age 25 的两行保持原顺序（稳定排序）。
	rows, _ = runQuery(t, s, "SELECT id FROM users ORDER BY age ASC")
	expectRows(t, rows, []string{"3", "2", "5", "1", "4"})
	// ORDER BY 不在 SELECT 列表里的列。
	rows, _ = runQuery(t, s, "SELECT name FROM users ORDER BY age DESC LIMIT 2")
	expectRows(t, rows, []string{"Dave", "Alice"})
}

func TestLimit(t *testing.T) {
	s := mustStore(t)
	rows, _ := runQuery(t, s, "SELECT id FROM users LIMIT 0")
	if len(rows) != 0 {
		t.Errorf("LIMIT 0 = %v, want empty", rows)
	}
	rows, _ = runQuery(t, s, "SELECT id FROM users LIMIT 100")
	if len(rows) != 5 {
		t.Errorf("LIMIT 100 = %d rows, want 5", len(rows))
	}
}

func TestCount(t *testing.T) {
	s := mustStore(t)
	rows, cols := runQuery(t, s, "SELECT COUNT(*) FROM users")
	if len(cols) != 1 || cols[0] != "COUNT(*)" {
		t.Errorf("cols = %v", cols)
	}
	expectRows(t, rows, []string{"5"})

	// COUNT(age) 跳过 NULL。
	rows, _ = runQuery(t, s, "SELECT COUNT(age) FROM users")
	expectRows(t, rows, []string{"4"})

	rows, _ = runQuery(t, s, "SELECT COUNT(*) FROM users WHERE age > 100")
	expectRows(t, rows, []string{"0"})

	rows, cols = runQuery(t, s, "SELECT COUNT(*), COUNT(age) FROM users")
	if len(cols) != 2 {
		t.Errorf("cols = %v, want 2", cols)
	}
	expectRows(t, rows, []string{"5|4"})
}

func TestEval(t *testing.T) {
	cols := []string{"a", "b", "s"}
	row := sql.Row{sql.IntVal(7), sql.FloatVal(2.0), sql.StrVal("x")}
	cases := []struct {
		src  string
		want sql.Value
	}{
		{"a + 1", sql.IntVal(8)},
		{"a / 2", sql.IntVal(3)}, // 整数除法
		{"b + 1", sql.FloatVal(3.0)},
		{"a * b", sql.FloatVal(14.0)},
		{"-a", sql.IntVal(-7)},
		{"a > 5", sql.BoolVal(true)},
		{"s = 'x'", sql.BoolVal(true)},
		{"s < 'y'", sql.BoolVal(true)},
		{"a = 7.0", sql.BoolVal(true)}, // Int 与 Float 比较按数值
		{"NOT (a = 1)", sql.BoolVal(true)},
	}
	for _, c := range cases {
		stmt, err := sql.Parse("SELECT * FROM t WHERE " + c.src)
		if err != nil {
			t.Fatalf("Parse(%q) failed: %v", c.src, err)
		}
		sel := stmt.(*sql.SelectStmt)
		got, err := Eval(sel.Where, row, cols)
		if err != nil {
			t.Errorf("Eval(%q) failed: %v", c.src, err)
			continue
		}
		if sql.Compare(got, c.want) != 0 || got.Kind != c.want.Kind {
			t.Errorf("Eval(%q) = %+v, want %+v", c.src, got, c.want)
		}
	}
}

func TestEvalNull(t *testing.T) {
	cols := []string{"a"}
	row := sql.Row{sql.Null()}
	stmt, _ := sql.Parse("SELECT * FROM t WHERE a = 1")
	sel := stmt.(*sql.SelectStmt)
	got, err := Eval(sel.Where, row, cols)
	if err != nil {
		t.Fatalf("Eval failed: %v", err)
	}
	if !got.IsNull() {
		t.Errorf("NULL = 1 should be NULL, got %v", got)
	}
	stmt, _ = sql.Parse("SELECT * FROM t WHERE a + 1")
	sel = stmt.(*sql.SelectStmt)
	got, err = Eval(sel.Where, row, cols)
	if err != nil {
		t.Fatalf("Eval failed: %v", err)
	}
	if !got.IsNull() {
		t.Errorf("NULL + 1 should be NULL, got %v", got)
	}
}

func TestEvalDivByZero(t *testing.T) {
	cols := []string{"a"}
	row := sql.Row{sql.IntVal(1)}
	for _, src := range []string{"a / 0", "a / 0.0"} {
		stmt, _ := sql.Parse("SELECT * FROM t WHERE " + src)
		sel := stmt.(*sql.SelectStmt)
		if _, err := Eval(sel.Where, row, cols); err == nil {
			t.Errorf("Eval(%q) should fail (division by zero)", src)
		}
	}
}

func TestEvalUnknownColumn(t *testing.T) {
	cols := []string{"a"}
	row := sql.Row{sql.IntVal(1)}
	stmt, _ := sql.Parse("SELECT * FROM t WHERE nope = 1")
	sel := stmt.(*sql.SelectStmt)
	if _, err := Eval(sel.Where, row, cols); err == nil {
		t.Error("Eval with unknown column should fail")
	}
}
