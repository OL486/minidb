package sql

import "strings"

// SplitStatements 按 ';' 切分 SQL 文本。分号在字符串里或行注释里时不切；
// 纯空白的片段会被丢掉。CLI 与测试共用这份实现。
func SplitStatements(src string) []string {
	var out []string
	var cur strings.Builder
	inStr := false
	inComment := false
	for i := 0; i < len(src); i++ {
		c := src[i]
		switch {
		case inComment:
			cur.WriteByte(c)
			if c == '\n' {
				inComment = false
			}
		case inStr:
			cur.WriteByte(c)
			if c == '\'' {
				if i+1 < len(src) && src[i+1] == '\'' {
					cur.WriteByte(src[i+1])
					i++
				} else {
					inStr = false
				}
			}
		case c == '-' && i+1 < len(src) && src[i+1] == '-':
			inComment = true
			cur.WriteByte(c)
			cur.WriteByte(src[i+1])
			i++
		case c == '\'':
			inStr = true
			cur.WriteByte(c)
		case c == ';':
			cur.WriteByte(c)
			if strings.TrimSpace(cur.String()) != ";" {
				out = append(out, cur.String())
			}
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	if strings.TrimSpace(cur.String()) != "" {
		out = append(out, cur.String())
	}
	return out
}

// IsSelect 粗略判断一条语句是不是 SELECT：去注释与前导空白后看关键字。
// CLI 用它分流“打印结果表”与“打印消息”，不做精确解析。
func IsSelect(stmt string) bool {
	s := strings.TrimSpace(stmt)
	for strings.HasPrefix(s, "--") {
		if i := strings.Index(s, "\n"); i >= 0 {
			s = strings.TrimSpace(s[i+1:])
		} else {
			return false
		}
	}
	return len(s) >= 6 && strings.EqualFold(s[:6], "SELECT")
}
