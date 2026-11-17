// Package test 是 minidb 的端到端测试：用公开的 db API
// 跑 examples/basic.sql，断言每条查询的结果。
package test

import (
	"os"
	"strings"
	"testing"

	"github.com/OL486/minidb/internal/db"
	"github.com/OL486/minidb/internal/sql"
)

// TestBasicSQL 跑 examples/basic.sql，断言 5 条查询的结果。
func TestBasicSQL(t *testing.T) {
	data, err := os.ReadFile("../examples/basic.sql")
	if err != nil {
		t.Fatalf("read examples/basic.sql: %v", err)
	}
	d := db.New()
	var selects [][][]sql.Value
	for _, stmt := range sql.SplitStatements(string(data)) {
		trimmed := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(stmt), ";"))
		if trimmed == "" {
			continue
		}
		if sql.IsSelect(trimmed) {
			rows, _, err := d.Query(trimmed)
			if err != nil {
				t.Fatalf("Query(%q) failed: %v", trimmed, err)
			}
			selects = append(selects, rows)
			continue
		}
		if _, err := d.Exec(trimmed); err != nil {
			t.Fatalf("Exec(%q) failed: %v", trimmed, err)
		}
	}
	if len(selects) != 5 {
		t.Fatalf("got %d SELECT results, want 5", len(selects))
	}
	want := [][][]sql.Value{
		{
			{sql.IntVal(1), sql.StrVal("Alice"), sql.IntVal(30)},
			{sql.IntVal(2), sql.StrVal("Bob"), sql.IntVal(25)},
			{sql.IntVal(3), sql.StrVal("Carol"), sql.Null()},
			{sql.IntVal(4), sql.StrVal("Dave"), sql.IntVal(40)},
		},
		{
			{sql.StrVal("Dave"), sql.IntVal(40)},
			{sql.StrVal("Alice"), sql.IntVal(30)},
		},
		{{sql.IntVal(4)}},
		{{sql.IntVal(3)}},
		{{sql.StrVal("Carol")}},
	}
	for i := range want {
		if len(selects[i]) != len(want[i]) {
			t.Fatalf("query %d: got %d rows, want %d", i, len(selects[i]), len(want[i]))
		}
		for j := range want[i] {
			for k := range want[i][j] {
				if sql.Compare(selects[i][j][k], want[i][j][k]) != 0 {
					t.Errorf("query %d row %d col %d: got %v, want %v",
						i, j, k, selects[i][j][k], want[i][j][k])
				}
			}
		}
	}
}
