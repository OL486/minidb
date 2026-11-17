package storage

import (
	"bytes"
	"strings"
	"testing"

	"github.com/OL486/minidb/internal/sql"
)

func TestRecordRoundTrip(t *testing.T) {
	rows := [][]sql.Value{
		{},
		{sql.Null()},
		{sql.IntVal(-42), sql.FloatVal(1.5), sql.StrVal(""), sql.BoolVal(true)},
		{sql.StrVal("hello, world"), sql.IntVal(0), sql.FloatVal(-0.25), sql.BoolVal(false)},
		{sql.StrVal(strings.Repeat("x", 10000)), sql.Null(), sql.IntVal(1 << 62)},
		{sql.FloatVal(1e300), sql.StrVal("ünïcödé ✓"), sql.IntVal(-1 << 62)},
	}
	for i, row := range rows {
		enc := EncodeRow(row)
		dec, err := DecodeRow(enc)
		if err != nil {
			t.Fatalf("row %d: DecodeRow failed: %v", i, err)
		}
		if len(dec) != len(row) {
			t.Fatalf("row %d: decoded %d cols, want %d", i, len(dec), len(row))
		}
		for j := range row {
			if sql.Compare(dec[j], row[j]) != 0 || dec[j].Kind != row[j].Kind {
				t.Errorf("row %d col %d: got %+v, want %+v", i, j, dec[j], row[j])
			}
		}
		// 再编一次必须字节一致（编码是确定性的）。
		if !bytes.Equal(EncodeRow(dec), enc) {
			t.Errorf("row %d: re-encode differs", i)
		}
	}
}

func TestRecordDecodeCorrupt(t *testing.T) {
	good := EncodeRow([]sql.Value{sql.IntVal(1), sql.StrVal("ab")})
	cases := [][]byte{
		{},
		{0xFF},             // 列数 varint 截断（0xFF 后面没字节）
		good[:len(good)-1], // 截尾
		good[:3],           // 截在字符串中间
		append(append([]byte{}, good...), 0x7F)[0:2], // tag 被改成未知值
	}
	// 构造一个未知 tag：列数 1 + tag 9。
	bad := []byte{0x01, 0x09}
	cases = append(cases, bad)
	// Bool 值非法（tag 4 后跟 2）。
	cases = append(cases, []byte{0x01, 0x04, 0x02})
	for i, b := range cases {
		if _, err := DecodeRow(b); err == nil {
			t.Errorf("case %d: DecodeRow(%x) should fail", i, b)
		}
	}
}

func TestMemBTreeOrder(t *testing.T) {
	bt := NewMemBTree()
	// 乱序插入，扫描必须按键升序。
	for _, k := range []int64{5, 1, 9, 3, 7, 2, 8, 4, 6, 0} {
		if err := bt.Insert(sql.IntVal(k), []byte{byte(k)}); err != nil {
			t.Fatalf("Insert(%d) failed: %v", k, err)
		}
	}
	if bt.Len() != 10 {
		t.Fatalf("Len() = %d, want 10", bt.Len())
	}
	kvs := bt.Scan()
	for i, kv := range kvs {
		if kv.Key.Int != int64(i) {
			t.Errorf("Scan()[%d].Key = %d, want %d", i, kv.Key.Int, i)
		}
		if len(kv.Val) != 1 || kv.Val[0] != byte(i) {
			t.Errorf("Scan()[%d].Val = %v, want [%d]", i, kv.Val, i)
		}
	}
}

func TestMemBTreeGetDeleteUpsert(t *testing.T) {
	bt := NewMemBTree()
	if _, ok := bt.Get(sql.IntVal(1)); ok {
		t.Error("Get on empty tree should miss")
	}
	if bt.Delete(sql.IntVal(1)) {
		t.Error("Delete on empty tree should return false")
	}
	_ = bt.Insert(sql.IntVal(1), []byte("a"))
	_ = bt.Insert(sql.IntVal(1), []byte("b")) // upsert 覆盖
	got, ok := bt.Get(sql.IntVal(1))
	if !ok || string(got) != "b" {
		t.Errorf("Get after upsert = %q, %v; want \"b\", true", got, ok)
	}
	if bt.Len() != 1 {
		t.Errorf("Len() = %d, want 1", bt.Len())
	}
	if !bt.Delete(sql.IntVal(1)) {
		t.Error("Delete should return true")
	}
	if _, ok := bt.Get(sql.IntVal(1)); ok {
		t.Error("Get after Delete should miss")
	}
}

func TestTableInsertScan(t *testing.T) {
	tbl := NewTable("users", []sql.ColumnDef{
		{Name: "id", Type: "INT"},
		{Name: "name", Type: "TEXT"},
	})
	if _, err := tbl.Insert([]sql.Value{sql.IntVal(1)}); err == nil {
		t.Error("Insert with wrong column count should fail")
	}
	if _, err := tbl.Insert([]sql.Value{sql.IntVal(1), sql.StrVal("a")}); err != nil {
		t.Fatalf("Insert failed: %v", err)
	}
	if _, err := tbl.Insert([]sql.Value{sql.IntVal(2), sql.StrVal("b")}); err != nil {
		t.Fatalf("Insert failed: %v", err)
	}
	rows, err := tbl.Scan()
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}
	if len(rows) != 2 || rows[0][1].Str != "a" || rows[1][1].Str != "b" {
		t.Errorf("Scan = %v, want [[1 a] [2 b]]", rows)
	}
	if tbl.RowCount() != 2 {
		t.Errorf("RowCount() = %d, want 2", tbl.RowCount())
	}
	if got := tbl.ColumnIndex("NAME"); got != 1 {
		t.Errorf("ColumnIndex(NAME) = %d, want 1 (case-insensitive)", got)
	}
	if got := tbl.ColumnIndex("users.name"); got != 1 {
		t.Errorf("ColumnIndex(users.name) = %d, want 1 (qualified)", got)
	}
	if got := tbl.ColumnIndex("nope"); got != -1 {
		t.Errorf("ColumnIndex(nope) = %d, want -1", got)
	}
}

func TestStoreCreateTable(t *testing.T) {
	s := NewStore()
	cols := []sql.ColumnDef{{Name: "id", Type: "INT"}}
	if err := s.CreateTable("Users", cols); err != nil {
		t.Fatalf("CreateTable failed: %v", err)
	}
	if _, ok := s.Table("users"); !ok {
		t.Error("Table lookup should be case-insensitive")
	}
	if err := s.CreateTable("USERS", cols); err == nil {
		t.Error("duplicate table (case-insensitive) should fail")
	}
	if err := s.CreateTable("t2", []sql.ColumnDef{{Name: "a", Type: "INT"}, {Name: "A", Type: "TEXT"}}); err == nil {
		t.Error("duplicate column (case-insensitive) should fail")
	}
	if err := s.CreateTable("t3", nil); err == nil {
		t.Error("zero-column table should fail")
	}
	if got := s.Tables(); len(got) != 1 || got[0] != "Users" {
		t.Errorf("Tables() = %v, want [Users]", got)
	}
}
