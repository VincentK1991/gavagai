# gavagai

> *"Gavagai"* — Quine's rabbit: the same utterance, indeterminate meaning. This tool pins down the meaning and renders it as SQL.

A Go CLI that compiles a **semantic model** (YAML or JSON) plus a **query** (JSON) into SQL for a target dialect, with predicate pushdown driven by the model.

---

## Overview

Data consumers describe *what* they want in a dialect-agnostic query. The semantic model knows *how* the data is physically laid out — tables, joins, measures, dimensions, and filter-safe columns. `gavagai` combines the two and emits the correct, optimized SQL.

```
semantic model (YAML/JSON)  +  query (JSON)  →  SQL (dialect)
```

Predicate pushdown is first-class: filters that can be safely pushed into subqueries or CTEs are identified from the semantic model and rewritten accordingly, avoiding full-table scans at query time.

---

## Concepts

### Semantic Model

A semantic model is a YAML or JSON document that declares:

- **Sources** — base tables or views, with their physical names and connection dialect.
- **Dimensions** — columns that can be grouped, filtered, or joined on.
- **Measures** — aggregated expressions (SUM, COUNT, …) computed over sources.
- **Joins** — relationships between sources, with join keys and cardinality hints.
- **Filters** — column-level annotations marking which predicates are safe to push down.

```yaml
# example: model.yaml
version: 1
sources:
  - name: orders
    table: raw.orders
    dialect: bigquery
    dimensions:
      - name: customer_id
        column: customer_id
        pushdown: true
      - name: status
        column: status
        pushdown: true
    measures:
      - name: revenue
        expression: SUM(amount)
  - name: customers
    table: raw.customers
    dialect: bigquery
    dimensions:
      - name: id
        column: id
joins:
  - left: orders
    right: customers
    on: orders.customer_id = customers.id
    type: left
```

### Query

A query is a JSON document describing the desired output in terms of model concepts — no SQL required.

```json
{
  "measures": ["orders.revenue"],
  "dimensions": ["customers.id"],
  "filters": [
    { "field": "orders.status", "op": "=", "value": "complete" }
  ],
  "limit": 100
}
```

### Predicate Pushdown

When a dimension is marked `pushdown: true`, `gavagai` rewrites the generated SQL to apply the filter as early as possible — inside a subquery or CTE rather than on the outer query — reducing data scanned before aggregation.

---

## Usage

```
gavagai [command] [flags]
```

### Commands

| Command | Description |
|---|---|
| `compile` | Compile a query against a model and print SQL |
| `validate` | Validate a model file without compiling |
| `version` | Print version information |

### `compile`

```bash
gavagai compile \
  --model   model.yaml \
  --query   query.json \
  --dialect postgres
```

| Flag | Short | Default | Description |
|---|---|---|---|
| `--model` | `-m` | — (required) | Path to semantic model file (YAML or JSON) |
| `--query` | `-q` | — (required) | Path to query file (JSON) |
| `--dialect` | `-d` | — (required) | Target SQL dialect (`postgres` or `bigquery`) |
| `--pretty` | | false | Emit multi-line SQL (default: compact single line) |
| `--explain` | | false | Print the query plan to **stderr** before the SQL |

SQL is written to **stdout**; the `--explain` plan and all errors go to **stderr**,
so `gavagai compile ... > out.sql` always yields clean SQL.

**Output** (`--pretty`, stdout):

```sql
SELECT
  region AS "region",
  tier AS "tier",
  SUM(orders.amount) AS "revenue",
  COUNT(DISTINCT orders.order_id) AS "order_count"
FROM analytics.orders AS "orders"
LEFT JOIN analytics.customers AS "customers"
  ON "orders"."customer_id" = "customers"."customer_id"
WHERE status = 'complete'
  AND tier IN ('gold', 'silver')
GROUP BY region, tier
HAVING SUM(orders.amount) >= 500
ORDER BY "revenue" DESC
LIMIT 20
```

The same query for `--dialect bigquery` differs only in quoting
(`` `region` ``) and table path (`` `my_project.analytics.orders` ``).

### `validate`

```bash
gavagai validate --model model.yaml
```

Exits `0` on a valid model, `1` with a descriptive error on failure.

---

## Project Structure (planned)

```
gavagai/
├── cmd/                  # Cobra commands (compile, validate, version)
│   ├── root.go
│   ├── compile.go
│   └── validate.go
├── internal/
│   ├── model/            # Semantic model parsing and validation
│   ├── query/            # Query parsing
│   ├── planner/          # Predicate pushdown and query planning
│   └── codegen/          # SQL generation per dialect
│       ├── bigquery.go
│       ├── snowflake.go
│       ├── postgres.go
│       └── duckdb.go
├── testdata/             # Fixture models and queries for tests
├── main.go
├── go.mod
└── .golangci.yml
```

---

## Toolchain

| Concern | Tool | Notes |
|---|---|---|
| CLI framework | [Cobra](https://github.com/spf13/cobra) | Industry standard; used in `kubectl`, `gh`, `hugo`; subcommands, flags, and help generation out of the box |
| Testing | Go `testing` + [testify](https://github.com/stretchr/testify) | Table-driven tests are idiomatic Go; testify adds readable assertions and mocking for SQL output comparisons |
| Linting / static analysis | [golangci-lint](https://golangci-lint.run) | Meta-linter running `go vet`, `staticcheck` (150+ checks), and 50+ others in parallel with caching |

---

## Getting Started

### Prerequisites

- Go 1.22+
- `golangci-lint` — install via the [official script](https://golangci-lint.run/usage/install/)

```bash
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
```

### Build

```bash
git clone https://github.com/vincentk1991/gavagai.git
cd gavagai
go build -o gavagai ./...
```

### Run

```bash
./gavagai compile --model testdata/model.yaml --query testdata/query.json
```

### Test

```bash
go test ./...
```

### Lint

```bash
golangci-lint run ./...
```

---

## Dialects

Planned support:

- [x] BigQuery
- [x] Snowflake
- [x] PostgreSQL
- [x] DuckDB

---

## License

MIT
