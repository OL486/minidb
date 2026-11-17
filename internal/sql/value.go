// Package sql 提供 minidb 的 SQL 方言：值类型、词法分析、语法树与解析器。
//
// M1 只支持 CREATE TABLE / INSERT / SELECT（+ WHERE / ORDER BY / LIMIT /
// COUNT）的一个 SQL 子集，语义见 docs/02-sql-semantics.md。
package sql

import (
	"fmt"
	"strconv"
)

// Kind 是 Value 的类型。
type Kind int

const (
	KindNull Kind = iota
	KindInt
	KindFloat
	KindStr
	KindBool
)

// Value 是 SQL 值的统一表示。Kind 决定哪个字段有效。
type Value struct {
	Kind  Kind
	Int   int64
	Float float64
	Str   string
	Bool  bool
}

// Null 返回 NULL 值。
func Null() Value { return Value{Kind: KindNull} }

// IntVal 返回整数值。
func IntVal(v int64) Value { return Value{Kind: KindInt, Int: v} }

// FloatVal 返回浮点值。
func FloatVal(v float64) Value { return Value{Kind: KindFloat, Float: v} }

// StrVal 返回字符串值。
func StrVal(s string) Value { return Value{Kind: KindStr, Str: s} }

// BoolVal 返回布尔值。
func BoolVal(b bool) Value { return Value{Kind: KindBool, Bool: b} }

// IsNull 报告值是否为 NULL。
func (v Value) IsNull() bool { return v.Kind == KindNull }

// IsNumeric 报告值是否为数字（Int 或 Float）。
func (v Value) IsNumeric() bool { return v.Kind == KindInt || v.Kind == KindFloat }

// String 把值格式化成结果输出用的文本：NULL、数字、字符串原文、true/false。
func (v Value) String() string {
	switch v.Kind {
	case KindNull:
		return "NULL"
	case KindInt:
		return strconv.FormatInt(v.Int, 10)
	case KindFloat:
		return strconv.FormatFloat(v.Float, 'g', -1, 64)
	case KindStr:
		return v.Str
	case KindBool:
		return strconv.FormatBool(v.Bool)
	default:
		return fmt.Sprintf("<bad kind %d>", int(v.Kind))
	}
}

// Compare 比较两个值，返回 -1、0 或 1。语义（见 docs/02-sql-semantics.md）：
//
//   - NULL 最小，任何值都大于 NULL；
//   - Int 与 Float 比较时按 float64 提升，1 与 1.0 相等；
//   - 不同类型之间按固定顺序排：Null < 数字 < 字符串 < 布尔；
//   - 字符串按字节序比较，布尔 false < true。
func Compare(a, b Value) int {
	if a.Kind == KindNull && b.Kind == KindNull {
		return 0
	}
	if a.Kind == KindNull {
		return -1
	}
	if b.Kind == KindNull {
		return 1
	}
	if a.IsNumeric() && b.IsNumeric() {
		af, bf := toFloat(a), toFloat(b)
		switch {
		case af < bf:
			return -1
		case af > bf:
			return 1
		default:
			return 0
		}
	}
	if a.Kind != b.Kind {
		return int(kindOrder(a.Kind)) - int(kindOrder(b.Kind))
	}
	switch a.Kind {
	case KindStr:
		switch {
		case a.Str < b.Str:
			return -1
		case a.Str > b.Str:
			return 1
		default:
			return 0
		}
	case KindBool:
		switch {
		case a.Bool == b.Bool:
			return 0
		case !a.Bool:
			return -1
		default:
			return 1
		}
	default:
		return 0
	}
}

// kindOrder 给出跨类型比较时的类型顺序：Null < 数字 < 字符串 < 布尔。
func kindOrder(k Kind) int {
	switch k {
	case KindNull:
		return 0
	case KindInt, KindFloat:
		return 1
	case KindStr:
		return 2
	case KindBool:
		return 3
	default:
		return 4
	}
}

func toFloat(v Value) float64 {
	if v.Kind == KindFloat {
		return v.Float
	}
	return float64(v.Int)
}

// Arith 对两个数值做四则运算（+、-、*、/），语义见 arith。
// 任一操作数为 NULL 时结果为 NULL；除零返回错误。
func Arith(op string, a, b Value) (Value, error) {
	return arith(op, a, b)
}

// arith 对两个数值做四则运算。任一操作数为 NULL 时结果为 NULL；
// 整数与整数运算结果仍为整数（除法为截断的整数除法）；
// 只要有一方是浮点数，结果就是浮点数。除零返回错误。
func arith(op string, a, b Value) (Value, error) {
	if a.IsNull() || b.IsNull() {
		return Null(), nil
	}
	if !a.IsNumeric() || !b.IsNumeric() {
		return Null(), fmt.Errorf("operator %s requires numeric operands, got %s and %s", op, kindName(a.Kind), kindName(b.Kind))
	}
	if a.Kind == KindInt && b.Kind == KindInt {
		switch op {
		case "+":
			return IntVal(a.Int + b.Int), nil
		case "-":
			return IntVal(a.Int - b.Int), nil
		case "*":
			return IntVal(a.Int * b.Int), nil
		case "/":
			if b.Int == 0 {
				return Null(), fmt.Errorf("division by zero")
			}
			return IntVal(a.Int / b.Int), nil
		}
	}
	af, bf := toFloat(a), toFloat(b)
	switch op {
	case "+":
		return FloatVal(af + bf), nil
	case "-":
		return FloatVal(af - bf), nil
	case "*":
		return FloatVal(af * bf), nil
	case "/":
		if bf == 0 {
			return Null(), fmt.Errorf("division by zero")
		}
		return FloatVal(af / bf), nil
	}
	return Null(), fmt.Errorf("unknown arithmetic operator %q", op)
}

func kindName(k Kind) string {
	switch k {
	case KindNull:
		return "NULL"
	case KindInt:
		return "INT"
	case KindFloat:
		return "FLOAT"
	case KindStr:
		return "TEXT"
	case KindBool:
		return "BOOL"
	default:
		return "?"
	}
}

// Row 是一行值，按表列顺序排列。
type Row []Value
