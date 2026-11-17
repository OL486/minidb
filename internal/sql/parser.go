package sql

import (
	"fmt"
	"strings"
)

// parser 是手写递归下降解析器。表达式优先级：
// OR → AND → 比较（含 IS [NOT] NULL）→ 加减 → 乘除 → 一元 → 原子。
type parser struct {
	toks []Token
	pos  int
	src  string
}

// Parse 解析一段 SQL 文本为一条语句。空输入、多余 token、
// 不支持的语法都返回 *ParseError（带行列与源码摘录），不 panic。
func Parse(src string) (Stmt, error) {
	toks, err := Lex(src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks, src: src}
	if p.peek().Kind == TokEOF {
		return nil, p.errorAt(p.peek(), "空语句")
	}
	stmt, err := p.parseStmt()
	if err != nil {
		return nil, err
	}
	// 允许末尾一个分号。
	if p.peek().Kind == TokSemi {
		p.pos++
	}
	if p.peek().Kind != TokEOF {
		return nil, p.errorAt(p.peek(), "意外的 token %s", p.peek().String())
	}
	return stmt, nil
}

func (p *parser) peek() Token { return p.toks[p.pos] }
func (p *parser) peekAt(n int) Token {
	if p.pos+n >= len(p.toks) {
		return p.toks[len(p.toks)-1]
	}
	return p.toks[p.pos+n]
}
func (p *parser) next() Token {
	t := p.toks[p.pos]
	if p.pos < len(p.toks)-1 {
		p.pos++
	}
	return t
}

func (p *parser) errorAt(t Token, format string, args ...any) error {
	return &ParseError{
		Line: t.Line,
		Col:  t.Col,
		Msg:  fmt.Sprintf(format, args...),
		Src:  srcLine(p.src, t.Line),
	}
}

// expect 要求下一个 token 是 kind，否则报错；返回吃掉的 token。
func (p *parser) expect(kind TokenKind, what string) (Token, error) {
	t := p.peek()
	if t.Kind != kind {
		return Token{}, p.errorAt(t, "需要 %s，但得到 %s", what, t.String())
	}
	return p.next(), nil
}

func (p *parser) parseStmt() (Stmt, error) {
	switch p.peek().Kind {
	case TokCreate:
		return p.parseCreateTable()
	case TokInsert:
		return p.parseInsert()
	case TokSelect:
		return p.parseSelect()
	default:
		return nil, p.errorAt(p.peek(), "不支持的语句开头 %s，只支持 CREATE TABLE、INSERT、SELECT", p.peek().String())
	}
}

// parseCreateTable 解析 CREATE TABLE t (a INT, b TEXT, ...)。
func (p *parser) parseCreateTable() (Stmt, error) {
	p.next() // CREATE
	if _, err := p.expect(TokTable, "TABLE"); err != nil {
		return nil, err
	}
	nameTok, err := p.expect(TokIdent, "表名")
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(TokLParen, "'('"); err != nil {
		return nil, err
	}
	cols, err := p.parseColumnDefs()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(TokRParen, "')'"); err != nil {
		return nil, err
	}
	return &CreateTableStmt{Table: nameTok.Lit, Cols: cols}, nil
}

// parseColumnDefs 解析列定义列表，至少一列。
func (p *parser) parseColumnDefs() ([]ColumnDef, error) {
	var cols []ColumnDef
	for {
		nameTok, err := p.expect(TokIdent, "列名")
		if err != nil {
			return nil, err
		}
		typTok := p.next()
		typ, err := normalizeColType(typTok)
		if err != nil {
			return nil, p.errorAt(typTok, "%s", err.Error())
		}
		cols = append(cols, ColumnDef{Name: nameTok.Lit, Type: typ})
		if p.peek().Kind != TokComma {
			break
		}
		p.next()
	}
	return cols, nil
}

// normalizeColType 把列类型名规范化为 INT、FLOAT、TEXT、BOOL。
func normalizeColType(t Token) (string, error) {
	if t.Kind != TokIdent {
		return "", fmt.Errorf("需要列类型 INT、FLOAT、TEXT 或 BOOL，但得到 %s", t.String())
	}
	switch strings.ToUpper(t.Lit) {
	case "INT", "INTEGER":
		return "INT", nil
	case "FLOAT", "REAL":
		return "FLOAT", nil
	case "TEXT":
		return "TEXT", nil
	case "BOOL", "BOOLEAN":
		return "BOOL", nil
	default:
		return "", fmt.Errorf("不支持的列类型 %q，只支持 INT、FLOAT、TEXT、BOOL", t.Lit)
	}
}

// parseInsert 解析 INSERT INTO t [(cols)] VALUES (...), (...)。
func (p *parser) parseInsert() (Stmt, error) {
	p.next() // INSERT
	if _, err := p.expect(TokInto, "INTO"); err != nil {
		return nil, err
	}
	nameTok, err := p.expect(TokIdent, "表名")
	if err != nil {
		return nil, err
	}
	stmt := &InsertStmt{Table: nameTok.Lit}
	if p.peek().Kind == TokLParen {
		p.next()
		for {
			colTok, err := p.expect(TokIdent, "列名")
			if err != nil {
				return nil, err
			}
			stmt.Cols = append(stmt.Cols, colTok.Lit)
			if p.peek().Kind != TokComma {
				break
			}
			p.next()
		}
		if _, err := p.expect(TokRParen, "')'"); err != nil {
			return nil, err
		}
	}
	if _, err := p.expect(TokValues, "VALUES"); err != nil {
		return nil, err
	}
	for {
		if _, err := p.expect(TokLParen, "'('"); err != nil {
			return nil, err
		}
		row, err := p.parseValueList()
		if err != nil {
			return nil, err
		}
		if len(row) == 0 {
			return nil, p.errorAt(p.peek(), "VALUES 里至少要有一个值")
		}
		stmt.Rows = append(stmt.Rows, row)
		if _, err := p.expect(TokRParen, "')'"); err != nil {
			return nil, err
		}
		if p.peek().Kind != TokComma {
			break
		}
		p.next()
	}
	return stmt, nil
}

// parseValueList 解析 VALUES 里的一组字面量。只接受字面量，
// 表达式（如 1+2）会被拒绝——M1 的 INSERT 值必须是字面量。
func (p *parser) parseValueList() ([]Expr, error) {
	var row []Expr
	for {
		v, err := p.parseValueLiteral()
		if err != nil {
			return nil, err
		}
		row = append(row, v)
		if p.peek().Kind != TokComma {
			break
		}
		p.next()
	}
	return row, nil
}

// parseValueLiteral 解析一个字面量值：数字、字符串、NULL、TRUE、FALSE，
// 或一元负号加数字。
func (p *parser) parseValueLiteral() (Expr, error) {
	t := p.peek()
	switch t.Kind {
	case TokInt:
		p.next()
		return &Literal{Val: IntVal(t.Int)}, nil
	case TokFloat:
		p.next()
		return &Literal{Val: FloatVal(t.Float)}, nil
	case TokString:
		p.next()
		return &Literal{Val: StrVal(t.Str)}, nil
	case TokNull:
		p.next()
		return &Literal{Val: Null()}, nil
	case TokTrue:
		p.next()
		return &Literal{Val: BoolVal(true)}, nil
	case TokFalse:
		p.next()
		return &Literal{Val: BoolVal(false)}, nil
	case TokMinus:
		p.next()
		num := p.peek()
		switch num.Kind {
		case TokInt:
			p.next()
			return &Literal{Val: IntVal(-num.Int)}, nil
		case TokFloat:
			p.next()
			return &Literal{Val: FloatVal(-num.Float)}, nil
		default:
			return nil, p.errorAt(num, "'-' 后面需要一个数字")
		}
	default:
		return nil, p.errorAt(t, "VALUES 里需要字面量，但得到 %s", t.String())
	}
}

// parseSelect 解析 SELECT 语句。
func (p *parser) parseSelect() (Stmt, error) {
	p.next() // SELECT
	stmt := &SelectStmt{Limit: -1}
	items, err := p.parseSelectList()
	if err != nil {
		return nil, err
	}
	stmt.Cols = items
	if _, err := p.expect(TokFrom, "FROM"); err != nil {
		return nil, err
	}
	tableTok, err := p.expect(TokIdent, "表名")
	if err != nil {
		return nil, err
	}
	stmt.Table = tableTok.Lit
	// JOIN 只解析进 AST，规划层拒绝执行（M5 才做）。
	if p.peek().Kind == TokJoin {
		join, err := p.parseJoin()
		if err != nil {
			return nil, err
		}
		stmt.Join = join
	}
	if p.peek().Kind == TokWhere {
		p.next()
		where, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		stmt.Where = where
	}
	if p.peek().Kind == TokOrder {
		p.next()
		if _, err := p.expect(TokBy, "BY"); err != nil {
			return nil, err
		}
		order, err := p.parseOrderList()
		if err != nil {
			return nil, err
		}
		stmt.OrderBy = order
	}
	if p.peek().Kind == TokLimit {
		p.next()
		nTok, err := p.expect(TokInt, "LIMIT 后面的整数")
		if err != nil {
			return nil, err
		}
		if nTok.Int < 0 {
			return nil, p.errorAt(nTok, "LIMIT 不能是负数")
		}
		stmt.Limit = int(nTok.Int)
	}
	return stmt, nil
}

// parseJoin 解析 JOIN t ON expr。
func (p *parser) parseJoin() (*JoinClause, error) {
	p.next() // JOIN
	tableTok, err := p.expect(TokIdent, "表名")
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(TokOn, "ON"); err != nil {
		return nil, err
	}
	on, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	return &JoinClause{Table: tableTok.Lit, On: on}, nil
}

// parseSelectList 解析 SELECT 列表：*、列名、COUNT(*) 或 COUNT(列)。
func (p *parser) parseSelectList() ([]SelectItem, error) {
	var items []SelectItem
	for {
		t := p.peek()
		switch {
		case t.Kind == TokStar:
			p.next()
			items = append(items, SelectItem{Star: true})
		case t.Kind == TokIdent && strings.EqualFold(t.Lit, "COUNT") && p.peekAt(1).Kind == TokLParen:
			p.next() // COUNT
			p.next() // (
			item, err := p.parseCountArg()
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		case t.Kind == TokIdent:
			p.next()
			items = append(items, SelectItem{Col: t.Lit})
		default:
			return nil, p.errorAt(t, "SELECT 列表里需要列名、* 或 COUNT，但得到 %s", t.String())
		}
		if p.peek().Kind != TokComma {
			break
		}
		p.next()
	}
	return items, nil
}

// parseCountArg 解析 COUNT 的参数：* 或列名，并吃掉右括号。
func (p *parser) parseCountArg() (SelectItem, error) {
	t := p.peek()
	var item SelectItem
	switch t.Kind {
	case TokStar:
		p.next()
		item = SelectItem{Count: true, CountCol: "*"}
	case TokIdent:
		p.next()
		item = SelectItem{Count: true, CountCol: t.Lit}
	default:
		return SelectItem{}, p.errorAt(t, "COUNT 里需要 * 或列名，但得到 %s", t.String())
	}
	if _, err := p.expect(TokRParen, "')'"); err != nil {
		return SelectItem{}, err
	}
	return item, nil
}

// parseOrderList 解析 ORDER BY 列表。
func (p *parser) parseOrderList() ([]OrderTerm, error) {
	var order []OrderTerm
	for {
		colTok, err := p.expect(TokIdent, "列名")
		if err != nil {
			return nil, err
		}
		term := OrderTerm{Col: colTok.Lit}
		switch p.peek().Kind {
		case TokDesc:
			p.next()
			term.Desc = true
		case TokAsc:
			p.next()
		}
		order = append(order, term)
		if p.peek().Kind != TokComma {
			break
		}
		p.next()
	}
	return order, nil
}

// parseExpr 是表达式入口（OR 优先级）。
func (p *parser) parseExpr() (Expr, error) {
	return p.parseOr()
}

func (p *parser) parseOr() (Expr, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.peek().Kind == TokOr {
		p.next()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{Op: "OR", Left: left, Right: right}
	}
	return left, nil
}

func (p *parser) parseAnd() (Expr, error) {
	left, err := p.parseComparison()
	if err != nil {
		return nil, err
	}
	for p.peek().Kind == TokAnd {
		p.next()
		right, err := p.parseComparison()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{Op: "AND", Left: left, Right: right}
	}
	return left, nil
}

// parseComparison 解析比较（含 IS [NOT] NULL）。不允许连续比较
// （如 a < b < c），多余的比较运算符会由上层报“意外的 token”。
func (p *parser) parseComparison() (Expr, error) {
	left, err := p.parseAdditive()
	if err != nil {
		return nil, err
	}
	t := p.peek()
	switch t.Kind {
	case TokIs:
		p.next()
		neg := false
		if p.peek().Kind == TokNot {
			p.next()
			neg = true
		}
		if _, err := p.expect(TokNull, "NULL"); err != nil {
			return nil, err
		}
		return &IsNullExpr{X: left, Neg: neg}, nil
	case TokEq, TokNe, TokLt, TokLe, TokGt, TokGe:
		p.next()
		right, err := p.parseAdditive()
		if err != nil {
			return nil, err
		}
		return &BinaryExpr{Op: t.Lit, Left: left, Right: right}, nil
	default:
		return left, nil
	}
}

func (p *parser) parseAdditive() (Expr, error) {
	left, err := p.parseMultiplicative()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		if t.Kind != TokPlus && t.Kind != TokMinus {
			break
		}
		p.next()
		right, err := p.parseMultiplicative()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{Op: t.Lit, Left: left, Right: right}
	}
	return left, nil
}

func (p *parser) parseMultiplicative() (Expr, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		if t.Kind != TokStar && t.Kind != TokSlash {
			break
		}
		p.next()
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{Op: t.Lit, Left: left, Right: right}
	}
	return left, nil
}

func (p *parser) parseUnary() (Expr, error) {
	t := p.peek()
	switch t.Kind {
	case TokMinus:
		p.next()
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &UnaryExpr{Op: "-", X: x}, nil
	case TokNot:
		p.next()
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &UnaryExpr{Op: "NOT", X: x}, nil
	default:
		return p.parsePrimary()
	}
}

func (p *parser) parsePrimary() (Expr, error) {
	t := p.peek()
	switch t.Kind {
	case TokInt:
		p.next()
		return &Literal{Val: IntVal(t.Int)}, nil
	case TokFloat:
		p.next()
		return &Literal{Val: FloatVal(t.Float)}, nil
	case TokString:
		p.next()
		return &Literal{Val: StrVal(t.Str)}, nil
	case TokNull:
		p.next()
		return &Literal{Val: Null()}, nil
	case TokTrue:
		p.next()
		return &Literal{Val: BoolVal(true)}, nil
	case TokFalse:
		p.next()
		return &Literal{Val: BoolVal(false)}, nil
	case TokIdent:
		p.next()
		name := t.Lit
		// 限定列名：a.b。存完整形式，执行层解析列名时先精确匹配，
		// 再按 '.' 后的后缀匹配。
		if p.peek().Kind == TokDot {
			p.next()
			partTok, err := p.expect(TokIdent, "列名")
			if err != nil {
				return nil, err
			}
			name = name + "." + partTok.Lit
		}
		return &ColumnRef{Name: name}, nil
	case TokLParen:
		p.next()
		e, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(TokRParen, "')'"); err != nil {
			return nil, err
		}
		return e, nil
	default:
		return nil, p.errorAt(t, "需要一个表达式，但得到 %s", t.String())
	}
}
