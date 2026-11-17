-- minidb M1 示例：建表、插入、查询。
-- 跑法：minidb run examples/basic.sql

CREATE TABLE users (id INT, name TEXT, age INT);

INSERT INTO users (id, name, age) VALUES (1, 'Alice', 30), (2, 'Bob', 25), (3, 'Carol', NULL);
INSERT INTO users VALUES (4, 'Dave', 40);

-- 全部行。
SELECT * FROM users;

-- WHERE + ORDER BY + LIMIT。
SELECT name, age FROM users WHERE age > 26 ORDER BY age DESC LIMIT 2;

-- 聚合：COUNT(*) 计全部行，COUNT(age) 跳过 NULL。
SELECT COUNT(*) FROM users;
SELECT COUNT(age) FROM users;

-- IS NULL。
SELECT name FROM users WHERE age IS NULL;
