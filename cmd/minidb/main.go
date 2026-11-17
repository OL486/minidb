// minidb 是 minidb 的命令行入口（M1：内存版）。
//
// 用法：
//
//	minidb run <file.sql>   执行 SQL 文件里的全部语句
//	minidb                  交互式 REPL（每行一条 SQL，以 ; 结尾）
//
// REPL 点命令：.tables 列出表，.help 显示帮助，.quit 退出。
package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/OL486/minidb/internal/db"
	"github.com/OL486/minidb/internal/sql"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	switch {
	case len(args) == 0:
		return repl(db.New(), os.Stdin, os.Stdout)
	case len(args) == 2 && args[0] == "run":
		return runFile(db.New(), args[1], os.Stdout, os.Stderr)
	default:
		fmt.Fprintln(os.Stderr, "用法：")
		fmt.Fprintln(os.Stderr, "  minidb run <file.sql>   执行 SQL 文件里的全部语句")
		fmt.Fprintln(os.Stderr, "  minidb                  交互式 REPL")
		return 1
	}
}

// runFile 执行 SQL 文件里的全部语句。遇到第一个错误就停，返回 1。
func runFile(d *db.DB, path string, out, errOut io.Writer) int {
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(errOut, "读文件失败：%v\n", err)
		return 1
	}
	for _, stmt := range sql.SplitStatements(string(data)) {
		if err := execOne(d, stmt, out); err != nil {
			fmt.Fprintf(errOut, "Error: %v\n", err)
			return 1
		}
	}
	return 0
}

// repl 是交互式循环：累积输入直到一行以 ';' 结尾再执行；
// 点命令立即执行；空输入跳过；错误打印后继续；Ctrl-D 退出。
func repl(d *db.DB, in io.Reader, out io.Writer) int {
	fmt.Fprintln(out, "minidb M1（内存版，数据不落盘）。输入 SQL，以 ; 结尾执行；.help 看帮助，.quit 退出。")
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var buf strings.Builder
	for {
		if buf.Len() == 0 {
			fmt.Fprint(out, "minidb> ")
		} else {
			fmt.Fprint(out, "    -> ")
		}
		if !sc.Scan() {
			break
		}
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if buf.Len() == 0 && strings.HasPrefix(line, ".") {
			if !dotCommand(d, line, out) {
				return 0
			}
			continue
		}
		buf.WriteString(line)
		buf.WriteString("\n")
		if !strings.HasSuffix(line, ";") {
			continue
		}
		stmt := buf.String()
		buf.Reset()
		if err := execOne(d, stmt, out); err != nil {
			fmt.Fprintf(out, "Error: %v\n", err)
		}
	}
	if err := sc.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "读输入失败：%v\n", err)
		return 1
	}
	return 0
}

// dotCommand 执行 REPL 点命令。返回 false 表示退出 REPL。
func dotCommand(d *db.DB, line string, out io.Writer) bool {
	fields := strings.Fields(line)
	switch fields[0] {
	case ".quit", ".exit":
		return false
	case ".tables":
		tables := d.Tables()
		if len(tables) == 0 {
			fmt.Fprintln(out, "(no tables)")
			return true
		}
		for _, t := range tables {
			fmt.Fprintln(out, t)
		}
		return true
	case ".help":
		fmt.Fprintln(out, "点命令：")
		fmt.Fprintln(out, "  .tables   列出全部表")
		fmt.Fprintln(out, "  .help     显示本帮助")
		fmt.Fprintln(out, "  .quit     退出")
		fmt.Fprintln(out, "SQL：每条语句以 ; 结尾，可跨多行。")
		return true
	default:
		fmt.Fprintf(out, "未知的点命令 %s，输入 .help 看帮助\n", fields[0])
		return true
	}
}

// execOne 执行一条 SQL：SELECT 打印结果表，其他语句打印消息。
// 语句是不是 SELECT 靠关键字判断：解析交给 db 层，这里只分流打印。
func execOne(d *db.DB, stmt string, out io.Writer) error {
	trimmed := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(stmt), ";"))
	if trimmed == "" {
		return nil
	}
	if sql.IsSelect(trimmed) {
		rows, cols, err := d.Query(trimmed)
		if err != nil {
			return err
		}
		fmt.Fprint(out, db.FormatRows(cols, rows))
		return nil
	}
	res, err := d.Exec(trimmed)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, res.Message)
	return nil
}
