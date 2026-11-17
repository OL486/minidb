package planner

import (
	"strings"
	"testing"

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
	return s
}

func mustParse(t *testing.T, src string) *sql.SelectStmt {
	t.Helper()
	stmt, err := sql.Parse(src)
	if err != nil {
		t.Fatalf("Parse(%q) failed: %v", src, err)
	}
	sel, ok := stmt.(*sql.SelectStmt)
	if !ok {
		t.Fatalf("Parse(%q) is %T, want *SelectStmt", src, stmt)
	}
	return sel
}

func TestPlanShape(t *testing.T) {
	s := mustStore(t)
	node, cols, err := Plan(mustParse(t, "SELECT name, age FROM users WHERE age > 20 ORDER BY age DESC LIMIT 5"), s)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}
	if len(cols) != 2 || cols[0] != "name" || cols[1] != "age" {
		t.Errorf("cols = %v, want [name age]", cols)
	}
	// 根节点必须是 Limit，链是 Limit -> Project -> Sort -> Filter -> SeqScan。
	lim, ok := node.(*LimitNode)
	if !ok {
		t.Fatalf("root is %T, want *LimitNode", node)
	}
	if lim.N != 5 {
		t.Errorf("Limit = %d, want 5", lim.N)
	}
	proj, ok := lim.Child.(*ProjectNode)
	if !ok {
		t.Fatalf("Limit child is %T, want *ProjectNode", lim.Child)
	}
	sortNode, ok := proj.Child.(*SortNode)
	if !ok {
		t.Fatalf("Project child is %T, want *SortNode", proj.Child)
	}
	if len(sortNode.Keys) != 1 || !sortNode.Keys[0].Desc {
		t.Errorf("Sort keys = %+v", sortNode.Keys)
	}
	filt, ok := sortNode.Child.(*FilterNode)
	if !ok {
		t.Fatalf("Sort child is %T, want *FilterNode", sortNode.Child)
	}
	if _, ok := filt.Child.(*SeqScanNode); !ok {
		t.Fatalf("Filter child is %T, want *SeqScanNode", filt.Child)
	}
}

func TestPlanStarAndNoClauses(t *testing.T) {
	s := mustStore(t)
	node, cols, err := Plan(mustParse(t, "SELECT * FROM users"), s)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}
	if len(cols) != 3 || cols[0] != "id" || cols[1] != "name" || cols[2] != "age" {
		t.Errorf("cols = %v, want [id name age]", cols)
	}
	proj, ok := node.(*ProjectNode)
	if !ok {
		t.Fatalf("root is %T, want *ProjectNode", node)
	}
	if _, ok := proj.Child.(*SeqScanNode); !ok {
		t.Fatalf("Project child is %T, want *SeqScanNode", proj.Child)
	}
}

func TestPlanCount(t *testing.T) {
	s := mustStore(t)
	node, cols, err := Plan(mustParse(t, "SELECT COUNT(*), COUNT(age) FROM users WHERE age > 20"), s)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}
	if len(cols) != 2 || cols[0] != "COUNT(*)" || cols[1] != "COUNT(age)" {
		t.Errorf("cols = %v, want [COUNT(*) COUNT(age)]", cols)
	}
	count, ok := node.(*CountNode)
	if !ok {
		t.Fatalf("root is %T, want *CountNode", node)
	}
	if _, ok := count.Child.(*FilterNode); !ok {
		t.Fatalf("Count child is %T, want *FilterNode", count.Child)
	}
}

func TestPlanErrors(t *testing.T) {
	s := mustStore(t)
	cases := []string{
		"SELECT * FROM nope",                                    // 表不存在
		"SELECT no_col FROM users",                              // 列不存在
		"SELECT * FROM users WHERE no_col = 1",                  // WHERE 列不存在
		"SELECT * FROM users ORDER BY no_col",                   // ORDER BY 列不存在
		"SELECT COUNT(*), name FROM users",                      // COUNT 混普通列
		"SELECT COUNT(no_col) FROM users",                       // COUNT 列不存在
		"SELECT COUNT(*) FROM users ORDER BY age",               // COUNT 配 ORDER BY
		"SELECT * FROM users JOIN users ON users.id = users.id", // JOIN
	}
	for _, src := range cases {
		if _, _, err := Plan(mustParse(t, src), s); err == nil {
			t.Errorf("Plan(%q) should fail", src)
		}
	}
}

func TestExplain(t *testing.T) {
	s := mustStore(t)
	node, _, err := Plan(mustParse(t, "SELECT name FROM users WHERE age > 20 ORDER BY age DESC LIMIT 10"), s)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}
	got := Explain(node)
	want := "SeqScan(table=users) -> Filter((age > 20)) -> Sort(age DESC) -> Project(name) -> Limit(10)"
	if got != want {
		t.Errorf("Explain = %q\nwant %q", got, want)
	}
	if !strings.Contains(Explain(mustPlanCount(t, s)), "COUNT(*)") {
		t.Error("Explain of COUNT plan should contain COUNT(*)")
	}
}

func mustPlanCount(t *testing.T, s *storage.Store) Node {
	t.Helper()
	node, _, err := Plan(mustParse(t, "SELECT COUNT(*) FROM users"), s)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}
	return node
}
