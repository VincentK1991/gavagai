# Query Rewriting & Predicate Pushdown Checklist

Each item is a capability that the codegen layer must implement and verify with a dedicated test.
Check off an item only when: (a) the implementation is complete, (b) a test exercises the exact
pattern, and (c) `make lint test build` passes with zero suppressions.

---

## 1. Filter / Predicate Pushdown

### 1.1 Simple scalar predicates
- [ ] Push `WHERE col = value` below an Aggregate node (pre-filter before GROUP BY)
- [ ] Push `WHERE col != value` below an Aggregate node
- [ ] Push `WHERE col > value` / `col >= value` / `col < value` / `col <= value`
- [ ] Push `WHERE col IN (...)` below an Aggregate node
- [ ] Push `WHERE col NOT IN (...)` below an Aggregate node
- [ ] Push `WHERE col IS NULL` below an Aggregate node
- [ ] Push `WHERE col IS NOT NULL` below an Aggregate node

### 1.2 Multi-condition pushdown
- [ ] Push `WHERE a AND b` — both conditions pushed independently
- [ ] Push `WHERE a OR b` — only if the whole disjunction can be pushed (same table)
- [ ] Mixed AND/OR: split conjuncts, push each independently where safe

### 1.3 Pushdown through JOIN
- [ ] Push filter on left (fact) table below the JOIN (into the left scan)
- [ ] Push filter on right (dimension) table below the JOIN (into the right scan)
- [ ] Do NOT push a filter that references columns from both sides of the JOIN

### 1.4 Pushdown through subquery / CTE
- [ ] Push filter into an inline subquery when the predicate references only its output columns
- [ ] Push filter into a CTE definition when the CTE is referenced once and filter is safe

### 1.5 HAVING vs WHERE placement
- [ ] Scalar filter on a raw column → emitted as WHERE (pre-aggregate)
- [ ] Filter on an aggregate result (`revenue > 1000`) → emitted as HAVING (post-aggregate)
- [ ] Mixed query: scalar filter becomes WHERE, aggregate filter becomes HAVING — both correct
- [ ] HAVING with `COUNT(DISTINCT ...)`, `MIN(...)`, `MAX(...)` rendered correctly

---

## 2. JOIN Rewriting

### 2.1 Standard inner / left join
- [ ] Single-hop LEFT JOIN between two datasets
- [ ] Multi-hop LEFT JOIN (A → B → C) via intermediate dataset
- [ ] Join condition rendered as `ON left.col = right.col`
- [ ] Composite join key: multiple ON columns joined with AND

### 2.2 Self-join
- [ ] Same dataset joined to itself with distinct aliases (`a AS t1`, `a AS t2`)
- [ ] Self-join with a filter distinguishing the two roles (e.g. parent/child rows)
- [ ] Self-join fan-out detection: SUM/AVG/COUNT over the self-joined table raises error

### 2.3 Semi-join (EXISTS / IN subquery)
- [ ] Rewrite an inner join where only left-side columns are selected → `EXISTS` subquery
- [ ] Rewrite `WHERE id IN (SELECT id FROM ...)` as a semi-join plan node
- [ ] Semi-join does not duplicate left rows when right side has duplicates

### 2.4 Anti-join (NOT EXISTS / NOT IN)
- [ ] `NOT EXISTS` subquery pattern generated from anti-join plan node
- [ ] `NOT IN` subquery alternative (dialect choice)
- [ ] Null-safe anti-join: `NOT IN` with NULLs on right side → rewritten to `NOT EXISTS` or `LEFT JOIN ... IS NULL`

### 2.5 Fan-out-safe pre-aggregation before JOIN
- [ ] SUM metric on the one-side dataset → pre-aggregate before join to avoid fan-out
- [ ] AVG metric → pre-aggregate numerator and denominator separately, combine after join
- [ ] Fan-out detection raises `FanOutError` for unsafe metrics (blocks codegen until fixed)

---

## 3. Aggregation Rewriting

### 3.1 Basic GROUP BY
- [ ] GROUP BY all dimension columns
- [ ] GROUP BY with no dimensions → single-row aggregate (scalar subquery style)
- [ ] GROUP BY on expression dimension (e.g. `DATE_TRUNC('month', created_at)`)

### 3.2 COUNT variants
- [ ] `COUNT(*)` rendered correctly
- [ ] `COUNT(col)` rendered correctly (excludes NULLs)
- [ ] `COUNT(DISTINCT col)` rendered correctly
- [ ] `COUNT(DISTINCT col)` across a join (fan-out safe — does not double-count)

### 3.3 Aggregate on expression
- [ ] `SUM(price * quantity)` — expression inside aggregate rendered verbatim
- [ ] `AVG(CASE WHEN status = 'complete' THEN amount END)` — conditional aggregate

### 3.4 Pre-aggregation (push aggregation down)
- [ ] Push partial SUM to the inner subquery/CTE, then SUM the partial sums
- [ ] Push COUNT DISTINCT into a subquery before joining to avoid over-count
- [ ] Nested aggregation: inner query groups by fine grain, outer by coarse grain

### 3.5 ROLLUP / CUBE / GROUPING SETS (future, not phase 4)
- [ ] `GROUP BY ROLLUP(a, b)` rendered for dialects that support it
- [ ] `GROUP BY GROUPING SETS(...)` rendered correctly

---

## 4. DISTINCT Rewriting

- [ ] Top-level `SELECT DISTINCT` when query has no aggregates but dedup is needed
- [ ] `COUNT(DISTINCT col)` inside aggregate (see §3.2)
- [ ] DISTINCT pushed below JOIN to reduce cardinality before join
- [ ] DISTINCT on multi-column group (composite dedup key)
- [ ] Rewrite DISTINCT + GROUP BY to GROUP BY only (redundant DISTINCT removed)

---

## 5. LIMIT / OFFSET Pushdown

- [ ] LIMIT rendered at top of query
- [ ] LIMIT pushed into a subquery scan when no JOIN/aggregation is present
- [ ] LIMIT NOT pushed below aggregate (result set is already reduced)
- [ ] LIMIT NOT pushed below JOIN (row count can change)
- [ ] OFFSET rendered alongside LIMIT when present
- [ ] Dialect variants: `LIMIT n OFFSET m` (PostgreSQL) vs `LIMIT m, n` (MySQL) vs `FETCH FIRST n ROWS ONLY` (ANSI)

---

## 6. CASE WHEN Rewriting

### 6.1 In dimension expressions
- [ ] `CASE WHEN col = 'a' THEN 'label_a' ELSE 'other' END` as a dimension
- [ ] Nested CASE WHEN inside a dimension
- [ ] CASE WHEN with IS NULL / IS NOT NULL branches

### 6.2 In metric expressions
- [ ] `SUM(CASE WHEN status = 'complete' THEN amount ELSE 0 END)` — conditional sum
- [ ] `COUNT(CASE WHEN flag = true THEN 1 END)` — conditional count
- [ ] `AVG(CASE WHEN ...)` — conditional average

### 6.3 In filter predicates
- [ ] Filter on a CASE WHEN expression column (dimension filter, not pushed below aggregate)
- [ ] CASE WHEN used as a virtual boolean flag in WHERE clause

### 6.4 COALESCE / NULLIF (related null-handling rewrites)
- [ ] `COALESCE(col, default)` in dimension expression
- [ ] `NULLIF(col, 0)` to avoid divide-by-zero in AVG expressions

---

## 7. Date / Time Grain Rewriting

- [ ] `DATE_TRUNC('day', ts)` dimension — PostgreSQL dialect
- [ ] `DATE_TRUNC(ts, 'day')` dimension — BigQuery dialect (argument order differs)
- [ ] `DATE_TRUNC('month', ts)` / `'quarter'` / `'year'`
- [ ] `EXTRACT(DOW FROM ts)` vs `EXTRACT(DAYOFWEEK FROM ts)` dialect split
- [ ] Date arithmetic: `ts + INTERVAL '7 days'` vs `DATE_ADD(ts, INTERVAL 7 DAY)`
- [ ] Timezone conversion: `AT TIME ZONE` (PostgreSQL) vs `DATETIME(ts, tz)` (BigQuery)

---

## 8. Subquery and CTE Strategy

- [ ] Inline subquery: single-use derived table emitted as `(SELECT ...) AS alias`
- [ ] CTE: multi-use or recursive tables emitted as `WITH alias AS (SELECT ...)`
- [ ] CTE vs subquery selection: use CTE when the same logical table is referenced >1 time
- [ ] Nested CTEs: CTE that references another CTE
- [ ] Recursive CTE: hierarchical / graph traversal (future)
- [ ] Push predicates into CTE body when the CTE is used exactly once

---

## 9. NULL Handling Rewrites

- [ ] `IS NULL` / `IS NOT NULL` predicates (see §1.1)
- [ ] LEFT JOIN null check: `WHERE right.id IS NULL` → anti-join pattern
- [ ] `COALESCE` to replace NULL with a default in GROUP BY key (avoid null-group splitting)
- [ ] `COUNT(col)` excludes NULLs — contrast with `COUNT(*)` in test
- [ ] NULL-safe equality: `col IS NOT DISTINCT FROM value` (PostgreSQL) vs `col <=> value` (MySQL)

---

## 10. Window Function Rewrites (future — post phase 4)

- [ ] `ROW_NUMBER() OVER (PARTITION BY ... ORDER BY ...)` as a dimension
- [ ] `RANK()` / `DENSE_RANK()` window dimensions
- [ ] Running SUM: `SUM(col) OVER (ORDER BY ts ROWS UNBOUNDED PRECEDING)`
- [ ] Moving average: `AVG(col) OVER (ORDER BY ts ROWS 6 PRECEDING)`
- [ ] Pushdown: filter on window function result → must be in outer query, not pushed down

---

## 11. Dialect-Specific Rewrites

### 11.1 Identifier quoting
- [ ] PostgreSQL: double-quote identifiers (`"orders"."customer_id"`)
- [ ] BigQuery: backtick identifiers (`` `project.dataset.table` ``)
- [ ] Project/dataset prefix in BigQuery table refs: `my_project.analytics.orders`

### 11.2 String functions
- [ ] `CONCAT(a, b)` vs `a || b` (PostgreSQL / ANSI)
- [ ] `UPPER` / `LOWER` — same across dialects (no rewrite needed, verify)

### 11.3 Boolean literals
- [ ] PostgreSQL: `TRUE` / `FALSE`
- [ ] BigQuery: `TRUE` / `FALSE` (same — no rewrite, verify test)

### 11.4 Type casts
- [ ] `CAST(col AS INT64)` BigQuery vs `CAST(col AS INTEGER)` PostgreSQL
- [ ] `col::integer` PostgreSQL shorthand — not valid in BigQuery

### 11.5 LIMIT syntax (see §5)

### 11.6 Array / UNNEST
- [ ] `UNNEST(array_col)` as a lateral join source (BigQuery: `CROSS JOIN UNNEST(...)`)
- [ ] `UNNEST` with ordinality (PostgreSQL: `WITH ORDINALITY`)

---

## 12. Expression Passthrough

- [ ] Metric expression rendered verbatim from OSI dialect entry when available
- [ ] Fallback: `ANSI_SQL` dialect expression used when no target-dialect entry exists
- [ ] Error raised when no expression is available for the target dialect and no ANSI_SQL fallback
- [ ] Dimension field expression rendered correctly (not just column name)
- [ ] Nested expression: expression references another field expression by name (future)

---

## 13. ORDER BY Rewrites

- [ ] `ORDER BY col ASC` / `ORDER BY col DESC`
- [ ] `ORDER BY` on a metric alias (post-aggregate reference)
- [ ] `ORDER BY` on a dimension expression (must match GROUP BY expression exactly)
- [ ] `NULLS FIRST` / `NULLS LAST` (PostgreSQL) vs `IS NULL ASC` trick (BigQuery workaround)
- [ ] Multiple ORDER BY columns with mixed directions

---

## 14. Miscellaneous Safety Rules

- [ ] No cartesian product: error if two datasets have no join path and are both referenced
- [ ] Ambiguous column: error if the same column name exists in multiple joined datasets and no qualifier is used
- [ ] Cyclic join path: BFS handles cycles (already visited nodes skipped), verify no infinite loop
- [ ] Empty result set guard: queries with always-false predicates still produce valid SQL (no special casing)
- [ ] Integer overflow: LIMIT value fits in dialect's integer type

---

## Progress Summary

| Section | Total items | Done |
|---------|-------------|------|
| 1. Filter pushdown | 15 | 0 |
| 2. JOIN rewriting | 15 | 0 |
| 3. Aggregation rewriting | 12 | 0 |
| 4. DISTINCT | 5 | 0 |
| 5. LIMIT / OFFSET | 7 | 0 |
| 6. CASE WHEN | 11 | 0 |
| 7. Date/time grain | 7 | 0 |
| 8. Subquery / CTE | 7 | 0 |
| 9. NULL handling | 5 | 0 |
| 10. Window functions | 5 | 0 |
| 11. Dialect rewrites | 12 | 0 |
| 12. Expression passthrough | 5 | 0 |
| 13. ORDER BY | 5 | 0 |
| 14. Safety rules | 5 | 0 |
| **Total** | **116** | **0** |
