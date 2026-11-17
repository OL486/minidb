package sql

import (
	"fmt"
	"strconv"
	"strings"
)

// TokenKind 是词法单元的类型。
type TokenKind int

const (
	// TokEOF 是输入结束。
	TokEOF TokenKind = iota
	// TokIdent 是标识符（表名、列名）。
	TokIdent
	// TokInt 是整数面值。
	TokInt
	// TokFloat 是浮点面值。
	TokFloat
	// TokString 是字符串面值。
	TokString

	// TokComma 是 ,。
	TokComma
	// TokLParen 是 (。
	TokLParen
	// TokRParen 是 )。
	TokRParen
	// TokSemi 是 ;。
	TokSemi
	// TokStar 是 *。
	TokStar
	// TokDot 是 .（限定列名 a.b 用）。
	TokDot
	// TokEq 是 =。
	TokEq
	// TokNe 是 != 或 <>。
	TokNe
	// TokLt 是 <。
	TokLt
	// TokLe 是 <=。
	TokLe
	// TokGt 是 >。
	TokGt
	// TokGe 是 >=。
	TokGe
	// TokPlus 是 +。
	TokPlus
	// TokMinus 是 -。
	TokMinus
	// TokSlash 是 /。
	TokSlash

	// 以下为关键字。关键字大小写不敏感，词法器统一按大写识别。
	TokSelect
	TokFrom
	TokWhere
	TokOrder
	TokBy
	TokLimit
	TokAsc
	TokDesc
	TokCreate
	TokTable
	TokInsert
	TokInto
	TokValues
	TokAnd
	TokOr
	TokNot
	TokNull
	TokTrue
	TokFalse
	TokIs
	TokJoin
	TokOn
	TokIndex
)

// keywords 把关键字大写形式映射到 token 类型。
var keywords = map[string]TokenKind{
	"SELECT": TokSelect,
	"FROM":   TokFrom,
	"WHERE":  TokWhere,
	"ORDER":  TokOrder,
	"BY":     TokBy,
	"LIMIT":  TokLimit,
	"ASC":    TokAsc,
	"DESC":   TokDesc,
	"CREATE": TokCreate,
	"TABLE":  TokTable,
	"INSERT": TokInsert,
	"INTO":   TokInto,
	"VALUES": TokValues,
	"AND":    TokAnd,
	"OR":     TokOr,
	"NOT":    TokNot,
	"NULL":   TokNull,
	"TRUE":   TokTrue,
	"FALSE":  TokFalse,
	"IS":     TokIs,
	"JOIN":   TokJoin,
	"ON":     TokOn,
	"INDEX":  TokIndex,
}

// Token 是一个词法单元。Lit 是原文；数值与字符串面值分别放在 Int、
// Float、Str 里；Line、Col 是 1 起始的行列号。
type Token struct {
	Kind  TokenKind
	Lit   string
	Int   int64
	Float float64
	Str   string
	Line  int
	Col   int
}

// String 供错误信息使用。
func (t Token) String() string {
	if t.Kind == TokEOF {
		return "文件结尾"
	}
	return fmt.Sprintf("%q", t.Lit)
}

// lexer 是手写词法器，一次把整段输入切成 token。
type lexer struct {
	src  string
	pos  int
	line int
	col  int
	toks []Token
}

// Lex 把 SQL 文本切成 token 列表。出错时返回 *ParseError。
func Lex(src string) ([]Token, error) {
	l := &lexer{src: src, line: 1, col: 1}
	for l.pos < len(l.src) {
		if err := l.step(); err != nil {
			return nil, err
		}
	}
	l.toks = append(l.toks, Token{Kind: TokEOF, Lit: "", Line: l.line, Col: l.col})
	return l.toks, nil
}

// srcLine 返回第 line 行的源码（不含换行），供 ParseError 摘录。
func srcLine(src string, line int) string {
	cur := 1
	start := 0
	for i := 0; i < len(src); i++ {
		if cur == line && src[i] == '\n' {
			return src[start:i]
		}
		if src[i] == '\n' {
			cur++
			start = i + 1
		}
	}
	if cur == line {
		return src[start:]
	}
	return ""
}

func (l *lexer) errf(line, col int, format string, args ...any) error {
	return &ParseError{Line: line, Col: col, Msg: fmt.Sprintf(format, args...), Src: srcLine(l.src, line)}
}

func (l *lexer) peek() byte {
	if l.pos >= len(l.src) {
		return 0
	}
	return l.src[l.pos]
}

func (l *lexer) peekAt(n int) byte {
	if l.pos+n >= len(l.src) {
		return 0
	}
	return l.src[l.pos+n]
}

func (l *lexer) advance() byte {
	c := l.src[l.pos]
	l.pos++
	if c == '\n' {
		l.line++
		l.col = 1
	} else {
		l.col++
	}
	return c
}

func (l *lexer) emit(kind TokenKind, lit string, line, col int) {
	l.toks = append(l.toks, Token{Kind: kind, Lit: lit, Line: line, Col: col})
}

func (l *lexer) step() error {
	c := l.peek()
	switch {
	case c == ' ' || c == '\t' || c == '\r' || c == '\n':
		l.advance()
		return nil
	case c == '-' && l.peekAt(1) == '-':
		// -- 行注释，吃到行尾。
		for l.pos < len(l.src) && l.peek() != '\n' {
			l.advance()
		}
		return nil
	case c == '\'':
		return l.lexString()
	case c >= '0' && c <= '9':
		return l.lexNumber()
	case c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z'):
		return l.lexIdent()
	case c == ',':
		l.advance()
		l.emit(TokComma, ",", l.line, l.col-1)
		return nil
	case c == '(':
		l.advance()
		l.emit(TokLParen, "(", l.line, l.col-1)
		return nil
	case c == ')':
		l.advance()
		l.emit(TokRParen, ")", l.line, l.col-1)
		return nil
	case c == ';':
		l.advance()
		l.emit(TokSemi, ";", l.line, l.col-1)
		return nil
	case c == '*':
		l.advance()
		l.emit(TokStar, "*", l.line, l.col-1)
		return nil
	case c == '.':
		l.advance()
		l.emit(TokDot, ".", l.line, l.col-1)
		return nil
	case c == '=':
		l.advance()
		l.emit(TokEq, "=", l.line, l.col-1)
		return nil
	case c == '!':
		line, col := l.line, l.col
		l.advance()
		if l.peek() != '=' {
			return l.errf(line, col, "无法识别的字符 '!'，是不是想写 '!='")
		}
		l.advance()
		l.emit(TokNe, "!=", line, col)
		return nil
	case c == '<':
		line, col := l.line, l.col
		l.advance()
		switch l.peek() {
		case '=':
			l.advance()
			l.emit(TokLe, "<=", line, col)
		case '>':
			l.advance()
			l.emit(TokNe, "<>", line, col)
		default:
			l.emit(TokLt, "<", line, col)
		}
		return nil
	case c == '>':
		line, col := l.line, l.col
		l.advance()
		if l.peek() == '=' {
			l.advance()
			l.emit(TokGe, ">=", line, col)
		} else {
			l.emit(TokGt, ">", line, col)
		}
		return nil
	case c == '+':
		l.advance()
		l.emit(TokPlus, "+", l.line, l.col-1)
		return nil
	case c == '-':
		l.advance()
		l.emit(TokMinus, "-", l.line, l.col-1)
		return nil
	case c == '/':
		l.advance()
		l.emit(TokSlash, "/", l.line, l.col-1)
		return nil
	default:
		return l.errf(l.line, l.col, "无法识别的字符 %q", c)
	}
}

// lexString 读单引号字符串，” 转义为一个单引号。
func (l *lexer) lexString() error {
	line, col := l.line, l.col
	l.advance() // 吃掉开头的 '
	var b strings.Builder
	for {
		if l.pos >= len(l.src) {
			return l.errf(line, col, "字符串没有闭合")
		}
		c := l.peek()
		if c == '\'' {
			if l.peekAt(1) == '\'' {
				b.WriteByte('\'')
				l.advance()
				l.advance()
				continue
			}
			l.advance() // 吃掉结尾的 '
			break
		}
		b.WriteByte(l.advance())
	}
	tok := Token{Kind: TokString, Str: b.String(), Line: line, Col: col}
	tok.Lit = "'" + b.String() + "'"
	l.toks = append(l.toks, tok)
	return nil
}

// lexNumber 读整数或浮点数。含小数点或指数时为浮点数。
func (l *lexer) lexNumber() error {
	line, col := l.line, l.col
	start := l.pos
	isFloat := false
	// 整数部分。
	for l.pos < len(l.src) && l.peek() >= '0' && l.peek() <= '9' {
		l.advance()
	}
	// 小数部分：'.' 后面必须跟数字，否则 '.' 不属于这个数。
	if l.peek() == '.' && l.peekAt(1) >= '0' && l.peekAt(1) <= '9' {
		isFloat = true
		l.advance() // '.'
		for l.pos < len(l.src) && l.peek() >= '0' && l.peek() <= '9' {
			l.advance()
		}
	}
	// 指数部分：e 后面可跟符号与数字。
	if c := l.peek(); c == 'e' || c == 'E' {
		n1, n2 := l.peekAt(1), l.peekAt(2)
		if (n1 >= '0' && n1 <= '9') || ((n1 == '+' || n1 == '-') && n2 >= '0' && n2 <= '9') {
			isFloat = true
			l.advance() // 'e' 或 'E'
			if l.peek() == '+' || l.peek() == '-' {
				l.advance()
			}
			for l.pos < len(l.src) && l.peek() >= '0' && l.peek() <= '9' {
				l.advance()
			}
		}
	}
	lit := l.src[start:l.pos]
	if isFloat {
		f, err := strconv.ParseFloat(lit, 64)
		if err != nil {
			return l.errf(line, col, "无效的数字 %q", lit)
		}
		l.toks = append(l.toks, Token{Kind: TokFloat, Lit: lit, Float: f, Line: line, Col: col})
		return nil
	}
	n, err := strconv.ParseInt(lit, 10, 64)
	if err != nil {
		return l.errf(line, col, "整数 %q 超出范围", lit)
	}
	l.toks = append(l.toks, Token{Kind: TokInt, Lit: lit, Int: n, Line: line, Col: col})
	return nil
}

// lexIdent 读标识符或关键字。关键字按大写匹配，其余为标识符。
func (l *lexer) lexIdent() error {
	line, col := l.line, l.col
	start := l.pos
	for l.pos < len(l.src) {
		c := l.peek()
		if c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			l.advance()
			continue
		}
		break
	}
	lit := l.src[start:l.pos]
	if kind, ok := keywords[strings.ToUpper(lit)]; ok {
		l.toks = append(l.toks, Token{Kind: kind, Lit: lit, Line: line, Col: col})
		return nil
	}
	l.toks = append(l.toks, Token{Kind: TokIdent, Lit: lit, Line: line, Col: col})
	return nil
}
