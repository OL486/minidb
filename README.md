# minidb

从零写的嵌入式 SQL 数据库（Go，零外部依赖）。和 [minilang](https://github.com/OL486/minilang)
同一系列的 from-scratch 作品：语言有了，数据库是它底下缺的那层存储。

## 当前状态：M1（内存版）

M1 全部在内存里跑通：`CREATE TABLE` / `INSERT` / `SELECT` + `WHERE` /
`ORDER BY` / `LIMIT` / `COUNT(*)` / `COUNT(列)`，表达式支持比较、AND / OR / NOT、
`IS [NOT] NULL`、四则运算。解析失败返回带行列的错误，不 panic。

## 能跑什么、不能跑什么

能跑：

- 建表、插入（多行、列列表可选）、查询、过滤、排序、分页、计数
- 交互式 REPL（`.tables`、`.help`、`.quit`）与 `run <file.sql>`

不能跑（后面里程碑的事）：

- **数据不落盘，进程退出就丢**——这是 M2 的事（pager + B+ 树落盘）
- JOIN（M5）、CREATE INDEX（M4）、WAL 与崩溃恢复（M3）
- 事务、GROUP BY、ALTER TABLE、并发写

完整语义见 [docs/02-sql-semantics.md](docs/02-sql-semantics.md)，
设计见 [docs/00-design.md](docs/00-design.md)。

## 怎么跑

```sh
# 需要 Go 1.22+。工具链在 ~/go/bin 时先加 PATH：
export PATH="$HOME/go/bin:$PATH"

# 跑示例
make examples
# 也就是：
go run ./cmd/minidb run examples/basic.sql

# 交互式
go run ./cmd/minidb
minidb> CREATE TABLE t (id INT, name TEXT);
minidb> INSERT INTO t VALUES (1, 'Alice');
minidb> SELECT * FROM t;
minidb> .quit
```

示例输出：

```
表 users 已创建
插入 3 行
插入 1 行
id | name | age
1 | Alice | 30
2 | Bob | 25
3 | Carol | NULL
4 | Dave | 40
(4 rows)
```

## 怎么测

```sh
make check   # go vet + go test ./...
make bench   # M1 不测性能，目标先占位，M5 接入
```

## 结构

```
cmd/minidb/            CLI：run <file.sql>、REPL
internal/sql/          词法、语法树、解析器、值类型、脚本切分
internal/planner/      SELECT → 计划树、Explain
internal/executor/     迭代器算子链（SeqScan / Filter / Project / Sort / Limit / Count）
internal/storage/      行编解码、内存 B-tree、表、catalog
internal/db/           顶层：Exec / Query / Explain
test/                  端到端：跑 examples/basic.sql
examples/basic.sql     建表、插入、查询示例
docs/                  设计与语义文档
```
