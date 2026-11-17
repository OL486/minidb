package sql

import (
	"strings"
	"testing"
)

func TestValueCompare(t *testing.T) {
	cases := []struct {
		name string
		a, b Value
		want int
	}{
		{"null smallest", Null(), IntVal(0), -1},
		{"null equal null", Null(), Null(), 0},
		{"int vs float equal", IntVal(1), FloatVal(1.0), 0},
		{"int vs float less", IntVal(1), FloatVal(1.5), -1},
		{"int compare", IntVal(2), IntVal(10), -1},
		{"str compare", StrVal("abc"), StrVal("abd"), -1},
		{"bool compare", BoolVal(false), BoolVal(true), -1},
		{"number before str", IntVal(999), StrVal("a"), -1},
		{"str before bool", StrVal("z"), BoolVal(false), -1},
		{"float vs int", FloatVal(2.5), IntVal(3), -1},
	}
	for _, c := range cases {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("%s: Compare(%v, %v) = %d, want %d", c.name, c.a, c.b, got, c.want)
		}
	}
}

func TestValueString(t *testing.T) {
	cases := []struct {
		v    Value
		want string
	}{
		{Null(), "NULL"},
		{IntVal(-42), "-42"},
		{FloatVal(1.5), "1.5"},
		{StrVal("hi"), "hi"},
		{BoolVal(true), "true"},
		{BoolVal(false), "false"},
	}
	for _, c := range cases {
		if got := c.v.String(); got != c.want {
			t.Errorf("Value(%v).String() = %q, want %q", c.v, got, c.want)
		}
	}
}

func TestLex(t *testing.T) {
	toks, err := Lex("select * from users where age >= 18 and name = 'O''Brien';")
	if err != nil {
		t.Fatalf("Lex failed: %v", err)
	}
	kinds := []TokenKind{
		TokSelect, TokStar, TokFrom, TokIdent, TokWhere, TokIdent, TokGe,
		TokInt, TokAnd, TokIdent, TokEq, TokString, TokSemi, TokEOF,
	}
	if len(toks) != len(kinds) {
		t.Fatalf("got %d tokens, want %d: %v", len(toks), len(kinds), toks)
	}
	for i, k := range kinds {
		if toks[i].Kind != k {
			t.Errorf("token %d: kind = %v, want %v (%q)", i, toks[i].Kind, k, toks[i].Lit)
		}
	}
	if toks[11].Str != "O'Brien" {
		t.Errorf("string token = %q, want %q", toks[11].Str, "O'Brien")
	}
	if toks[7].Int != 18 {
		t.Errorf("int token = %d, want 18", toks[7].Int)
	}
}

func TestLexNumbers(t *testing.T) {
	toks, err := Lex("1 1.5 1e3 1.5e-3 2E+10")
	if err != nil {
		t.Fatalf("Lex failed: %v", err)
	}
	if toks[0].Kind != TokInt || toks[0].Int != 1 {
		t.Errorf("token 0 = %+v, want int 1", toks[0])
	}
	if toks[1].Kind != TokFloat || toks[1].Float != 1.5 {
		t.Errorf("token 1 = %+v, want float 1.5", toks[1])
	}
	if toks[2].Kind != TokFloat || toks[2].Float != 1000 {
		t.Errorf("token 2 = %+v, want float 1000", toks[2])
	}
	if toks[3].Kind != TokFloat || toks[3].Float != 0.0015 {
		t.Errorf("token 3 = %+v, want float 0.0015", toks[3])
	}
	if toks[4].Kind != TokFloat || toks[4].Float != 2e10 {
		t.Errorf("token 4 = %+v, want float 2e10", toks[4])
	}
}

func TestLexErrors(t *testing.T) {
	cases := []string{
		"SELECT 'abc",                    // 未闭合字符串
		"SELECT @",                       // 无法识别的字符
		"SELECT 99999999999999999999999", // 整数溢出
		"SELECT !",                       // 孤立的 !
	}
	for _, src := range cases {
		if _, err := Lex(src); err == nil {
			t.Errorf("Lex(%q) should fail", src)
		}
	}
}

func TestLexLineComment(t *testing.T) {
	toks, err := Lex("SELECT 1 -- 注释\n, 2")
	if err != nil {
		t.Fatalf("Lex failed: %v", err)
	}
	// SELECT 1 , 2 EOF
	if len(toks) != 5 {
		t.Fatalf("got %d tokens, want 5: %v", len(toks), toks)
	}
}

func TestParseSelect(t *testing.T) {
	stmt, err := Parse("SELECT name, age FROM users WHERE age > 18 AND name != 'x' ORDER BY age DESC, name LIMIT 10;")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	sel, ok := stmt.(*SelectStmt)
	if !ok {
		t.Fatalf("stmt is %T, want *SelectStmt", stmt)
	}
	if sel.Table != "users" || len(sel.Cols) != 2 || sel.Cols[0].Col != "name" {
		t.Errorf("bad select list/table: %+v", sel)
	}
	if sel.Where == nil || sel.Where.String() == "" {
		t.Error("WHERE missing")
	}
	if len(sel.OrderBy) != 2 || !sel.OrderBy[0].Desc || sel.OrderBy[1].Desc {
		t.Errorf("bad ORDER BY: %+v", sel.OrderBy)
	}
	if sel.Limit != 10 {
		t.Errorf("Limit = %d, want 10", sel.Limit)
	}
}

func TestParseSelectStarAndCount(t *testing.T) {
	stmt, err := Parse("select * from t")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	sel := stmt.(*SelectStmt)
	if len(sel.Cols) != 1 || !sel.Cols[0].Star {
		t.Errorf("want star select, got %+v", sel.Cols)
	}
	if sel.Limit != -1 {
		t.Errorf("Limit = %d, want -1 (no limit)", sel.Limit)
	}

	stmt, err = Parse("SELECT COUNT(*) FROM t")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	sel = stmt.(*SelectStmt)
	if len(sel.Cols) != 1 || !sel.Cols[0].Count || sel.Cols[0].CountCol != "*" {
		t.Errorf("want COUNT(*), got %+v", sel.Cols)
	}

	stmt, err = Parse("SELECT COUNT(age) FROM t")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	sel = stmt.(*SelectStmt)
	if !sel.Cols[0].Count || sel.Cols[0].CountCol != "age" {
		t.Errorf("want COUNT(age), got %+v", sel.Cols)
	}
}

func TestParseCreateTable(t *testing.T) {
	stmt, err := Parse("CREATE TABLE users (id INT, name TEXT, score REAL, ok BOOLEAN)")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	ct := stmt.(*CreateTableStmt)
	if ct.Table != "users" || len(ct.Cols) != 4 {
		t.Fatalf("bad create table: %+v", ct)
	}
	want := []string{"INT", "TEXT", "FLOAT", "BOOL"}
	for i, w := range want {
		if ct.Cols[i].Type != w {
			t.Errorf("col %d type = %q, want %q", i, ct.Cols[i].Type, w)
		}
	}
}

func TestParseInsert(t *testing.T) {
	stmt, err := Parse("INSERT INTO users (id, name) VALUES (1, 'a'), (2, NULL)")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	ins := stmt.(*InsertStmt)
	if ins.Table != "users" || len(ins.Cols) != 2 || len(ins.Rows) != 2 {
		t.Fatalf("bad insert: %+v", ins)
	}
	if len(ins.Rows[0]) != 2 || len(ins.Rows[1]) != 2 {
		t.Errorf("bad row lengths: %+v", ins.Rows)
	}
}

func TestParseInsertNoColList(t *testing.T) {
	stmt, err := Parse("INSERT INTO t VALUES (-1, 2.5, TRUE)")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	ins := stmt.(*InsertStmt)
	if len(ins.Cols) != 0 || len(ins.Rows) != 1 || len(ins.Rows[0]) != 3 {
		t.Fatalf("bad insert: %+v", ins)
	}
	lit := ins.Rows[0][0].(*Literal)
	if lit.Val.Int != -1 {
		t.Errorf("first value = %v, want -1", lit.Val)
	}
}

func TestParseExprPrecedence(t *testing.T) {
	stmt, err := Parse("SELECT * FROM t WHERE a = 1 OR b = 2 AND NOT c = 3")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	sel := stmt.(*SelectStmt)
	// 顶层必须是 OR（OR 优先级最低）。
	or, ok := sel.Where.(*BinaryExpr)
	if !ok || or.Op != "OR" {
		t.Fatalf("top expr = %T %v, want OR BinaryExpr", sel.Where, sel.Where)
	}
	// 右边必须是 AND。
	if _, ok := or.Right.(*BinaryExpr); !ok {
		t.Fatalf("right of OR = %T, want AND BinaryExpr", or.Right)
	}
}

func TestParseIsNull(t *testing.T) {
	stmt, err := Parse("SELECT * FROM t WHERE a IS NULL OR b IS NOT NULL")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	sel := stmt.(*SelectStmt)
	or := sel.Where.(*BinaryExpr)
	left := or.Left.(*IsNullExpr)
	if left.Neg {
		t.Error("left IS NULL should not be negated")
	}
	right := or.Right.(*IsNullExpr)
	if !right.Neg {
		t.Error("right IS NOT NULL should be negated")
	}
}

func TestParseJoinParsedButKept(t *testing.T) {
	stmt, err := Parse("SELECT * FROM a JOIN b ON a.id = b.id")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	sel := stmt.(*SelectStmt)
	if sel.Join == nil || sel.Join.Table != "b" {
		t.Errorf("JOIN not parsed: %+v", sel.Join)
	}
}

func TestParseErrors(t *testing.T) {
	cases := []string{
		"",                                 // 空语句
		"DROP TABLE t",                     // 不支持的语句
		"SELECT FROM t",                    // SELECT 列表缺失
		"SELECT * t",                       // 缺 FROM
		"SELECT * FROM",                    // 缺表名
		"CREATE TABLE t ()",                // 零列
		"CREATE TABLE t (a VARCHAR)",       // 不支持的列类型
		"CREATE TABLE t (a INT",            // 缺右括号
		"INSERT INTO t VALUES (1+2)",       // VALUES 里不是字面量
		"INSERT INTO t VALUES ()",          // 空行
		"SELECT * FROM t WHERE age >",      // 表达式不完整
		"SELECT * FROM t LIMIT -1",         // LIMIT 负数
		"SELECT * FROM t LIMIT 'x'",        // LIMIT 不是整数
		"SELECT * FROM t ORDER BY",         // ORDER BY 缺列
		"SELECT COUNT(x, y) FROM t",        // COUNT 多参数
		"SELECT * FROM t; SELECT * FROM t", // 一次只能一条语句
		"SELECT * FROM t WHERE (a = 1",     // 括号未闭合
	}
	for _, src := range cases {
		if _, err := Parse(src); err == nil {
			t.Errorf("Parse(%q) should fail", src)
		}
	}
}

func TestParseErrorPosition(t *testing.T) {
	_, err := Parse("SELECT * FORM t")
	if err == nil {
		t.Fatal("Parse should fail")
	}
	pe, ok := err.(*ParseError)
	if !ok {
		t.Fatalf("error is %T, want *ParseError", err)
	}
	if pe.Line != 1 || pe.Col != 10 {
		t.Errorf("position = %d:%d, want 1:10", pe.Line, pe.Col)
	}
	if !strings.Contains(pe.Error(), "第 1 行第 10 列") || !strings.Contains(pe.Error(), "^") {
		t.Errorf("error text missing position/caret: %q", pe.Error())
	}
}

func TestParseCaseInsensitive(t *testing.T) {
	if _, err := Parse("SeLeCt * FrOm t WhErE a = 1 AnD b Is NuLl"); err != nil {
		t.Errorf("Parse failed: %v", err)
	}
}

func TestSplitStatements(t *testing.T) {
	src := `-- 注释里的分号 ; 不算
CREATE TABLE t (a TEXT);
INSERT INTO t VALUES ('a;b'), ('c'';d');
SELECT * FROM t;`
	got := SplitStatements(src)
	if len(got) != 3 {
		t.Fatalf("SplitStatements = %d stmts, want 3: %q", len(got), got)
	}
	if !strings.HasPrefix(strings.TrimSpace(got[0]), "--") {
		t.Errorf("stmt 0 should keep its comment, got %q", got[0])
	}
	if !strings.Contains(got[1], "'a;b'") || !strings.Contains(got[1], "'c'';d'") {
		t.Errorf("stmt 1 = %q, want both quoted semicolons kept", got[1])
	}
	if len(SplitStatements("")) != 0 || len(SplitStatements("  ;  ")) != 0 {
		t.Error("empty / semicolon-only input should give no statements")
	}
}

func TestIsSelect(t *testing.T) {
	if !IsSelect("SELECT * FROM t") || !IsSelect("  select 1") {
		t.Error("SELECT should be detected")
	}
	if !IsSelect("-- 注释\nSELECT * FROM t") {
		t.Error("SELECT after a comment should be detected")
	}
	if IsSelect("INSERT INTO t VALUES (1)") || IsSelect("-- 只有注释") {
		t.Error("non-SELECT should not be detected")
	}
}
