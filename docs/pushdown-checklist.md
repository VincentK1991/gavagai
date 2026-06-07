# Query Rewriting & Predicate Pushdown Checklist

Each item is a capability that the planner/codegen layer must implement and verify with a dedicated test.
Check off an item only when: (a) the implementation is complete, (b) a test exercises the exact
pattern, and (c) `make lint test build` passes with zero suppressions.

## Gating

This checklist is executable. Every item maps to a subtest in
`internal/conformance/` named by its id (e.g. `1.1/=`, `2.5/fan-out-detected`).
The subtest runs the real pipeline — parse → validate → plan → pushdown → codegen —
over an inline `(semantic model, query)` fixture.

- A subtest that **passes** ⇒ the box is checkable (annotated `← gate: <id>` below).
- A subtest that calls `pending(...)` is **skipped**: the rewrite/emitter is not
  implemented yet, or the query IR cannot yet express the case. Turning a box green
  is a two-step ritual: implement the behaviour, then delete the `pending()` call so
  the assertion runs.

Run the gates with `go test ./internal/conformance/... -v`. As of this commit:
**47 boxes green, 30 pending** (see the progress table at the bottom). The green
set now covers the full plan-level core (Phases 0–4) plus the Phase 5 PostgreSQL
emitter: filter/pushdown, join resolution, fan-out, GROUP BY, HAVING, ORDER BY,
LIMIT, SELECT DISTINCT, identifier quoting, table-path, expression passthrough,
and CASE WHEN/NULL rendering. The pending set is BigQuery codegen (Phase 6),
query-IR extensions (OR, semi/anti-join, OFFSET, window functions), and a few
planner rewrites (pre-aggregation, COALESCE/NULLIF).

---

## 1. Filter / Predicate Pushdown

### 1.1 Simple scalar predicates
- [x] Push `WHERE col = value` below an Aggregate node (pre-filter before GROUP BY) ← gate: `1.1/=`
- [x] Push `WHERE col != value` below an Aggregate node ← gate: `1.1/!=`
- [x] Push `WHERE col > value` / `col >= value` / `col < value` / `col <= value` ← gate: `1.1/>`, `1.1/>=`, `1.1/<`, `1.1/<=`
- [x] Push `WHERE col IN (...)` below an Aggregate node ← gate: `1.1/IN`
- [x] Push `WHERE col NOT IN (...)` below an Aggregate node ← gate: `1.1/NOT IN`
- [x] Push `WHERE col IS NULL` below an Aggregate node ← gate: `1.1/IS NULL`
- [x] Push `WHERE col IS NOT NULL` below an Aggregate node ← gate: `1.1/IS NOT NULL`

### 1.2 Multi-condition pushdown
- [x] Push `WHERE a AND b` — both conditions pushed independently ← gate: `1.2/AND`
- [ ] Push `WHERE a OR b` — only if the whole disjunction can be pushed (same table)
- [ ] Mixed AND/OR: split conjuncts, push each independently where safe

### 1.3 Pushdown through JOIN
- [x] Push filter on left (fact) table below the JOIN (into the left scan) ← gate: `1.3/push-into-left-scan`
- [x] Push filter on right (dimension) table below the JOIN (into the right scan) ← gate: `1.3/push-into-right-scan`
- [x] Mixed-dataset filters each pushed to their own scan; no FilterNode wraps the JoinNode ← gate: `1.3/cross-dataset-stays-above-join`
- [x] Pushdown is idempotent: applying PushDown twice yields the same tree ← gate: `1.3/pushdown-idempotent`

### 1.4 Pushdown through subquery / CTE
- [ ] Push filter into an inline subquery when the predicate references only its output columns
- [ ] Push filter into a CTE definition when the CTE is referenced once and filter is safe

### 1.5 HAVING vs WHERE placement
- [x] Scalar filter on a raw column → emitted as WHERE (pre-aggregate) ← gate: `1.5/where-vs-having`
- [x] Filter on an aggregate result (`revenue > 1000`) → emitted as HAVING (post-aggregate) ← gate: `1.5/where-vs-having`
- [x] Mixed query: scalar filter becomes WHERE, aggregate filter becomes HAVING — both correct ← gate: `1.5/where-vs-having`
- [ ] HAVING with `COUNT(DISTINCT ...)`, `MIN(...)`, `MAX(...)` rendered correctly

---

## 2. JOIN Rewriting

### 2.1 Standard inner / left join
- [x] Single-hop LEFT JOIN between two datasets ← gate: `2.1/single-hop-left`
- [x] Multi-hop LEFT JOIN (A → B → C) via intermediate dataset ← gate: `2.1/multi-hop`
- [ ] Join condition rendered as `ON left.col = right.col` (codegen)
- [x] Composite join key: multiple ON columns joined with AND ← gate: `2.1/composite-key` (plan-level; AND rendering pending)

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
- [x] Fan-out detection raises `FanOutError` for unsafe metrics (blocks codegen until fixed) ← gate: `2.5/fan-out-detected`, `2.5/fan-out-safe-metric-ok`

---

## 3. Aggregation Rewriting

### 3.1 Basic GROUP BY
- [x] GROUP BY all dimension columns ← gate: `3.1/group-by-dimensions`
- [x] GROUP BY with no dimensions → single-row aggregate (scalar subquery style) ← gate: `3.1/no-dimension-single-row`
- [x] GROUP BY on expression dimension (e.g. `DATE_TRUNC('month', created_at)`) ← gate: `3.1/expression-dimension`

### 3.2 COUNT variants
- [ ] `COUNT(*)` rendered correctly
- [ ] `COUNT(col)` rendered correctly (excludes NULLs)
- [x] `COUNT(DISTINCT col)` rendered correctly ← gate: `3.2/count-variants-render`
- [x] `COUNT(DISTINCT col)` across a join (fan-out safe — does not double-count) ← gate: `3.2/count-distinct-safe-across-join`

### 3.3 Aggregate on expression
- [ ] `SUM(price * quantity)` — expression inside aggregate rendered verbatim
- [x] `AVG(CASE WHEN status = 'complete' THEN amount END)` — conditional aggregate ← gate: `3.3/conditional-aggregate-expression`

### 3.4 Pre-aggregation (push aggregation down)
- [ ] Push partial SUM to the inner subquery/CTE, then SUM the partial sums
- [ ] Push COUNT DISTINCT into a subquery before joining to avoid over-count
- [ ] Nested aggregation: inner query groups by fine grain, outer by coarse grain

### 3.5 ROLLUP / CUBE / GROUPING SETS (future, not phase 4)
- [ ] `GROUP BY ROLLUP(a, b)` rendered for dialects that support it
- [ ] `GROUP BY GROUPING SETS(...)` rendered correctly

---

## 4. DISTINCT Rewriting

- [x] Top-level `SELECT DISTINCT` when query has no aggregates but dedup is needed ← gate: `4/distinct-render`
- [ ] `COUNT(DISTINCT col)` inside aggregate (see §3.2)
- [ ] DISTINCT pushed below JOIN to reduce cardinality before join
- [ ] DISTINCT on multi-column group (composite dedup key)
- [ ] Rewrite DISTINCT + GROUP BY to GROUP BY only (redundant DISTINCT removed)

---

## 5. LIMIT / OFFSET Pushdown

- [x] LIMIT rendered at top of query ← gate: `5/dialect-limit-syntax`
- [ ] LIMIT pushed into a subquery scan when no JOIN/aggregation is present
- [x] LIMIT NOT pushed below aggregate (result set is already reduced) ← gate: `5/limit-is-outermost`
- [x] LIMIT NOT pushed below JOIN (row count can change) ← gate: `5/limit-is-outermost`
- [ ] OFFSET rendered alongside LIMIT when present
- [ ] Dialect variants: `LIMIT n OFFSET m` (PostgreSQL) vs `LIMIT m, n` (MySQL) vs `FETCH FIRST n ROWS ONLY` (ANSI)

---

## 6. CASE WHEN Rewriting

### 6.1 In dimension expressions
- [x] `CASE WHEN col = 'a' THEN 'label_a' ELSE 'other' END` as a dimension ← gate: `6.1/case-dimension-render`
- [ ] Nested CASE WHEN inside a dimension
- [ ] CASE WHEN with IS NULL / IS NOT NULL branches

### 6.2 In metric expressions
- [ ] `SUM(CASE WHEN status = 'complete' THEN amount ELSE 0 END)` — conditional sum
- [ ] `COUNT(CASE WHEN flag = true THEN 1 END)` — conditional count
- [ ] `AVG(CASE WHEN ...)` — conditional average

### 6.3 In filter predicates
- [x] Filter on a CASE WHEN expression column (dimension filter, not pushed below aggregate) ← gate: `6.3/filter-on-case-dimension`
- [ ] CASE WHEN used as a virtual boolean flag in WHERE clause

### 6.4 COALESCE / NULLIF (related null-handling rewrites)
- [ ] `COALESCE(col, default)` in dimension expression
- [ ] `NULLIF(col, 0)` to avoid divide-by-zero in AVG expressions

---

## 7. Date / Time Grain Rewriting

- [x] `DATE_TRUNC('day', ts)` dimension — PostgreSQL dialect ← gate: `7/date-trunc-postgres`
- [x] `DATE_TRUNC(ts, 'day')` dimension — BigQuery dialect (argument order differs) ← gate: `7/date-trunc-bigquery`
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

- [x] `IS NULL` / `IS NOT NULL` predicates (see §1.1) ← gate: `9/is-null-predicate`
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
- [x] PostgreSQL: double-quote identifiers (`"orders"."customer_id"`) ← gate: `11/identifier-quoting`
- [ ] BigQuery: backtick identifiers (`` `project.dataset.table` ``) (Phase 6)
- [ ] Project/dataset prefix in BigQuery table refs: `my_project.analytics.orders` (Phase 6)

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

- [x] Metric expression rendered verbatim from OSI dialect entry when available ← gate: `12/verbatim-target-dialect`
- [x] Fallback: `ANSI_SQL` dialect expression used when no target-dialect entry exists ← gate: `12/ansi-fallback`
- [x] Error raised when no expression is available for the target dialect and no ANSI_SQL fallback ← gate: `12/missing-dialect-error`
- [x] Dimension field expression rendered correctly (not just column name) ← gate: `12/verbatim-target-dialect`
- [ ] Nested expression: expression references another field expression by name (future)

---

## 13. ORDER BY Rewrites

- [x] `ORDER BY col ASC` / `ORDER BY col DESC` ← gate: `13/directions-and-default`
- [x] `ORDER BY` on a metric alias (post-aggregate reference) ← gate: `13/directions-and-default`
- [x] `ORDER BY` on a dimension expression (must match GROUP BY expression exactly) ← gate: `13/directions-and-default` (alias form)
- [ ] `NULLS FIRST` / `NULLS LAST` (PostgreSQL) vs `IS NULL ASC` trick (BigQuery workaround)
- [x] Multiple ORDER BY columns with mixed directions ← gate: `13/directions-and-default`

---

## 14. Miscellaneous Safety Rules

- [x] No cartesian product: error if two datasets have no join path and are both referenced ← gate: `14/no-cartesian-product`
- [ ] Ambiguous column: error if the same column name exists in multiple joined datasets and no qualifier is used
- [x] Cyclic join path: BFS handles cycles (already visited nodes skipped), verify no infinite loop ← gate: `14/cyclic-join-path-terminates`
- [ ] Empty result set guard: queries with always-false predicates still produce valid SQL (no special casing)
- [ ] Integer overflow: LIMIT value fits in dialect's integer type

---

## Progress Summary

| Section | Total items | Done |
|---------|-------------|------|
| 1. Filter pushdown | 16 | 15 |
| 2. JOIN rewriting | 15 | 3 |
| 3. Aggregation rewriting | 12 | 6 |
| 4. DISTINCT | 5 | 1 |
| 5. LIMIT / OFFSET | 7 | 3 |
| 6. CASE WHEN | 11 | 2 |
| 7. Date/time grain | 7 | 2 |
| 8. Subquery / CTE | 7 | 0 |
| 9. NULL handling | 5 | 1 |
| 10. Window functions | 5 | 0 |
| 11. Dialect rewrites | 12 | 2 |
| 12. Expression passthrough | 5 | 4 |
| 13. ORDER BY | 5 | 3 |
| 14. Safety rules | 5 | 2 |
| **Total** | **117** | **47** |
