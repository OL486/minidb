package storage

import (
	"fmt"
	"sort"
	"strings"

	"github.com/OL486/minidb/internal/sql"
)

// Table 是一张内存表：列定义 + 一个以行号为键的 B-tree，
// 值是 EncodeRow 序列化后的行。M2 会把 B-tree 换成磁盘实现，
// Table 的接口保持不变。
type Table struct {
	Name   string
	Cols   []sql.ColumnDef
	btree  BTree
	nextID uint64
}

// NewTable 创建一张空表。
func NewTable(name string, cols []sql.ColumnDef) *Table {
	return &Table{Name: name, Cols: cols, btree: NewMemBTree(), nextID: 1}
}

// ColumnIndex 按列名找列下标。列名不区分大小写；限定列名 a.b
// 按 '.' 后的部分匹配。找不到返回 -1。
func (t *Table) ColumnIndex(name string) int {
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[i+1:]
	}
	for i, c := range t.Cols {
		if strings.EqualFold(c.Name, name) {
			return i
		}
	}
	return -1
}

// ColumnNames 返回全部列名（建表顺序）。
func (t *Table) ColumnNames() []string {
	names := make([]string, len(t.Cols))
	for i, c := range t.Cols {
		names[i] = c.Name
	}
	return names
}

// Insert 插入一行，返回行号。行长度必须与列数一致。
func (t *Table) Insert(row []sql.Value) (uint64, error) {
	if len(row) != len(t.Cols) {
		return 0, fmt.Errorf("表 %s 有 %d 列，但插入了 %d 个值", t.Name, len(t.Cols), len(row))
	}
	id := t.nextID
	t.nextID++
	if err := t.btree.Insert(sql.IntVal(int64(id)), EncodeRow(row)); err != nil {
		return 0, err
	}
	return id, nil
}

// Scan 按行号升序返回全部行（解码后）。
func (t *Table) Scan() ([][]sql.Value, error) {
	kvs := t.btree.Scan()
	rows := make([][]sql.Value, 0, len(kvs))
	for _, kv := range kvs {
		row, err := DecodeRow(kv.Val)
		if err != nil {
			return nil, fmt.Errorf("表 %s 行号 %s 解码失败：%v", t.Name, kv.Key.String(), err)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// RowCount 返回行数。
func (t *Table) RowCount() int { return t.btree.Len() }

// Store 是内存 catalog：表名到表的映射。表名不区分大小写。
// M2 会把 catalog 写进系统表 _tables，接口保持不变。
type Store struct {
	tables map[string]*Table
}

// NewStore 返回一个空 catalog。
func NewStore() *Store { return &Store{tables: make(map[string]*Table)} }

// CreateTable 建表。表已存在、零列、列名重复时返回错误。
func (s *Store) CreateTable(name string, cols []sql.ColumnDef) error {
	key := strings.ToLower(name)
	if _, ok := s.tables[key]; ok {
		return fmt.Errorf("表 %s 已存在", name)
	}
	if len(cols) == 0 {
		return fmt.Errorf("表 %s 至少要有一列", name)
	}
	seen := make(map[string]bool, len(cols))
	for _, c := range cols {
		lower := strings.ToLower(c.Name)
		if seen[lower] {
			return fmt.Errorf("表 %s 的列 %s 重复", name, c.Name)
		}
		seen[lower] = true
	}
	s.tables[key] = NewTable(name, cols)
	return nil
}

// Table 按表名查找。表名不区分大小写。
func (s *Store) Table(name string) (*Table, bool) {
	t, ok := s.tables[strings.ToLower(name)]
	return t, ok
}

// Tables 返回全部表名（按字母序）。
func (s *Store) Tables() []string {
	names := make([]string, 0, len(s.tables))
	for _, t := range s.tables {
		names = append(names, t.Name)
	}
	sort.Strings(names)
	return names
}
