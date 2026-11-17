# minidb SQL 语义（M1）

本文档定义 M1 支持的 SQL 方言。凡是本文档没写的语义，一律返回错误，不猜。

## 支持的语句

```sql
CREATE TABLE users (id INT, name TEXT, age INT);
INSERT INTO users (id, name) VALUES (1, 'Alice'), (2, 'Bob');
INSERT INTO users VALUES (3, 'Carol', NULL);
SELECT * FROM users WHERE age > 26 ORDER BY age DESC LIMIT 2;
SELECT COUNT(*) FROM users;
SELECT COUNT(age) FROM users;
```

- 关键字大小写不敏感；表名、列名不区分大小写。
- 标识符：字母或下划线开头，后跟字母、数字、下划线。`count` 是保留字
  （`COUNT(...)` 专用），不能用作列名。
- `--` 是行注释；字符串用单引号，`''` 转义为一个单引号。
- 每条语句以 `;` 结尾（REPL 里可跨多行）。

## 列类型

| 声明 | 规范化 | 接受的值 |
| --- | --- | --- |
| `INT`、`INTEGER` | INT | 整数、NULL |
| `FLOAT`、`REAL` | FLOAT | 整数（自动提升为浮点）、浮点、NULL |
| `TEXT` | TEXT | 字符串、NULL |
| `BOOL`、`BOOLEAN` | BOOL | TRUE、FALSE、NULL |

不支持的类型（如 `VARCHAR`、`DATE`）在建表时就报错。

## 值与比较

值分五种：NULL、整数（int64）、浮点（float64）、字符串（UTF-8）、布尔。

- **NULL 最小**：任何值都大于 NULL；两个 NULL 相等。
- **Int 与 Float 比较按 float64 提升**：`1 = 1.0` 为真。
- **不同类型按固定顺序排**：NULL < 数字 < 字符串 < 布尔。
  也就是说 `1 < 'a'` 为真，`'a' < TRUE` 为真——这是本方言自定的顺序，
  不是 SQLite 的类型亲和。
- 字符串按字节序比较；布尔 false < true。

## 表达式

优先级：`OR` → `AND` → 比较（含 `IS [NOT] NULL`）→ 加减 → 乘除 →
一元（`-`、`NOT`）→ 原子（字面量、列、括号）。

- 比较：`= != <> < <= > >=`。任一操作数为 NULL 时结果为 NULL
  （不是 false——但 `WHERE` 只保留结果为真（Bool true）的行，
  所以 NULL 在 WHERE 里等效于不保留）。
- `IS NULL` / `IS NOT NULL`：判空的唯一正确写法。`= NULL` 永远得到 NULL，
  永远不保留行——这是 SQL 标准行为，不是 bug。
- 逻辑：`AND` / `OR` / `NOT` 按三值真值表：
  - `NULL AND FALSE = FALSE`，`NULL AND TRUE = NULL`；
  - `NULL OR TRUE = TRUE`，`NULL OR FALSE = NULL`；
  - `NOT NULL = NULL`。
- 算术：`+ - * /`。任一操作数为 NULL 时结果为 NULL；
  整数与整数运算结果仍为整数（除法是截断的整数除法，`7 / 2 = 3`）；
  只要有一方是浮点数，结果就是浮点数。
- **除零是错误**，不是 NULL 也不是 Inf：`1 / 0` 与 `1 / 0.0` 都报错。
- 非数字参与算术、非布尔参与逻辑都报错。

## SELECT

- 列表：`*`、列名、`COUNT(*)`、`COUNT(列)`。`COUNT` 不能和普通列混用；
  一条语句里可以有多个 `COUNT`（输出多列）。
- `COUNT(*)` 计全部行；`COUNT(列)` 只计该列非 NULL 的行。
- `WHERE` 只保留谓词为 Bool true 的行；谓词结果不是布尔也不是 NULL 时报错。
- `ORDER BY` 可写多列，各自 `ASC`（默认）或 `DESC`；NULL 最小，
  所以升序时 NULL 在最前，降序时 NULL 在最后；排序是稳定的。
  `ORDER BY` 可以引用不在 SELECT 列表里的列。
- `LIMIT n`：n 是非负整数；`LIMIT 0` 返回 0 行。
- `JOIN`：语法上能解析进 AST，但执行时明确报错（M5 才做）。

## INSERT

- 列列表可选：不写就是按建表顺序插全部列；写了就按列表顺序，
  没提到的列填 NULL。
- 值必须是字面量（数字、字符串、NULL、TRUE、FALSE、一元负号加数字），
  `VALUES (1+2)` 这样的表达式会被拒绝。
- 行长度与列数不一致、类型不匹配、列重复、表不存在时都报错，
  整条语句不插入任何行（校验在写入之前做完）。

## 错误

解析失败返回带行列与源码摘录的错误，例如：

```
第 1 行第 10 列：需要 FROM，但得到 "FORM"
SELECT * FORM t
         ^
```

不支持的语法、未知的表或列、类型错误都返回错误，绝不 panic。

## 与 SQLite 的差异点（M1 已知）

- 没有类型亲和：`'123'` 插进 INT 列会报错，SQLite 会存成整数 123。
- `1 / 0` 报错，SQLite 返回 NULL。
- 浮点打印用最短表示（`1.0` 打印成 `1`）。
- 没有 `AUTOINCREMENT`、没有默认值（没给的列就是 NULL）。
