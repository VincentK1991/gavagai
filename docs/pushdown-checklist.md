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
**80 boxes green, 39 pending** (see the progress table at the bottom). The green
set covers the full plan-level core (Phases 0–4) plus both SQL emitters
(Phase 5 PostgreSQL, Phase 6 BigQuery): filter/pushdown (including OR /
mixed AND-OR disjunctions), join resolution and ON/composite-key rendering,
fan-out, GROUP BY, HAVING, ORDER BY (with NULLS FIRST/LAST), LIMIT/OFFSET,
SELECT DISTINCT, COUNT variants (`*`, col, DISTINCT), conditional aggregates
(SUM/COUNT/AVG of CASE), DATE_TRUNC grains, COALESCE/NULLIF, dialect-divergent
expression passthrough (identifier quoting, CAST, string concat, boolean
literals), CASE WHEN/NULL rendering, subquery/CTE emission (inline derived
tables, WITH clauses, CTE-vs-subquery selection, nested CTEs, and
predicate-pushdown into the subquery/CTE body, via `planner.Materialize`), and
fan-out-safe pre-aggregation (each metric aggregated on its own grain in an
isolated subquery, combined by a shared-dimension or cross join, via
`planner.planPreAggregated`). The pending set is the larger features that still
need new plan-node types or codegen machinery: self/semi/anti-joins, window
functions, ROLLUP/CUBE, recursive CTEs, nested fine→coarse aggregation, and
ambiguous-column qualification.

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
- [x] Push `WHERE a OR b` — only if the whole disjunction can be pushed (same table) ← gate: `1.2/OR`, `1.2/OR-cross-dataset-stays-above-join`
- [x] Mixed AND/OR: split conjuncts, push each independently where safe ← gate: `1.2/mixed-and-or`

### 1.3 Pushdown through JOIN
- [x] Push filter on left (fact) table below the JOIN (into the left scan) ← gate: `1.3/push-into-left-scan`
- [x] Push filter on right (dimension) table below the JOIN (into the right scan) ← gate: `1.3/push-into-right-scan`
- [x] Mixed-dataset filters each pushed to their own scan; no FilterNode wraps the JoinNode ← gate: `1.3/cross-dataset-stays-above-join`
- [x] Pushdown is idempotent: applying PushDown twice yields the same tree ← gate: `1.3/pushdown-idempotent`

### 1.4 Pushdown through subquery / CTE
- [x] Push filter into an inline subquery when the predicate references only its output columns ← gate: `1.4/into-subquery`
- [x] Push filter into a CTE definition when the CTE is referenced once and filter is safe ← gate: `1.4/into-cte`

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
- [x] Join condition rendered as `ON left.col = right.col` (codegen) ← gate: `2.1/on-condition-render`
- [x] Composite join key: multiple ON columns joined with AND ← gate: `2.1/composite-key` (plan), `2.1/composite-key-render` (AND rendering)

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
- [x] SUM metric on the one-side dataset → pre-aggregate before join to avoid fan-out ← gate: `2.5/sum-pre-aggregated`
- [x] AVG metric → pre-aggregate, combine after join (gavagai computes AVG on its own grain, where rows are un-fanned, so no numerator/denominator split is needed) ← gate: `2.5/avg-pre-aggregated`
- [x] Fan-out detection raises `FanOutError` for unsafe metrics when no safe pre-aggregation exists (e.g. many-to-many attribution) ← gate: `2.5/fan-out-detected`, `2.5/fan-out-safe-metric-ok`

---

## 3. Aggregation Rewriting

### 3.1 Basic GROUP BY
- [x] GROUP BY all dimension columns ← gate: `3.1/group-by-dimensions`
- [x] GROUP BY with no dimensions → single-row aggregate (scalar subquery style) ← gate: `3.1/no-dimension-single-row`
- [x] GROUP BY on expression dimension (e.g. `DATE_TRUNC('month', created_at)`) ← gate: `3.1/expression-dimension`

### 3.2 COUNT variants
- [x] `COUNT(*)` rendered correctly ← gate: `3.2/count-star-render`
- [x] `COUNT(col)` rendered correctly (excludes NULLs) ← gate: `3.2/count-col-render`
- [x] `COUNT(DISTINCT col)` rendered correctly ← gate: `3.2/count-variants-render`
- [x] `COUNT(DISTINCT col)` across a join (fan-out safe — does not double-count) ← gate: `3.2/count-distinct-safe-across-join`

### 3.3 Aggregate on expression
- [x] `SUM(price * quantity)` — expression inside aggregate rendered verbatim ← gate: `3.3/aggregate-on-expression`
- [x] `AVG(CASE WHEN status = 'complete' THEN amount END)` — conditional aggregate ← gate: `3.3/conditional-aggregate-expression`

### 3.4 Pre-aggregation (push aggregation down)
- [x] Push partial SUM to the inner subquery/CTE, then combine the partials (no outer re-aggregation needed: each grain is summed once in its own subquery) ← gate: `3.4/partial-aggregate-in-subquery`
- [x] Push COUNT DISTINCT into a subquery before joining to avoid over-count ← gate: `3.4/count-distinct-pre-aggregated`
- [ ] Nested aggregation: inner query groups by fine grain, outer by coarse grain (needs a metric-of-metric IR construct)

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
- [x] OFFSET rendered alongside LIMIT when present ← gate: `5/offset`
- [x] Dialect variants: `LIMIT n OFFSET m` — PostgreSQL and BigQuery (the two supported dialects) agree on this form ← gate: `5/dialect-limit-offset-form`

---

## 6. CASE WHEN Rewriting

### 6.1 In dimension expressions
- [x] `CASE WHEN col = 'a' THEN 'label_a' ELSE 'other' END` as a dimension ← gate: `6.1/case-dimension-render`
- [ ] Nested CASE WHEN inside a dimension
- [ ] CASE WHEN with IS NULL / IS NOT NULL branches

### 6.2 In metric expressions
- [x] `SUM(CASE WHEN status = 'complete' THEN amount ELSE 0 END)` — conditional sum ← gate: `6.2/case-metric-render`
- [x] `COUNT(CASE WHEN flag = true THEN 1 END)` — conditional count ← gate: `6.2/case-metric-render`
- [x] `AVG(CASE WHEN ...)` — conditional average ← gate: `6.2/case-metric-render`

### 6.3 In filter predicates
- [x] Filter on a CASE WHEN expression column (dimension filter, not pushed below aggregate) ← gate: `6.3/filter-on-case-dimension`
- [ ] CASE WHEN used as a virtual boolean flag in WHERE clause

### 6.4 COALESCE / NULLIF (related null-handling rewrites)
- [x] `COALESCE(col, default)` in dimension expression ← gate: `6.4/coalesce-nullif`
- [x] `NULLIF(col, 0)` to avoid divide-by-zero in AVG expressions ← gate: `6.4/coalesce-nullif`

---

## 7. Date / Time Grain Rewriting

- [x] `DATE_TRUNC('day', ts)` dimension — PostgreSQL dialect ← gate: `7/date-trunc-postgres`
- [x] `DATE_TRUNC(ts, 'day')` dimension — BigQuery dialect (argument order differs) ← gate: `7/date-trunc-bigquery`
- [x] `DATE_TRUNC('month', ts)` / `'quarter'` / `'year'` ← gate: `7/date-trunc-grains`
- [ ] `EXTRACT(DOW FROM ts)` vs `EXTRACT(DAYOFWEEK FROM ts)` dialect split
- [ ] Date arithmetic: `ts + INTERVAL '7 days'` vs `DATE_ADD(ts, INTERVAL 7 DAY)`
- [ ] Timezone conversion: `AT TIME ZONE` (PostgreSQL) vs `DATETIME(ts, tz)` (BigQuery)

---

## 8. Subquery and CTE Strategy

- [x] Inline subquery: single-use derived table emitted as `(SELECT ...) AS alias` ← gate: `8/inline-subquery`
- [x] CTE: multi-use or recursive tables emitted as `WITH alias AS (SELECT ...)` ← gate: `8/cte`
- [x] CTE vs subquery selection: use CTE when the same logical table is referenced >1 time ← gate: `8/cte-vs-subquery-choice`
- [x] Nested CTEs: CTE that references another CTE ← gate: `8/nested-cte`
- [ ] Recursive CTE: hierarchical / graph traversal (future)
- [x] Push predicates into CTE body when the CTE is used exactly once ← gate: `8/push-predicate-into-cte`

---

## 9. NULL Handling Rewrites

- [x] `IS NULL` / `IS NOT NULL` predicates (see §1.1) ← gate: `9/is-null-predicate`
- [ ] LEFT JOIN null check: `WHERE right.id IS NULL` → anti-join pattern
- [ ] `COALESCE` to replace NULL with a default in GROUP BY key (avoid null-group splitting)
- [x] `COUNT(col)` excludes NULLs — contrast with `COUNT(*)` in test ← gate: `9/count-col-excludes-null`
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
- [x] BigQuery: backtick identifiers (`` `orders`.`customer_id` ``) ← gate: `11/bigquery-backtick-quoting`, `11/dialect-divergence`
- [x] Project/dataset prefix in BigQuery table refs: `my_project.analytics.orders` ← gate: bigquery `TestBackquoteQuoting` + golden files

### 11.2 String functions
- [x] `CONCAT(a, b)` vs `a || b` (PostgreSQL / ANSI) ← gate: `11/casts-and-string-fns`
- [x] `UPPER` / `LOWER` — same across dialects (no rewrite needed, verify) ← gate: `11/casts-and-string-fns`

### 11.3 Boolean literals
- [x] PostgreSQL: `TRUE` / `FALSE` ← gate: `11/casts-and-string-fns`
- [x] BigQuery: `TRUE` / `FALSE` (same — no rewrite, verify test) ← gate: `11/casts-and-string-fns`

### 11.4 Type casts
- [x] `CAST(col AS INT64)` BigQuery vs `CAST(col AS INTEGER)` PostgreSQL ← gate: `11/casts-and-string-fns`
- [x] `col::integer` PostgreSQL shorthand — not valid in BigQuery ← gate: `11/casts-and-string-fns`

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
- [x] `NULLS FIRST` / `NULLS LAST` — supported natively by both PostgreSQL and BigQuery ← gate: `13/nulls-first-last`
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
| 1. Filter pushdown | 20 | 19 |
| 2. JOIN rewriting | 16 | 7 |
| 3. Aggregation rewriting | 14 | 11 |
| 4. DISTINCT | 5 | 1 |
| 5. LIMIT / OFFSET | 6 | 5 |
| 6. CASE WHEN | 10 | 7 |
| 7. Date/time grain | 6 | 3 |
| 8. Subquery / CTE | 6 | 5 |
| 9. NULL handling | 5 | 2 |
| 10. Window functions | 5 | 0 |
| 11. Dialect rewrites | 11 | 9 |
| 12. Expression passthrough | 5 | 4 |
| 13. ORDER BY | 5 | 5 |
| 14. Safety rules | 5 | 2 |
| **Total** | **119** | **80** |
