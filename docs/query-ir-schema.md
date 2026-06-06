# Query IR Schema

The query IR (intermediate representation) is the stable interface between a caller (LLM agent, BI tool, or human) and the `gavagai` compiler. It names what the caller wants in terms of the semantic model — no SQL, no dialect knowledge required.

The compiler validates every reference against the loaded semantic model before emitting any SQL. Unknown metrics, fields, or operators produce explicit errors rather than silently wrong SQL.

---

## Format

The query IR is a **JSON object** with the following top-level keys. All keys except the selection (metrics / dimensions) are optional.

```json
{
  "metrics":    ["<dataset>.<metric_name>", ...],
  "dimensions": ["<dataset>.<field_name>",  ...],
  "filters":    [ <Filter>,    ... ],
  "having":     [ <Having>,    ... ],
  "order_by":   [ <OrderItem>, ... ],
  "limit":      <integer>
}
```

**Constraint:** at least one of `metrics` or `dimensions` must be non-empty.

---

## Reference format

All references use dot-qualified names: `dataset_name.element_name`.

- `dataset_name` — the logical dataset name from the semantic model (e.g. `orders`, `customers`). This is the OSI `Dataset.name`, **not** the physical source path.
- `element_name` — the metric name (from model-level `metrics`) or field name (from `dataset.fields`).

**Examples:**
- `orders.revenue` — metric `revenue` from the model, with `orders` as the primary source dataset.
- `customers.region` — field `region` from the `customers` dataset.

---

## `Filter`

```json
{
  "field": "<dataset>.<field_name>",
  "op":    "<operator>",
  "value": <scalar | array>
}
```

| Key | Type | Required | Description |
|---|---|---|---|
| `field` | string | yes | Dot-qualified field reference |
| `op` | string | yes | Comparison operator (see table below) |
| `value` | any | conditional | Required for all operators except `IS NULL` / `IS NOT NULL` |

### Filter operators

| Operator | Value type | Example |
|---|---|---|
| `=` | scalar | `"value": "complete"` |
| `!=` | scalar | `"value": "cancelled"` |
| `>` | number | `"value": 1000` |
| `>=` | number | `"value": 0` |
| `<` | number | `"value": 500` |
| `<=` | number | `"value": 999` |
| `IN` | array | `"value": ["EMEA", "APAC"]` |
| `NOT IN` | array | `"value": ["pending", "cancelled"]` |
| `IS NULL` | — | no value |
| `IS NOT NULL` | — | no value |

---

## `Having`

Post-aggregation predicate applied after grouping (maps to SQL `HAVING`).

```json
{
  "metric": "<dataset>.<metric_name>",
  "op":     "<operator>",
  "value":  <number>
}
```

| Key | Type | Required | Description |
|---|---|---|---|
| `metric` | string | yes | Dot-qualified metric reference |
| `op` | string | yes | One of `=`, `!=`, `>`, `>=`, `<`, `<=` |
| `value` | number | yes | Numeric threshold |

---

## `OrderItem`

```json
{
  "field":     "<dataset>.<field_or_metric_name>",
  "direction": "ASC | DESC"
}
```

| Key | Type | Required | Description |
|---|---|---|---|
| `field` | string | yes | Dot-qualified reference to a selected metric or dimension |
| `direction` | string | no | `ASC` or `DESC`; defaults to `ASC` if omitted |

---

## Full example

**Business question:** Monthly revenue by customer region for gold and silver
customers, where monthly revenue exceeds £500, ordered highest first.

```json
{
  "metrics":    ["orders.revenue"],
  "dimensions": ["customers.region", "orders.order_date"],
  "filters": [
    { "field": "customers.tier",  "op": "IN", "value": ["gold", "silver"] },
    { "field": "orders.status",   "op": "=",  "value": "complete"         }
  ],
  "having": [
    { "metric": "orders.revenue", "op": ">",  "value": 500 }
  ],
  "order_by": [
    { "field": "orders.revenue",  "direction": "DESC" }
  ],
  "limit": 100
}
```

---

## Validation rules enforced by `gavagai validate`

1. At least one metric or dimension must be selected.
2. Every `dataset` in a reference must exist in the semantic model.
3. Every metric name must exist in `semantic_model.metrics`.
4. Every dimension field name must exist in the referenced dataset's `fields` **and** carry a `dimension` annotation.
5. Every filter field must exist in the referenced dataset's `fields`.
6. Filter operators must be one of the recognised values above.
7. `IS NULL` / `IS NOT NULL` filters must carry no `value`.
8. All other filter operators require a `value`.
9. Having metric references follow the same rules as metric references.
10. Having operators must be one of `=`, `!=`, `>`, `>=`, `<`, `<=`.
11. `order_by` fields must reference a known metric or dimension.
12. `order_by` direction must be `ASC`, `DESC`, or absent.
