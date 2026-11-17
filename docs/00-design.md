# minidb 设计（M1：内存版）

minidb 是一个单进程、单文件的嵌入式 SQL 数据库，Go 实现，SQL 方言自定，
对标 SQLite 的常用子集，但不追求兼容 SQLite 的类型亲和等边角语义。

## 分层

从上到下四层，每层只依赖下一层：

```
SQL 文本
  → internal/sql      词法、语法树、解析器、值类型
  → internal/planner  SELECT → 计划树（SeqScan / Filter / Project / Sort / Limit / Count）
  → internal/executor 迭代器算子链（Next() 逐行吐数据）
  → internal/storage  行编解码 + 内存 B-tree + 表 + catalog
  → internal/db       顶层：解析 → 规划 → 执行，单写者一把锁
```

CLI（`cmd/minidb`）只调 `internal/db` 的公开接口：`New`、`Exec`、`Query`、
`Tables`、`Schema`、`Explain`、`FormatRows`。

## M1 的范围

能做：

- `CREATE TABLE`（列类型 INT / FLOAT / TEXT / BOOL）
- `INSERT INTO … VALUES …`（多行，列列表可选）
- `SELECT` + `WHERE` + `ORDER BY` + `LIMIT` + `COUNT(*)` / `COUNT(列)`
- 表达式：比较、AND / OR / NOT、IS [NOT] NULL、四则运算、一元负号
- 交互式 REPL（`.tables`、`.help`、`.quit`）与 `run <file.sql>`

不能做（后面里程碑的事）：

- 数据不落盘：进程退出数据就丢（M2 落盘）
- JOIN（M5）、CREATE INDEX / IndexScan（M4）、WAL 与崩溃恢复（M3）
- 事务（单条语句原子性都没有显式保证之外的东西；M1 只有单写者锁）
- GROUP BY / HAVING、ALTER TABLE、视图、触发器、外键
- 并发写、多进程

## 关键设计决策

### 内存 B-tree 的接口就是磁盘 B-tree 的接口

`internal/storage/btree.go` 定义了 `BTree` 接口（`Insert` / `Get` /
`Delete` / `Scan` / `Len`），M1 的 `MemBTree` 是排序切片 + 二分查找实现，
刻意保持与 B+ 树相同的有序扫描语义。M2 把磁盘 B+ 树接进来时，
`Table` 与执行层不需要任何改动。

### 行先序列化再存

`Table.Insert` 先把行 `EncodeRow` 成字节再放进 B-tree，`Scan` 时再
`DecodeRow`。行格式（`[列数 varint][每列：tag 1 字节 + 长度 varint + 数据]`）
是 M2 磁盘格式的同一份，M1 已经用它存行，M2 直接复用，不用重写编解码。
`DecodeRow` 对损坏输入只返回错误，绝不 panic。

### 表达式求值是三值逻辑

`WHERE` 只保留谓词为 Bool true 的行；比较与算术里任一操作数为 NULL
时结果为 NULL；AND / OR / NOT 按三值真值表算。实现在
`internal/executor` 的 `Eval`，语义表见 `docs/02-sql-semantics.md`。

### 规划层只做结构翻译，不做代价估算

`planner.Plan` 的工作是解析列、生成算子链、决定输出列名。
M1 没有索引，所以永远是 SeqScan；有 ORDER BY 就一定加 Sort。
`Explain` 接口已经立起来，M4 的 IndexScan 直接复用。

### 类型检查在写入时做，不在解析时做

`INSERT` 的值先按字面量解析，再由 `db.checkValue` 按列类型校验：
NULL 任何列都接受；INT 列只接受整数；FLOAT 列接受整数（提升为浮点）
或浮点；TEXT 只接受字符串；BOOL 只接受布尔。类型不对返回错误，不猜测转换。

## 测试策略

- 每包内 `_test.go`：lexer / parser 正例反例、Value 比较语义、
  record 编解码 round-trip 与损坏输入、MemBTree 有序扫描、
  planner 计划形状、executor 各算子、db 端到端。
- `test/e2e_test.go`：跑 `examples/basic.sql`，断言 5 条查询的结果。
- 差分测试（随机 SQL vs SQLite）是 M2 以后的事，M1 先把确定性行为测全。
