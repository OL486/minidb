package sql

import (
	"fmt"
	"strings"
)

// Stmt 是 SQL 语句的根接口。
type Stmt interface {
	stmt()
}

// ColumnDef 是一列的定义：列名与声明类型。
// Type 规范化为 "INT"、"FLOAT"、"TEXT"、"BOOL" 四种之一。
type ColumnDef struct {
	Name string
	Type string
}

// CreateTableStmt 是 CREATE TABLE 语句。
type CreateTableStmt struct {
	Table string
	Cols  []ColumnDef
}

func (*CreateTableStmt) stmt() {}

// InsertStmt 是 INSERT INTO 语句。Cols 为空表示按建表顺序插入全部列。
// Rows 的每个表达式必须是字面量（解析器保证）。
type InsertStmt struct {
	Table string
	Cols  []string
	Rows  [][]Expr
}

func (*InsertStmt) stmt() {}

// SelectItem 是 SELECT 列表中的一项：星号、一列，或 COUNT 聚合。
type SelectItem struct {
	Star     bool   // SELECT *
	Col      string // 普通列名
	Count    bool   // COUNT(...)
	CountCol string // "*" 或列名
}

// OrderTerm 是 ORDER BY 中的一项。
type OrderTerm struct {
	Col  string
	Desc bool
}

// JoinClause 是 JOIN 子句。M1 只解析它，规划层会拒绝执行（M5 才做）。
type JoinClause struct {
	Table string
	On    Expr
}

// SelectStmt 是 SELECT 语句。Limit 为 -1 表示没有 LIMIT。
type SelectStmt struct {
	Cols    []SelectItem
	Table   string
	Join    *JoinClause
	Where   Expr
	OrderBy []OrderTerm
	Limit   int
}

func (*SelectStmt) stmt() {}

// Expr 是表达式接口。
type Expr interface {
	expr()
	// String 把表达式还原成 SQL 文本（用于 Explain 与错误信息）。
	String() string
}

// BinaryExpr 是二元表达式：比较、逻辑、算术。
type BinaryExpr struct {
	Op    string // "=", "!=", "<", "<=", ">", ">=", "AND", "OR", "+", "-", "*", "/"
	Left  Expr
	Right Expr
}

func (*BinaryExpr) expr() {}

// String 还原二元表达式。
func (e *BinaryExpr) String() string {
	return "(" + e.Left.String() + " " + e.Op + " " + e.Right.String() + ")"
}

// UnaryExpr 是一元表达式：一元负号或 NOT。
type UnaryExpr struct {
	Op string // "-" 或 "NOT"
	X  Expr
}

func (*UnaryExpr) expr() {}

// String 还原一元表达式。
func (e *UnaryExpr) String() string {
	if e.Op == "NOT" {
		return "(NOT " + e.X.String() + ")"
	}
	return "(-" + e.X.String() + ")"
}

// IsNullExpr 是 IS [NOT] NULL 表达式。
type IsNullExpr struct {
	X   Expr
	Neg bool // true 表示 IS NOT NULL
}

func (*IsNullExpr) expr() {}

// String 还原 IS NULL 表达式。
func (e *IsNullExpr) String() string {
	if e.Neg {
		return "(" + e.X.String() + " IS NOT NULL)"
	}
	return "(" + e.X.String() + " IS NULL)"
}

// ColumnRef 是列引用。
type ColumnRef struct {
	Name string
}

func (*ColumnRef) expr() {}

// String 还原列引用。
func (e *ColumnRef) String() string { return e.Name }

// Literal 是字面量。
type Literal struct {
	Val Value
}

func (*Literal) expr() {}

// String 还原字面量：字符串加单引号，单引号本身转义为两个单引号。
func (e *Literal) String() string {
	if e.Val.Kind == KindStr {
		return "'" + strings.ReplaceAll(e.Val.Str, "'", "''") + "'"
	}
	return e.Val.String()
}

// ParseError 是解析失败的错误，带行列与源码摘录。
// 格式参考 minilang 的 diag：先一行“第 X 行第 Y 列：原因”，
// 再一行源码，最后是一行指向出错位置的 ^。
type ParseError struct {
	Line int
	Col  int
	Msg  string
	Src  string // 出错行的源码（不含换行）
}

// Error 实现 error 接口。
func (e *ParseError) Error() string {
	if e.Src == "" {
		return fmt.Sprintf("第 %d 行第 %d 列：%s", e.Line, e.Col, e.Msg)
	}
	return fmt.Sprintf("第 %d 行第 %d 列：%s\n%s\n%s^", e.Line, e.Col, e.Msg, e.Src, strings.Repeat(" ", e.Col-1))
}
