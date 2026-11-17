// Package storage 提供 minidb 的存储层。
//
// M1 只有内存实现：行按规划的格式序列化后存在内存 B-tree 里；
// 磁盘实现（pager、WAL、catalog 落盘）是 M2、M3 的事，接口保持不变。
package storage

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/OL486/minidb/internal/sql"
)

// 行序列化格式（规划 docs/01-storage-format.md 的 M2 版提前实现，
// M1 的内存表已经用它存行，M2 的磁盘 B-tree 直接复用）：
//
//	[列数 varint][每列：类型 tag 1 字节 + 长度 varint + 数据]
//
// tag：0=NULL（无数据），1=Int（8 字节大端），2=Float（8 字节 IEEE754 大端），
// 3=Str（长度 varint + UTF-8 字节），4=Bool（1 字节 0/1）。
const (
	tagNull  = 0
	tagInt   = 1
	tagFloat = 2
	tagStr   = 3
	tagBool  = 4
)

// EncodeRow 把一行值序列化成字节。结果可直接用作 B-tree 的值。
func EncodeRow(row []sql.Value) []byte {
	var b []byte
	b = binary.AppendUvarint(b, uint64(len(row)))
	for _, v := range row {
		switch v.Kind {
		case sql.KindNull:
			b = append(b, tagNull)
		case sql.KindInt:
			b = append(b, tagInt)
			var tmp [8]byte
			binary.BigEndian.PutUint64(tmp[:], uint64(v.Int))
			b = append(b, tmp[:]...)
		case sql.KindFloat:
			b = append(b, tagFloat)
			var tmp [8]byte
			binary.BigEndian.PutUint64(tmp[:], math.Float64bits(v.Float))
			b = append(b, tmp[:]...)
		case sql.KindStr:
			b = append(b, tagStr)
			b = binary.AppendUvarint(b, uint64(len(v.Str)))
			b = append(b, v.Str...)
		case sql.KindBool:
			b = append(b, tagBool)
			if v.Bool {
				b = append(b, 1)
			} else {
				b = append(b, 0)
			}
		}
	}
	return b
}

// DecodeRow 把 EncodeRow 的输出还原成一行值。输入损坏时返回错误，
// 绝不 panic（这是 M1 的硬性要求：坏数据只报错）。
func DecodeRow(b []byte) ([]sql.Value, error) {
	n, m := binary.Uvarint(b)
	if m <= 0 {
		return nil, fmt.Errorf("行数据损坏：列数 varint 无效")
	}
	rest := b[m:]
	row := make([]sql.Value, 0, n)
	for i := uint64(0); i < n; i++ {
		if len(rest) == 0 {
			return nil, fmt.Errorf("行数据损坏：第 %d 列缺 tag", i)
		}
		tag := rest[0]
		rest = rest[1:]
		var v sql.Value
		switch tag {
		case tagNull:
			v = sql.Null()
		case tagInt:
			if len(rest) < 8 {
				return nil, fmt.Errorf("行数据损坏：第 %d 列 Int 只有 %d 字节", i, len(rest))
			}
			v = sql.IntVal(int64(binary.BigEndian.Uint64(rest[:8])))
			rest = rest[8:]
		case tagFloat:
			if len(rest) < 8 {
				return nil, fmt.Errorf("行数据损坏：第 %d 列 Float 只有 %d 字节", i, len(rest))
			}
			v = sql.FloatVal(math.Float64frombits(binary.BigEndian.Uint64(rest[:8])))
			rest = rest[8:]
		case tagStr:
			ln, m := binary.Uvarint(rest)
			if m <= 0 {
				return nil, fmt.Errorf("行数据损坏：第 %d 列字符串长度 varint 无效", i)
			}
			rest = rest[m:]
			if uint64(len(rest)) < ln {
				return nil, fmt.Errorf("行数据损坏：第 %d 列字符串要 %d 字节只剩 %d 字节", i, ln, len(rest))
			}
			v = sql.StrVal(string(rest[:ln]))
			rest = rest[ln:]
		case tagBool:
			if len(rest) < 1 {
				return nil, fmt.Errorf("行数据损坏：第 %d 列 Bool 缺数据", i)
			}
			if rest[0] > 1 {
				return nil, fmt.Errorf("行数据损坏：第 %d 列 Bool 值为 %d", i, rest[0])
			}
			v = sql.BoolVal(rest[0] == 1)
			rest = rest[1:]
		default:
			return nil, fmt.Errorf("行数据损坏：第 %d 列未知 tag %d", i, tag)
		}
		row = append(row, v)
	}
	return row, nil
}
