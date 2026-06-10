# Metric Filters: Semi-Joins and Anti-Joins the Semantic-Layer Way

This document records how gavagai expresses semi-join ("customers WITH
orders") and anti-join ("customers WITHOUT orders") semantics, why the design
deliberately avoids adding join vocabulary to the query IR, and how the
implementation maps onto machinery the compiler already had.

## The problem

The query IR is intentionally tiny: metrics + dimensions + filters + having +
order + limit. A plain filter compares **one column against a literal**:

```json
{ "field": "orders.status", "op": "=", "value": "complete" }
```

"Customers who have placed no orders" is not a column-vs-literal comparison —
it is a comparison against *the existence of rows in a related dataset*. The
classic SQL spellings (`EXISTS` / `NOT EXISTS`, `IN (SELECT …)` /
`NOT IN (SELECT …)`, `LEFT JOIN … IS NULL`) are *mechanisms*, and putting any
of them into the IR would leak SQL into the layer whose whole purpose is to
keep callers describing **intent**.

## What the established semantic layers do

We surveyed dbt MetricFlow, Cube, LookML, and the OSI spec before designing
this. They agree on the architecture gavagai already has — joins live in the
model (relationships/entities), never in the query; the query stays tiny —
and they solve existence filtering with **one** query-level construct:
filtering an entity by a related metric. dbt MetricFlow's form:

```
--where "{{ Metric('order_count', group_by=['customer']) }} > 0"   # semi-join
--where "{{ Metric('order_count', group_by=['customer']) }} = 0"   # anti-join
```

Semi-join and anti-join are not separate features in this worldview: they are
**thresholds on an aggregated metric**. MetricFlow renders this as a grouped
subquery LEFT JOINed on the entity — exactly the SQL shape shown below.

gavagai adopts this pattern directly (see `docs/pushdown-checklist.md` §2.3,
§2.4, §9 gates).

## The IR addition

One filter variant. When `metric` is set, the filter is a **metric filter**:

```json
{
  "metrics": ["customers.customer_count"],
  "filters": [
    { "metric": "orders.order_count",
      "group_by": "customers.customer_id",
      "op": "=", "value": 0 }
  ]
}
```

| Key | Meaning |
|---|---|
| `metric` | The model metric to aggregate (`dataset.metric_name`). |
| `group_by` | The **entity** to aggregate it to and join back on — a field of the outer dataset being filtered (`dataset.field_name`). It must be a plain column, and the metric's dataset must be reachable from the entity's dataset via the model's relationships. |
| `op` / `value` | Numeric comparison on the aggregated value: `=`, `!=`, `>`, `>=`, `<`, `<=`. |

Reading guide:

- `op: ">" , value: 0` → **semi-join**: keep entities that *have* related rows.
- `op: "=", value: 0` → **anti-join**: keep entities that have *none*.
- Any other threshold → existence with a floor/ceiling ("customers with
  ≥ 1000 revenue"), which plain EXISTS cannot even express.

Metric filters AND-combine with ordinary filters and may not appear inside
`or` groups.

## The generated SQL

For the anti-join example above:

```sql
SELECT
  COUNT(DISTINCT customer_id) AS "customer_count"
FROM analytics.customers AS "customers"
LEFT JOIN (
  SELECT
    "customers"."customer_id" AS "mf_key",
    COUNT(DISTINCT order_id) AS "order_count"
  FROM analytics.orders AS "orders"
  LEFT JOIN analytics.customers AS "customers"
    ON "orders"."customer_id" = "customers"."customer_id"
  GROUP BY "customers"."customer_id"
) AS "mf0_order_count"
  ON "customers"."customer_id" = "mf0_order_count"."mf_key"
WHERE COALESCE("mf0_order_count"."order_count", 0) = 0
```

Three properties carry the correctness argument:

1. **No duplication (semi-join safety).** The subquery `GROUP BY`s the
   entity, so it has *exactly one row per entity*. The LEFT JOIN can never
   multiply outer rows, no matter how many related rows exist.
2. **Null safety (anti-join safety).** Entities with no related rows get
   `NULL` from the LEFT JOIN. `COALESCE(metric, 0)` makes them compare as 0,
   so `= 0` is a null-safe anti-join — the `NOT IN (SELECT …)` NULL trap
   cannot occur, and no dialect-specific `NOT EXISTS` rewrite is needed.
   (The same coalescing means "fewer than 5 orders" includes customers with
   zero orders, which matches the intuitive reading.)
3. **Fan-out safety.** The metric is aggregated *inside* the subquery at the
   entity grain, before any join to the outer query — the same
   pre-aggregation argument used by the §2.5/§3.4 rewrites. `detectFanOut`
   runs on the subquery's own join path, so a metric whose path would
   duplicate its grain is rejected at plan time.

## How the implementation maps onto existing machinery

The feature is small because it composes pieces that already existed:

| Step | Mechanism | Where |
|---|---|---|
| Parse / validate | `Filter.Metric` + `Filter.GroupBy` fields; metric must exist, `group_by` must resolve, op numeric-comparison only, value numeric, not allowed inside `or` | `internal/query/types.go`, `internal/query/validate.go` |
| Resolve | `resolveMetricFilters` resolves the metric and entity against the model index | `internal/planner/metricfilter.go` |
| Build the subquery | `resolveJoins` (the ordinary BFS join resolver) connects the metric's dataset to the entity's dataset; `detectFanOut` checks the path; `qualifyAmbiguousColumns` pins the entity column when both datasets expose it bare; the entity is exposed under the fixed alias `mf_key` so the subquery's projection cannot collide with outer columns | `internal/planner/metricfilter.go` |
| Wrap and join | The aggregate is wrapped in a `SubqueryNode` (from the §8 subquery/CTE work) and LEFT JOINed onto the outer tree with an ordinary `JoinNode` | `applyMetricFilter` |
| Filter | A `Predicate` with `QualifyColumn` (from the §14 ambiguity work) referencing `mf<i>_<metric>.<metric>`, plus the new `CoalesceZero` flag | `internal/planner/nodes.go` |
| Push down | Nothing new: the predicate's dataset is the subquery alias, which matches no `ScanNode`, so `PushDown` correctly leaves it as a residual WHERE above the join — exactly where it must sit to see the LEFT JOIN's NULLs | `internal/planner/pushdown.go` (unchanged) |
| Emit | `joinSourceSQL` already renders a `SubqueryNode` on a join's right side (§8 work); the only codegen addition is wrapping the predicate in `COALESCE(expr, 0)` when `CoalesceZero` is set | `internal/codegen/sqlbuilder.go` |

New plan-node surface: **zero new node types** — one new `Predicate` field
(`CoalesceZero`). The subquery alias is `mf<i>_<metric_name>` (deterministic,
one per metric filter, collision-free with dataset aliases in practice).

## Limitations (deliberate, v1)

- `group_by` must be a **plain column** field. Expression entities would need
  expression-equality joins; declined with a clear error.
- Metric filters are **not supported on the pre-aggregated (fan-out) path**:
  if the outer query itself requires the §2.5 pre-aggregation rewrite, a
  metric filter makes `Plan` return the original `FanOutError`.
- The aggregated value coalesces to **0**, which is the right identity for
  COUNT/SUM-shaped metrics (the existence use case). A MIN/MAX/AVG metric
  filter compares missing entities as 0 too — acceptable, documented, and the
  same trade-off MetricFlow makes.
- One entity per metric filter (MetricFlow has the same constraint), which is
  what keeps the no-duplication guarantee provable.

## What this did NOT change

The query IR gained two optional fields on the existing `Filter` object —
no new top-level constructs, no join vocabulary, no SQL in the IR. Existing
queries are byte-for-byte unaffected, and every pre-existing test passes
unchanged.
