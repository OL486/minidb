// Package db 是 minidb 的顶层：一个进程内、单写者的内存数据库。
//
// M1 只有内存实现：New() 返回空库，数据随进程退出而丢（落盘是 M2 的事）。
// 单把 sync.Mutex 保护全部操作，不支持并发写，读也拿同一把锁，
// 简单正确第一（规划原文）。
package db

import (
	"fmt"
	"strings"
	"sync"

	"github.com/OL486/minidb/internal/executor"
	"github.com/OL486/minidb/internal/planner"
	"github.com/OL486/minidb/internal/sql"
	"github.com/OL486/minidb/internal/storage"
)

// DB 是一个内存数据库。
type DB struct {
	mu    sync.Mutex
	store *storage.Store
}

// New 返回一个空的内存数据库。
func New() *DB {
	return &DB{store: storage.NewStore()}
}

// Result 是 Exec 的结果：影响的行数与一条人类可读的消息。
type Result struct {
	RowsAffected int
	Message      string
}

// Exec 执行 CREATE TABLE 或 INSERT。SELECT 请用 Query。
func (d *DB) Exec(text string) (Result, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	stmt, err := sql.Parse(text)
	if err != nil {
		return Result{}, err
	}
	switch s := stmt.(type) {
	case *sql.CreateTableStmt:
		if err := d.store.CreateTable(s.Table, s.Cols); err != nil {
			return Result{}, err
		}
		return Result{Message: fmt.Sprintf("表 %s 已创建", s.Table)}, nil
	case *sql.InsertStmt:
		n, err := d.insert(s)
		if err != nil {
			return Result{}, err
		}
		return Result{RowsAffected: n, Message: fmt.Sprintf("插入 %d 行", n)}, nil
	case *sql.SelectStmt:
		return Result{}, fmt.Errorf("SELECT 返回行集，请用 Query 执行")
	default:
		return Result{}, fmt.Errorf("不支持的语句 %T", stmt)
	}
}

// insert 执行 INSERT：解析列列表、校验行长度与列类型、逐行写入。
func (d *DB) insert(s *sql.InsertStmt) (int, error) {
	tbl, ok := d.store.Table(s.Table)
	if !ok {
		return 0, fmt.Errorf("表 %s 不存在", s.Table)
	}
	// 确定每列要插到哪一列：没写列列表就是按建表顺序插全部列。
	var colIdx []int
	if len(s.Cols) == 0 {
		for i := range tbl.Cols {
			colIdx = append(colIdx, i)
		}
	} else {
		seen := make(map[int]bool, len(s.Cols))
		for _, name := range s.Cols {
			i := tbl.ColumnIndex(name)
			if i < 0 {
				return 0, fmt.Errorf("表 %s 没有列 %s", s.Table, name)
			}
			if seen[i] {
				return 0, fmt.Errorf("列 %s 在 INSERT 里出现了两次", name)
			}
			seen[i] = true
			colIdx = append(colIdx, i)
		}
	}
	for _, exprs := range s.Rows {
		if len(exprs) != len(colIdx) {
			return 0, fmt.Errorf("表 %s 这次插入 %d 列，但给了 %d 个值", s.Table, len(colIdx), len(exprs))
		}
		row := make([]sql.Value, len(tbl.Cols))
		for i := range row {
			row[i] = sql.Null()
		}
		for j, e := range exprs {
			lit, ok := e.(*sql.Literal)
			if !ok {
				return 0, fmt.Errorf("INSERT 的值必须是字面量")
			}
			v, err := checkValue(lit.Val, tbl.Cols[colIdx[j]].Type)
			if err != nil {
				return 0, fmt.Errorf("列 %s：%v", tbl.Cols[colIdx[j]].Name, err)
			}
			row[colIdx[j]] = v
		}
		if _, err := tbl.Insert(row); err != nil {
			return 0, err
		}
	}
	return len(s.Rows), nil
}

// checkValue 按列类型校验一个值：NULL 任何列都接受；
// INT 列只接受整数；FLOAT 列接受整数（提升为浮点）或浮点；
// TEXT 列只接受字符串；BOOL 列只接受布尔。类型不对返回错误，不猜测转换。
func checkValue(v sql.Value, colType string) (sql.Value, error) {
	if v.IsNull() {
		return v, nil
	}
	switch colType {
	case "INT":
		if v.Kind != sql.KindInt {
			return sql.Value{}, fmt.Errorf("需要 INT，得到 %s", v.String())
		}
		return v, nil
	case "FLOAT":
		if v.Kind == sql.KindInt {
			return sql.FloatVal(float64(v.Int)), nil
		}
		if v.Kind != sql.KindFloat {
			return sql.Value{}, fmt.Errorf("需要 FLOAT，得到 %s", v.String())
		}
		return v, nil
	case "TEXT":
		if v.Kind != sql.KindStr {
			return sql.Value{}, fmt.Errorf("需要 TEXT，得到 %s", v.String())
		}
		return v, nil
	case "BOOL":
		if v.Kind != sql.KindBool {
			return sql.Value{}, fmt.Errorf("需要 BOOL，得到 %s", v.String())
		}
		return v, nil
	default:
		return sql.Value{}, fmt.Errorf("未知的列类型 %q", colType)
	}
}

// Query 执行 SELECT，返回行、列名。非 SELECT 语句返回错误。
func (d *DB) Query(text string) ([][]sql.Value, []string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	stmt, err := sql.Parse(text)
	if err != nil {
		return nil, nil, err
	}
	sel, ok := stmt.(*sql.SelectStmt)
	if !ok {
		return nil, nil, fmt.Errorf("Query 只接受 SELECT 语句，得到 %T", stmt)
	}
	node, cols, err := planner.Plan(sel, d.store)
	if err != nil {
		return nil, nil, err
	}
	op, err := executor.Build(node, d.store)
	if err != nil {
		return nil, nil, err
	}
	rows, err := executor.Execute(op)
	if err != nil {
		return nil, nil, err
	}
	return rows, cols, nil
}

// Explain 返回 SELECT 的执行计划文本。M1 的计划只有 SeqScan，
// 但 Explain 的接口先立起来，M4 的 IndexScan 会直接复用。
func (d *DB) Explain(text string) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	stmt, err := sql.Parse(text)
	if err != nil {
		return "", err
	}
	sel, ok := stmt.(*sql.SelectStmt)
	if !ok {
		return "", fmt.Errorf("Explain 只接受 SELECT 语句")
	}
	node, _, err := planner.Plan(sel, d.store)
	if err != nil {
		return "", err
	}
	return planner.Explain(node), nil
}

// Tables 返回全部表名（按字母序）。
func (d *DB) Tables() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.store.Tables()
}

// Schema 返回一张表的列定义。表不存在时 ok 为 false。
func (d *DB) Schema(table string) ([]sql.ColumnDef, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	tbl, ok := d.store.Table(table)
	if !ok {
		return nil, false
	}
	cols := make([]sql.ColumnDef, len(tbl.Cols))
	copy(cols, tbl.Cols)
	return cols, true
}

// FormatRows 把查询结果格式化成人类可读的文本：
// 列名用 " | " 连接一行，行也一样，末尾一行 "(N rows)"。
// 空结果也打印列名行与 "(0 rows)"。
func FormatRows(cols []string, rows [][]sql.Value) string {
	var b strings.Builder
	b.WriteString(strings.Join(cols, " | "))
	b.WriteString("\n")
	for _, row := range rows {
		parts := make([]string, len(row))
		for i, v := range row {
			parts[i] = v.String()
		}
		b.WriteString(strings.Join(parts, " | "))
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "(%d rows)\n", len(rows))
	return b.String()
}
