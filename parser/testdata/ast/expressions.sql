-- Expression forms, one per line, exercising every node in ast/expr.go and
-- the eleven precedence levels of parse.y's %left/%right block.
SELECT a OR b AND NOT c;
SELECT 1 + 2 * 3 - 4 / 5 % 6;
SELECT (1 + 2) * 3;
SELECT 1 < 2 = 3, 1 & 2 | 3 << 4 >> 5;
SELECT 'a' || 'b' -> 'c' ->> 'd';
SELECT -x, +x, ~x, NOT x;
SELECT - -x, 1 - -2;
SELECT x COLLATE nocase, -x COLLATE binary;
SELECT x IS y, x IS NOT y, x IS DISTINCT FROM y, x IS NOT DISTINCT FROM y;
SELECT x ISNULL, x NOTNULL, x NOT NULL;
SELECT x LIKE y, x NOT LIKE y ESCAPE '\', x GLOB y, x REGEXP y, x MATCH y;
SELECT x BETWEEN 1 AND 2, x NOT BETWEEN 1 AND 2 AND z;
SELECT x IN (), x IN (1, 2), x IN (SELECT 1), x NOT IN tbl, x IN sch.tbl(1);
SELECT CASE WHEN a THEN b END, CASE a WHEN b THEN c ELSE d END;
SELECT CAST(x AS UNSIGNED BIG INT), CAST(x AS decimal(10, -2)), CAST(x AS);
SELECT count(*), count(DISTINCT a), group_concat(a ORDER BY b DESC), max(ALL a);
SELECT EXISTS (SELECT 1), (SELECT 1), (a, b) = (1, 2);
SELECT NULL, 1, 1.5, 1e3, 0xff, 1_000, 'str', x'ab', CURRENT_TIMESTAMP;
SELECT ?, ?12, :name, @name, $foo::bar(baz);
SELECT a, t.a, sch.t.a, "quoted", `back`, [square], 'a'.'b';
