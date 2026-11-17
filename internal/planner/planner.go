// Package planner 把 SELECT 语句翻译成执行计划。
//
// M1 只做两件事（规划原文）：谓词下推——WHERE 里对单列的等值条件
// 如果该列有索引就生成 IndexScan（M1 没有索引，所以永远是 SeqScan）；
// ORDER BY 走索引时省掉 Sort（M1 没有索引，所以有 ORDER BY 就一定加 Sort）。
// 没有代价模型，不做连接顺序优化。JOIN 在这里被拒绝（M5 才做）。
package planner

import (
	"fmt"
	"strings"

	"github.com/OL486/minidb/internal/sql"
	"github.com/OL486/minidb/internal/storage"
)

// Node 是计划树的一个节点。
type Node interface {
	node()
}

// SeqScanNode 是全表扫描：按行号顺序读一张表的全部行。
type SeqScanNode struct {
	Table string
}

func (*SeqScanNode) node() {}

// FilterNode 是过滤：只放行谓词为真（Bool true）的行；
// 谓词为 NULL 或非布尔时，执行层会报错。
type FilterNode struct {
	Child Node
	Pred  sql.Expr
}

func (*FilterNode) node() {}

// ProjectNode 是投影：按 SELECT 列表取列。Items 里只有 Star 或 Col，
// COUNT 在 CountNode 里处理，不进这里。
type ProjectNode struct {
	Child Node
	Items []sql.SelectItem
}

func (*ProjectNode) node() {}

// SortNode 是内存排序。超过 maxSortRows 行时执行层直接报错，不做外排。
type SortNode struct {
	Child Node
	Keys  []sql.OrderTerm
}

func (*SortNode) node() {}

// LimitNode 是取前 N 行。
type LimitNode struct {
	Child Node
	N     int
}

func (*LimitNode) node() {}

// CountNode 是聚合：只支持 COUNT(*) 与 COUNT(列)，一行输出一列，
// 一条语句里可以有多个 COUNT（输出多列）。不做 GROUP BY。
type CountNode struct {
	Child Node
	Cols  []string // "*" 或列名
}

func (*CountNode) node() {}

// Plan 把 SELECT 语句翻译成计划树，同时返回输出列名。
// 表不存在、列不存在、COUNT 与普通列混用、JOIN、COUNT 配 ORDER BY
// 都会在这里返回错误。
func Plan(stmt *sql.SelectStmt, store *storage.Store) (Node, []string, error) {
	tbl, ok := store.Table(stmt.Table)
	if !ok {
		return nil, nil, fmt.Errorf("表 %s 不存在", stmt.Table)
	}
	if stmt.Join != nil {
		return nil, nil, fmt.Errorf("JOIN 暂不支持，M5 才做")
	}
	cols := tbl.ColumnNames()
	if stmt.Where != nil {
		if err := checkColumns(stmt.Where, tbl); err != nil {
			return nil, nil, err
		}
	}
	for _, term := range stmt.OrderBy {
		if tbl.ColumnIndex(term.Col) < 0 {
			return nil, nil, fmt.Errorf("ORDER BY 的列 %s 不存在", term.Col)
		}
	}
	var countCols []string
	var outCols []string
	for _, item := range stmt.Cols {
		switch {
		case item.Star:
			outCols = append(outCols, cols...)
		case item.Count:
			if item.CountCol != "*" && tbl.ColumnIndex(item.CountCol) < 0 {
				return nil, nil, fmt.Errorf("COUNT 的列 %s 不存在", item.CountCol)
			}
			countCols = append(countCols, item.CountCol)
			outCols = append(outCols, "COUNT("+item.CountCol+")")
		default:
			if tbl.ColumnIndex(item.Col) < 0 {
				return nil, nil, fmt.Errorf("列 %s 不存在", item.Col)
			}
			outCols = append(outCols, item.Col)
		}
	}
	hasCount := len(countCols) > 0
	if hasCount && len(stmt.Cols) != len(countCols) {
		return nil, nil, fmt.Errorf("COUNT 不能和普通列混在同一个 SELECT 里")
	}

	var node Node = &SeqScanNode{Table: stmt.Table}
	if stmt.Where != nil {
		node = &FilterNode{Child: node, Pred: stmt.Where}
	}
	if hasCount {
		if len(stmt.OrderBy) > 0 {
			return nil, nil, fmt.Errorf("ORDER BY 不能和 COUNT 一起用")
		}
		node = &CountNode{Child: node, Cols: countCols}
		if stmt.Limit >= 0 {
			node = &LimitNode{Child: node, N: stmt.Limit}
		}
		return node, outCols, nil
	}
	if len(stmt.OrderBy) > 0 {
		node = &SortNode{Child: node, Keys: stmt.OrderBy}
	}
	node = &ProjectNode{Child: node, Items: stmt.Cols}
	if stmt.Limit >= 0 {
		node = &LimitNode{Child: node, N: stmt.Limit}
	}
	return node, outCols, nil
}

// checkColumns 检查表达式里引用的列是否都在表里。
func checkColumns(e sql.Expr, tbl *storage.Table) error {
	switch ex := e.(type) {
	case *sql.ColumnRef:
		if tbl.ColumnIndex(ex.Name) < 0 {
			return fmt.Errorf("列 %s 不存在", ex.Name)
		}
		return nil
	case *sql.BinaryExpr:
		if err := checkColumns(ex.Left, tbl); err != nil {
			return err
		}
		return checkColumns(ex.Right, tbl)
	case *sql.UnaryExpr:
		return checkColumns(ex.X, tbl)
	case *sql.IsNullExpr:
		return checkColumns(ex.X, tbl)
	case *sql.Literal:
		return nil
	default:
		return fmt.Errorf("未知的表达式类型 %T", e)
	}
}

// Explain 把计划树渲染成一行纯文本，例如：
//
//	SeqScan(table=users) -> Filter(age > 20) -> Sort(age DESC) -> Project(name) -> Limit(10)
func Explain(n Node) string {
	var parts []string
	var walk func(Node)
	walk = func(n Node) {
		switch node := n.(type) {
		case *SeqScanNode:
			parts = append(parts, fmt.Sprintf("SeqScan(table=%s)", node.Table))
		case *FilterNode:
			walk(node.Child)
			parts = append(parts, fmt.Sprintf("Filter(%s)", node.Pred.String()))
		case *ProjectNode:
			walk(node.Child)
			var cols []string
			for _, item := range node.Items {
				if item.Star {
					cols = append(cols, "*")
				} else {
					cols = append(cols, item.Col)
				}
			}
			parts = append(parts, fmt.Sprintf("Project(%s)", strings.Join(cols, ", ")))
		case *SortNode:
			walk(node.Child)
			var keys []string
			for _, k := range node.Keys {
				if k.Desc {
					keys = append(keys, k.Col+" DESC")
				} else {
					keys = append(keys, k.Col)
				}
			}
			parts = append(parts, fmt.Sprintf("Sort(%s)", strings.Join(keys, ", ")))
		case *LimitNode:
			walk(node.Child)
			parts = append(parts, fmt.Sprintf("Limit(%d)", node.N))
		case *CountNode:
			walk(node.Child)
			var cols []string
			for _, c := range node.Cols {
				cols = append(cols, "COUNT("+c+")")
			}
			parts = append(parts, strings.Join(cols, ", "))
		}
	}
	walk(n)
	return strings.Join(parts, " -> ")
}
