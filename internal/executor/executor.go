// Package executor 提供 minidb 的查询执行层：迭代器模型的算子链。
//
// 每个算子实现 Operator 接口：Next() 逐行吐数据，ok=false 表示结束。
// 算子链由 planner 的计划树通过 Build 搭起来，Execute 把链跑完收成行。
package executor

import (
	"fmt"
	"sort"
	"strings"

	"github.com/OL486/minidb/internal/planner"
	"github.com/OL486/minidb/internal/sql"
	"github.com/OL486/minidb/internal/storage"
)

// maxSortRows 是 Sort 算子一次能排的行数上限，超过就报错，
// 不做外排（规划原文）。
const maxSortRows = 100000

// Operator 是迭代器算子：Next 每次返回一行，ok=false 表示结束。
// 错误直接上浮，调用方会附上算子名。
type Operator interface {
	Next() (sql.Row, bool, error)
	// Columns 返回本算子输出的列名（按输出顺序）。
	Columns() []string
}

// Build 按计划树搭算子链。
func Build(n planner.Node, store *storage.Store) (Operator, error) {
	switch node := n.(type) {
	case *planner.SeqScanNode:
		tbl, ok := store.Table(node.Table)
		if !ok {
			return nil, fmt.Errorf("SeqScan: 表 %s 不存在", node.Table)
		}
		rows, err := tbl.Scan()
		if err != nil {
			return nil, fmt.Errorf("SeqScan: %v", err)
		}
		return &seqScanOp{tbl: tbl, rows: rows, cols: tbl.ColumnNames()}, nil
	case *planner.FilterNode:
		child, err := Build(node.Child, store)
		if err != nil {
			return nil, err
		}
		return &filterOp{child: child, pred: node.Pred, cols: child.Columns()}, nil
	case *planner.ProjectNode:
		child, err := Build(node.Child, store)
		if err != nil {
			return nil, err
		}
		inCols := child.Columns()
		outCols := make([]string, 0, len(node.Items))
		for _, item := range node.Items {
			if item.Star {
				outCols = append(outCols, inCols...)
			} else {
				outCols = append(outCols, item.Col)
			}
		}
		return &projectOp{child: child, items: node.Items, inCols: inCols, outCols: outCols}, nil
	case *planner.SortNode:
		child, err := Build(node.Child, store)
		if err != nil {
			return nil, err
		}
		return &sortOp{child: child, keys: node.Keys, cols: child.Columns()}, nil
	case *planner.LimitNode:
		child, err := Build(node.Child, store)
		if err != nil {
			return nil, err
		}
		return &limitOp{child: child, n: node.N, cols: child.Columns()}, nil
	case *planner.CountNode:
		child, err := Build(node.Child, store)
		if err != nil {
			return nil, err
		}
		outCols := make([]string, len(node.Cols))
		for i, c := range node.Cols {
			outCols[i] = "COUNT(" + c + ")"
		}
		return &countOp{child: child, cols: node.Cols, inCols: child.Columns(), outCols: outCols}, nil
	default:
		return nil, fmt.Errorf("未知的计划节点 %T", n)
	}
}

// Execute 把算子链跑完，收成全部行。
func Execute(op Operator) ([][]sql.Value, error) {
	var rows [][]sql.Value
	for {
		row, ok, err := op.Next()
		if err != nil {
			return nil, err
		}
		if !ok {
			return rows, nil
		}
		rows = append(rows, row)
	}
}

// seqScanOp 顺序扫描一张表。
type seqScanOp struct {
	tbl  *storage.Table
	rows [][]sql.Value
	idx  int
	cols []string
}

func (o *seqScanOp) Columns() []string { return o.cols }

func (o *seqScanOp) Next() (sql.Row, bool, error) {
	if o.idx >= len(o.rows) {
		return nil, false, nil
	}
	row := o.rows[o.idx]
	o.idx++
	return row, true, nil
}

// filterOp 只放行谓词为真（Bool true）的行；谓词为 NULL 的行丢弃；
// 谓词结果不是布尔也不是 NULL 时报错。
type filterOp struct {
	child Operator
	pred  sql.Expr
	cols  []string
}

func (o *filterOp) Columns() []string { return o.cols }

func (o *filterOp) Next() (sql.Row, bool, error) {
	for {
		row, ok, err := o.child.Next()
		if err != nil {
			return nil, false, fmt.Errorf("Filter: %v", err)
		}
		if !ok {
			return nil, false, nil
		}
		v, err := Eval(o.pred, row, o.cols)
		if err != nil {
			return nil, false, fmt.Errorf("Filter: %v", err)
		}
		if v.Kind == sql.KindBool && v.Bool {
			return row, true, nil
		}
		if v.Kind != sql.KindBool && v.Kind != sql.KindNull {
			return nil, false, fmt.Errorf("Filter: WHERE 子句的结果必须是布尔值，得到 %s", v.String())
		}
		// NULL 当 false，丢弃该行。
	}
}

// projectOp 按 SELECT 列表取列。Star 展开成输入的全部列。
type projectOp struct {
	child   Operator
	items   []sql.SelectItem
	inCols  []string
	outCols []string
}

func (o *projectOp) Columns() []string { return o.outCols }

func (o *projectOp) Next() (sql.Row, bool, error) {
	row, ok, err := o.child.Next()
	if err != nil {
		return nil, false, fmt.Errorf("Project: %v", err)
	}
	if !ok {
		return nil, false, nil
	}
	out := make(sql.Row, 0, len(o.outCols))
	for _, item := range o.items {
		if item.Star {
			out = append(out, row...)
			continue
		}
		i := columnIndex(item.Col, o.inCols)
		if i < 0 {
			return nil, false, fmt.Errorf("Project: 列 %s 不存在", item.Col)
		}
		out = append(out, row[i])
	}
	return out, true, nil
}

// sortOp 把孩子全部读进内存排序。超过 maxSortRows 行直接报错。
type sortOp struct {
	child  Operator
	keys   []sql.OrderTerm
	cols   []string
	rows   [][]sql.Value
	idx    int
	loaded bool
}

func (o *sortOp) Columns() []string { return o.cols }

func (o *sortOp) Next() (sql.Row, bool, error) {
	if !o.loaded {
		rows, err := Execute(o.child)
		if err != nil {
			return nil, false, fmt.Errorf("Sort: %v", err)
		}
		if len(rows) > maxSortRows {
			return nil, false, fmt.Errorf("Sort: 要排序 %d 行，超过上限 %d，不做外排", len(rows), maxSortRows)
		}
		keyIdx := make([]int, len(o.keys))
		for i, k := range o.keys {
			j := columnIndex(k.Col, o.cols)
			if j < 0 {
				return nil, false, fmt.Errorf("Sort: 列 %s 不存在", k.Col)
			}
			keyIdx[i] = j
		}
		sort.SliceStable(rows, func(a, b int) bool {
			for i, j := range keyIdx {
				c := sql.Compare(rows[a][j], rows[b][j])
				if c != 0 {
					if o.keys[i].Desc {
						return c > 0
					}
					return c < 0
				}
			}
			return false
		})
		o.rows = rows
		o.loaded = true
	}
	if o.idx >= len(o.rows) {
		return nil, false, nil
	}
	row := o.rows[o.idx]
	o.idx++
	return row, true, nil
}

// limitOp 只取前 N 行。
type limitOp struct {
	child Operator
	n     int
	seen  int
	cols  []string
}

func (o *limitOp) Columns() []string { return o.cols }

func (o *limitOp) Next() (sql.Row, bool, error) {
	if o.seen >= o.n {
		return nil, false, nil
	}
	row, ok, err := o.child.Next()
	if err != nil {
		return nil, false, fmt.Errorf("Limit: %v", err)
	}
	if !ok {
		return nil, false, nil
	}
	o.seen++
	return row, true, nil
}

// countOp 聚合计数：第一次 Next 时把孩子跑完计数，返回一行；
// 之后 Next 返回结束。COUNT(*) 计全部行，COUNT(列) 只计该列非 NULL 的行。
type countOp struct {
	child   Operator
	cols    []string // "*" 或列名
	inCols  []string
	outCols []string
	done    bool
}

func (o *countOp) Columns() []string { return o.outCols }

func (o *countOp) Next() (sql.Row, bool, error) {
	if o.done {
		return nil, false, nil
	}
	o.done = true
	counts := make([]int64, len(o.cols))
	colIdx := make([]int, len(o.cols))
	for i, c := range o.cols {
		if c == "*" {
			colIdx[i] = -1
			continue
		}
		j := columnIndex(c, o.inCols)
		if j < 0 {
			return nil, false, fmt.Errorf("Count: 列 %s 不存在", c)
		}
		colIdx[i] = j
	}
	for {
		row, ok, err := o.child.Next()
		if err != nil {
			return nil, false, fmt.Errorf("Count: %v", err)
		}
		if !ok {
			break
		}
		for i, j := range colIdx {
			if j == -1 || !row[j].IsNull() {
				counts[i]++
			}
		}
	}
	out := make(sql.Row, len(counts))
	for i, c := range counts {
		out[i] = sql.IntVal(c)
	}
	return out, true, nil
}

// columnIndex 在列名表里找列下标。先精确匹配（不区分大小写），
// 再按 '.' 后的后缀匹配（限定列名 a.b 用）。找不到返回 -1。
func columnIndex(name string, cols []string) int {
	for i, c := range cols {
		if strings.EqualFold(c, name) {
			return i
		}
	}
	if j := strings.LastIndex(name, "."); j >= 0 {
		suffix := name[j+1:]
		for i, c := range cols {
			if strings.EqualFold(c, suffix) {
				return i
			}
		}
	}
	return -1
}

// Eval 对一行求表达式的值。行按 cols 的顺序排列。
//
// 三值逻辑（SQL 标准）：比较与算术里任一操作数为 NULL 时结果为 NULL；
// AND/OR/NOT 按三值真值表算；WHERE 只保留结果为 Bool true 的行。
func Eval(e sql.Expr, row sql.Row, cols []string) (sql.Value, error) {
	switch ex := e.(type) {
	case *sql.Literal:
		return ex.Val, nil
	case *sql.ColumnRef:
		i := columnIndex(ex.Name, cols)
		if i < 0 {
			return sql.Value{}, fmt.Errorf("列 %s 不存在", ex.Name)
		}
		return row[i], nil
	case *sql.UnaryExpr:
		v, err := Eval(ex.X, row, cols)
		if err != nil {
			return sql.Value{}, err
		}
		switch ex.Op {
		case "-":
			if v.IsNull() {
				return sql.Null(), nil
			}
			if !v.IsNumeric() {
				return sql.Value{}, fmt.Errorf("一元 - 需要数字，得到 %s", v.String())
			}
			if v.Kind == sql.KindInt {
				return sql.IntVal(-v.Int), nil
			}
			return sql.FloatVal(-v.Float), nil
		case "NOT":
			if v.IsNull() {
				return sql.Null(), nil
			}
			if v.Kind != sql.KindBool {
				return sql.Value{}, fmt.Errorf("NOT 需要布尔值，得到 %s", v.String())
			}
			return sql.BoolVal(!v.Bool), nil
		default:
			return sql.Value{}, fmt.Errorf("未知的一元运算符 %q", ex.Op)
		}
	case *sql.IsNullExpr:
		v, err := Eval(ex.X, row, cols)
		if err != nil {
			return sql.Value{}, err
		}
		isNull := v.IsNull()
		if ex.Neg {
			return sql.BoolVal(!isNull), nil
		}
		return sql.BoolVal(isNull), nil
	case *sql.BinaryExpr:
		return evalBinary(ex, row, cols)
	default:
		return sql.Value{}, fmt.Errorf("未知的表达式类型 %T", e)
	}
}

// evalBinary 求二元表达式的值。
func evalBinary(e *sql.BinaryExpr, row sql.Row, cols []string) (sql.Value, error) {
	// AND / OR 要先拿到两边的值再按三值真值表算。
	if e.Op == "AND" || e.Op == "OR" {
		l, err := Eval(e.Left, row, cols)
		if err != nil {
			return sql.Value{}, err
		}
		r, err := Eval(e.Right, row, cols)
		if err != nil {
			return sql.Value{}, err
		}
		return evalLogic(e.Op, l, r)
	}
	l, err := Eval(e.Left, row, cols)
	if err != nil {
		return sql.Value{}, err
	}
	r, err := Eval(e.Right, row, cols)
	if err != nil {
		return sql.Value{}, err
	}
	switch e.Op {
	case "=", "!=", "<", "<=", ">", ">=":
		if l.IsNull() || r.IsNull() {
			return sql.Null(), nil
		}
		c := sql.Compare(l, r)
		switch e.Op {
		case "=":
			return sql.BoolVal(c == 0), nil
		case "!=":
			return sql.BoolVal(c != 0), nil
		case "<":
			return sql.BoolVal(c < 0), nil
		case "<=":
			return sql.BoolVal(c <= 0), nil
		case ">":
			return sql.BoolVal(c > 0), nil
		default: // ">="
			return sql.BoolVal(c >= 0), nil
		}
	case "+", "-", "*", "/":
		return sql.Arith(e.Op, l, r)
	default:
		return sql.Value{}, fmt.Errorf("未知的二元运算符 %q", e.Op)
	}
}

// evalLogic 按三值真值表算 AND / OR。操作数必须是布尔或 NULL。
func evalLogic(op string, l, r sql.Value) (sql.Value, error) {
	if (l.Kind != sql.KindBool && !l.IsNull()) || (r.Kind != sql.KindBool && !r.IsNull()) {
		return sql.Value{}, fmt.Errorf("%s 需要布尔值，得到 %s 和 %s", op, l.String(), r.String())
	}
	lb, lNull := l.Bool, l.IsNull()
	rb, rNull := r.Bool, r.IsNull()
	switch op {
	case "AND":
		if (!lNull && !lb) || (!rNull && !rb) {
			return sql.BoolVal(false), nil
		}
		if lNull || rNull {
			return sql.Null(), nil
		}
		return sql.BoolVal(true), nil
	case "OR":
		if (!lNull && lb) || (!rNull && rb) {
			return sql.BoolVal(true), nil
		}
		if lNull || rNull {
			return sql.Null(), nil
		}
		return sql.BoolVal(false), nil
	default:
		return sql.Value{}, fmt.Errorf("未知的逻辑运算符 %q", op)
	}
}
