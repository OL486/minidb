package storage

import (
	"github.com/OL486/minidb/internal/sql"
)

// KV 是 B-tree 里的一条键值对：键是行号（或索引键），值是序列化后的行。
type KV struct {
	Key sql.Value
	Val []byte
}

// BTree 是表的行存储接口。M1 用内存实现 MemBTree 顶替；
// M2 的磁盘 B+ 树实现同一个接口后直接替换，不动上层代码。
//
// 语义：键按 sql.Compare 排序；Scan 按键升序返回全部条目；
// Insert 对已存在的键做覆盖（upsert）。
type BTree interface {
	Insert(key sql.Value, val []byte) error
	Get(key sql.Value) ([]byte, bool)
	Delete(key sql.Value) bool
	Scan() []KV
	Len() int
}

// MemBTree 是 BTree 的内存实现：排序切片 + 二分查找。
// 它刻意保持与 B+ 树相同的有序扫描语义，M2 换磁盘实现时
// 执行层不需要任何改动。
type MemBTree struct {
	keys []sql.Value
	vals [][]byte
}

// NewMemBTree 返回一个空的内存 B-tree。
func NewMemBTree() *MemBTree { return &MemBTree{} }

// search 返回 key 应在的位置，以及 key 是否已存在。
func (m *MemBTree) search(key sql.Value) (int, bool) {
	lo, hi := 0, len(m.keys)
	for lo < hi {
		mid := (lo + hi) / 2
		if c := sql.Compare(m.keys[mid], key); c < 0 {
			lo = mid + 1
		} else if c > 0 {
			hi = mid
		} else {
			return mid, true
		}
	}
	return lo, false
}

// Insert 插入或覆盖一条键值对。
func (m *MemBTree) Insert(key sql.Value, val []byte) error {
	i, found := m.search(key)
	if found {
		cp := make([]byte, len(val))
		copy(cp, val)
		m.vals[i] = cp
		return nil
	}
	m.keys = append(m.keys, sql.Value{})
	m.vals = append(m.vals, nil)
	copy(m.keys[i+1:], m.keys[i:])
	copy(m.vals[i+1:], m.vals[i:])
	m.keys[i] = key
	cp := make([]byte, len(val))
	copy(cp, val)
	m.vals[i] = cp
	return nil
}

// Get 按键查找，返回值的副本。
func (m *MemBTree) Get(key sql.Value) ([]byte, bool) {
	i, found := m.search(key)
	if !found {
		return nil, false
	}
	cp := make([]byte, len(m.vals[i]))
	copy(cp, m.vals[i])
	return cp, true
}

// Delete 删除键，返回是否删到了东西。
func (m *MemBTree) Delete(key sql.Value) bool {
	i, found := m.search(key)
	if !found {
		return false
	}
	m.keys = append(m.keys[:i], m.keys[i+1:]...)
	m.vals = append(m.vals[:i], m.vals[i+1:]...)
	return true
}

// Scan 按键升序返回全部条目（键与值的副本）。
func (m *MemBTree) Scan() []KV {
	out := make([]KV, 0, len(m.keys))
	for i, k := range m.keys {
		cp := make([]byte, len(m.vals[i]))
		copy(cp, m.vals[i])
		out = append(out, KV{Key: k, Val: cp})
	}
	return out
}

// Len 返回条目数。
func (m *MemBTree) Len() int { return len(m.keys) }
