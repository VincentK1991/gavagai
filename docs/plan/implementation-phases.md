# gavagai — Phased Implementation Plan

**Related:** [ADR-0001](../adr/ADR-0001-semantic-model-driven-query-compiler.md)

## Phase gate (applies to every phase)

A phase is complete when all three of these pass with zero errors or warnings:

```bash
make lint    # golangci-lint run ./...
make test    # go test ./...
make build   # go build ./...
```

No phase may close with a lint suppression (`//nolint`) that was not present at the start of the phase, a skipped test, or a test with an empty body.

---

## Phase 0 — Repo bootstrap

**Goal:** A compilable, testable, lint-clean skeleton. No business logic yet.

### Deliverables

| File / dir | Purpose |
|---|---|
| `go.mod` | Module `github.com/vincentk1991/gavagai`, Go 1.22+ |
| `main.go` | Entry point — calls `cmd.Execute()` |
| `cmd/root.go` | Cobra root command, `--version` flag |
| `cmd/compile.go` | `compile` subcommand stub — returns `ErrNotImplemented` |
| `cmd/validate.go` | `validate` subcommand stub — returns `ErrNotImplemented` |
| `cmd/version.go` | `version` subcommand — prints module version |
| `.golangci.yml` | Linter config: `gofmt`, `govet`, `staticcheck`, `errcheck`, `gosimple`, `unused` enabled |
| `Makefile` | Targets: `build`, `test`, `lint`, `clean` |
| `.github/workflows/ci.yml` | GitHub Actions: run `make lint test` on every push and PR |

### Dependencies

```
go get github.com/spf13/cobra@latest
```

### Tests required

- `cmd/root_test.go` — execute root command with `--help`; assert exit code 0 and non-empty output.
- `cmd/compile_test.go` — execute `compile` with no args; assert error is returned.
- `cmd/validate_test.go` — execute `validate` with no args; assert error is returned.

### Gate

`make lint test build` passes. CI workflow is green on push.

---

## Phase 1 — OSI semantic model parsing

**Goal:** Parse and structurally validate an OSI semantic model document (YAML or JSON) into Go types.

### Deliverables

| File / dir | Purpose |
|---|---|
| `internal/model/types.go` | Go structs mirroring OSI schema: `SemanticModel`, `Dataset`, `Field`, `Metric`, `Relationship`, `Expression`, `Dimension`, `CustomExtension`, `AIContext` |
| `internal/model/parse.go` | `ParseFile(path string) (*SemanticModel, error)` — detects YAML vs JSON by extension, unmarshals, validates required fields |
| `internal/model/validate.go` | `Validate(m *SemanticModel) []ValidationError` — checks required fields, unique names, expression presence |
| `testdata/models/simple.yaml` | Minimal valid model: one dataset, one dimension, one metric, no joins |
| `testdata/models/joined.yaml` | Two datasets with a relationship |
| `testdata/models/invalid_missing_name.yaml` | Model with missing required `name` field |

### OSI types to implement

```
SemanticModel
  name           string   (required)
  description    string
  ai_context     AIContext
  datasets       []Dataset  (required, ≥1)
  relationships  []Relationship
  metrics        []Metric
  custom_extensions []CustomExtension

Dataset
  name           string   (required)
  source         string   (required — physical table/view ref)
  primary_key    []string
  description    string
  fields         []Field

Field
  name           string   (required)
  expression     Expression (required)
  dimension      *Dimension
  label          string
  description    string

Metric
  name           string   (required)
  expression     Expression (required)
  description    string

Relationship
  name           string   (required)
  from           string   (required — dataset name)
  to             string   (required — dataset name)
  from_columns   []string (required)
  to_columns     []string (required)

Expression
  dialects []DialectExpression

DialectExpression
  dialect    string  (ANSI_SQL | SNOWFLAKE | BIGQUERY | POSTGRES | …)
  expression string
```

### Tests required

Table-driven tests in `internal/model/parse_test.go`:

- Parse `simple.yaml` → assert field counts and names.
- Parse `joined.yaml` → assert relationship is loaded.
- Parse `invalid_missing_name.yaml` → assert `ValidationError` returned.
- Parse a JSON equivalent of `simple.yaml` → same assertions.
- Parse non-existent path → assert error.
- `Validate` with duplicate dataset name → assert error.
- `Validate` with field missing expression → assert error.

### Gate

`make lint test build` passes. All table-driven cases pass.

---

## Phase 2 — Query IR definition and parsing

**Goal:** Define the query IR schema, parse it from JSON, and validate every referenced name against a loaded semantic model.

### Deliverables

| File / dir | Purpose |
|---|---|
| `internal/query/types.go` | `Query`, `Filter`, `OrderItem` Go structs |
| `internal/query/parse.go` | `ParseFile(path string) (*Query, error)` |
| `internal/query/validate.go` | `Validate(q *Query, m *model.SemanticModel) []ValidationError` — every metric/dimension/filter field must exist in the model |
| `docs/query-ir-schema.md` | Human-readable IR reference with annotated example |
| `testdata/queries/simple.json` | Selects one metric + one dimension, no filter |
| `testdata/queries/with_filter.json` | Adds equality filter on a dimension |
| `testdata/queries/unknown_metric.json` | References a metric not in `simple.yaml` |

### Query IR schema

```json
{
  "metrics":    ["<dataset>.<metric_name>"],
  "dimensions": ["<dataset>.<field_name>"],
  "filters": [
    { "field": "<dataset>.<field_name>", "op": "=|!=|>|>=|<|<=|IN|IS NULL|IS NOT NULL", "value": "<scalar_or_array>" }
  ],
  "having": [
    { "metric": "<dataset>.<metric_name>", "op": ">|>=|<|<=|=|!=", "value": "<number>" }
  ],
  "order_by": [
    { "field": "<dataset>.<field_or_metric_name>", "direction": "ASC|DESC" }
  ],
  "limit": 100
}
```

Metric and dimension references use dot-qualified names: `orders.revenue`, `customers.region`.

### Tests required

Table-driven tests in `internal/query/validate_test.go`:

- Valid query against `simple.yaml` model → no errors.
- Query with unknown metric → `ValidationError` naming the metric.
- Query with unknown dimension → `ValidationError` naming the dimension.
- Filter referencing unknown field → `ValidationError`.
- Query with no metrics and no dimensions → `ValidationError` ("query must select at least one metric or dimension").
- Malformed JSON → parse error.

### Gate

`make lint test build` passes.

---

## Phase 3 — Neutral relational-algebra plan + join resolution + fan-out detection

**Goal:** Resolve a validated `(model, query)` pair into a dialect-independent plan tree. Detect fan-out and refuse loudly.

### Deliverables

| File / dir | Purpose |
|---|---|
| `internal/planner/nodes.go` | Plan node types: `ScanNode`, `JoinNode`, `FilterNode`, `AggregateNode`, `HavingNode`, `OrderNode`, `LimitNode`, `ExprNode` |
| `internal/planner/planner.go` | `Plan(q *query.Query, m *model.SemanticModel) (*PlanNode, error)` |
| `internal/planner/join_resolver.go` | Derives the join tree from OSI `relationships` for the datasets referenced by the query |
| `internal/planner/fanout.go` | `DetectFanOut(joinTree, metrics) error` — detects one-to-many joins where an additive measure would double-count; returns a descriptive error |

### Plan node shape

```
PlanNode (interface)
  ├── ScanNode      { dataset *Dataset, alias string }
  ├── JoinNode      { left, right PlanNode, on []JoinCondition, kind JoinKind }
  ├── FilterNode    { input PlanNode, predicate Expr }   ← used for pushdown
  ├── AggregateNode { input PlanNode, groupBy []Expr, aggregates []AggExpr }
  ├── HavingNode    { input PlanNode, predicate Expr }
  ├── OrderNode     { input PlanNode, items []OrderItem }
  └── LimitNode     { input PlanNode, count int }
```

### Fan-out detection rule (v1)

> **Correction (implemented semantics):** an earlier draft of this rule said the
> *many* side fans out. That is backwards — joining many `orders` to one
> `customer` does **not** duplicate `orders` rows. The implemented (and correct)
> rule is below.

A metric sourced at dataset **D** fans out when **D is on the *one* side** of a
join edge in use (its rows are multiplied by the *many* side) **and** the
metric's aggregate is not robust to row duplication. OSI relationships are
`from` (many, foreign key) → `to` (one, primary key), so D fans out when some
join edge has `to == D`.

Aggregate safety:

- **Unsafe** (double-count under duplication): `SUM`, `AVG`, plain `COUNT(...)`.
- **Safe** (idempotent under duplication): `COUNT(DISTINCT ...)`, `MIN`, `MAX`.

The compiler returns a `*planner.FanOutError` whose message contains the string
`fan-out detected`, names the offending metric and the relationship causing the
duplication, and suggests using a fan-out-safe metric or removing the reference.

No implicit pre-aggregation is performed.

### Tests required

Table-driven tests in `internal/planner/planner_test.go`:

- Simple query (no joins) → plan is `Limit(Order(Having(Aggregate(Scan))))`.
- Query requiring a join → plan contains a `JoinNode` with correct on-condition.
- Query against two datasets with no declared relationship → error.
- Fan-out scenario (additive metric, one-to-many join) → error containing "fan-out detected".
- Non-additive join (many-to-one safe direction) → no error.

Table-driven tests in `internal/planner/fanout_test.go`:

- At least five distinct fan-out / no-fan-out scenarios covering different join cardinalities.

### Gate

`make lint test build` passes. The fan-out test cases are all covered and named.

---

## Phase 4 — Predicate pushdown

**Goal:** Rewrite `FilterNode`s in the plan tree to sit at the lowest scope whose input exposes the filtered columns — inside a subquery or CTE rather than on the outer query.

### Deliverables

| File / dir | Purpose |
|---|---|
| `internal/planner/pushdown.go` | `PushDown(root PlanNode) PlanNode` — pure tree rewrite, returns a new root |

### Pushdown rule

A `FilterNode` whose predicate references only columns available in a `ScanNode` (i.e., no join is needed to resolve it) is pushed to wrap that `ScanNode` directly. Filters that span multiple datasets remain at the join output scope.

### Tests required

Table-driven tests in `internal/planner/pushdown_test.go`:

- Single-dataset filter → `FilterNode` is a direct child of `ScanNode` in output tree.
- Cross-dataset filter (join-key equality) → `FilterNode` remains above `JoinNode`.
- Multiple filters, mixed pushability → each filter lands at the correct scope.
- Pure pushdown is idempotent: applying `PushDown` twice yields the same tree.

### Gate

`make lint test build` passes.

---

## Phase 5 — PostgreSQL emitter

**Goal:** Walk the finished plan tree and emit syntactically correct PostgreSQL SQL.

### Deliverables

| File / dir | Purpose |
|---|---|
| `internal/codegen/dialect.go` | `Dialect` interface: `EmitSQL(root PlanNode) (string, error)` |
| `internal/codegen/postgres/emitter.go` | `PostgresDialect` implementing `Dialect` |
| `internal/codegen/postgres/expr.go` | Expression rendering: identifier quoting (`"name"`), `DATE_TRUNC`, `CAST`, literal escaping |
| `testdata/golden/postgres/` | Golden `.sql` files, one per test fixture |

### Dialect expression selection

When rendering an `ExprNode` that originated from an OSI field or metric, the emitter selects the dialect expression using:

1. Exact match on `POSTGRES`.
2. Fallback to `ANSI_SQL`.
3. Error if neither is present: `no expression for dialect postgres in field "<name>"`.

### Tests required

Golden-file tests in `internal/codegen/postgres/emitter_test.go`:

Each test loads a `(model, query)` fixture, runs the full pipeline (`parse → validate → plan → pushdown → emit`), and compares output to a golden `.sql` file. Update goldens with `go test -update`.

Fixtures to cover:
- Simple select: one metric, one dimension, no join, no filter.
- Filter pushed down: filter on a single-dataset dimension.
- Join query: two datasets, one metric each side, cross-dataset filter above join.
- HAVING clause: metric filter.
- ORDER + LIMIT.
- Missing dialect expression → error (no POSTGRES or ANSI_SQL expression on a field).

### Gate

`make lint test build` passes. All golden files committed to the repo.

---

## Phase 6 — BigQuery emitter

**Goal:** BigQuery SQL emitter using the same plan tree as Phase 5.

### Deliverables

| File / dir | Purpose |
|---|---|
| `internal/codegen/bigquery/emitter.go` | `BigQueryDialect` + `renderer` implementing `codegen.Renderer` |

> **Implementation note (deviation from the original sketch):** the SQL clause
> structure proved identical across PostgreSQL and BigQuery, so the builder was
> extracted into `internal/codegen/sqlbuilder.go` (`EmitSelect` + the `Renderer`
> interface) rather than duplicated per dialect. Each dialect package now
> supplies only its `Renderer` (quoting + dialect tag); there is no per-dialect
> `expr.go`. Argument-order differences such as `DATE_TRUNC(col, MONTH)` are
> carried in the semantic model's per-dialect expression entries, not in code.
| `testdata/golden/bigquery/` | Golden `.sql` files matching the same fixtures as Phase 5 |

### BigQuery differences from Postgres to cover

| Feature | Postgres | BigQuery |
|---|---|---|
| Identifier quoting | `"name"` | `` `name` `` |
| `DATE_TRUNC` | `DATE_TRUNC('month', col)` | `DATE_TRUNC(col, MONTH)` |
| Table path | `schema.table` | `project.dataset.table` |
| `LIMIT` | `LIMIT n` | `LIMIT n` (same) |
| String literals | `'value'` | `'value'` (same) |

### Tests required

Same fixture matrix as Phase 5, golden files in `testdata/golden/bigquery/`. All Postgres fixtures must have a BigQuery counterpart.

Additional test: compile same `(model, query)` for both dialects in one test and assert the outputs differ where syntax differs (identifier quoting at minimum).

### Gate

`make lint test build` passes. All golden files committed.

---

## Phase 7 — CLI integration

**Goal:** Wire the Cobra `compile` and `validate` commands to the full pipeline. End-to-end tests using real fixture files on disk.

### Deliverables

| File / dir | Purpose |
|---|---|
| `cmd/compile.go` | Implements `--model`, `--query`, `--dialect`, `--pretty`, `--explain` flags; runs full pipeline; writes SQL to stdout |
| `cmd/validate.go` | Implements `--model` flag; runs model parse + validate; exits 0 or 1 |
| `internal/pipeline/pipeline.go` | Orchestrates parse→validate→plan→pushdown→emit; registers both dialects; keeps `cmd` thin and unit-testable |
| `internal/pretty/pretty.go` | `Compact` collapses emitted SQL to one line (CLI default); `--pretty` keeps the multi-line form |

> **Implementation notes (deviations):**
> - A dedicated `internal/pipeline` package was added so the orchestration is
>   testable without a Cobra tree and owns the dialect blank-imports.
> - `--pretty` controls compact-vs-multiline rather than adding indentation:
>   the emitters already produce canonical multi-line SQL, so the CLI default
>   compacts it to one line (machine-friendly) and `--pretty` keeps it expanded.
> - No `testdata/e2e/` dir: the existing model/query fixtures and emitter golden
>   files are the single source of truth, exercised through the CLI by the
>   `cmd` integration tests (reusing them avoids fixture drift).

### CLI interface (final)

```
gavagai compile \
  --model   <path>   (required)
  --query   <path>   (required)
  --dialect <name>   (required: bigquery | postgres)
  --pretty           (optional: pretty-print output SQL)
  --explain          (optional: print plan summary before SQL, to stderr)

gavagai validate --model <path>

gavagai version
```

### Tests required

`cmd/compile_test.go` — integration tests using `exec.Command` or by calling `cmd.Execute()` directly:

- Compile `simple.yaml` + `simple.json` for postgres → output matches golden.
- Compile same for bigquery → output matches bigquery golden.
- `--model` missing → error message mentions `--model`, exit code 1.
- `--dialect` invalid value → error message, exit code 1.
- Fan-out query → error message contains "fan-out detected", exit code 1.
- `--explain` flag → plan summary appears on stderr, SQL on stdout.

`cmd/validate_test.go`:

- Valid model → exit 0, no output.
- Invalid model (missing required field) → exit 1, error describes which field.

### Gate

`make lint test build` passes. All E2E tests pass. `./gavagai --help` renders correctly.

---

## Summary table

| Phase | Milestone | Key new packages | Gate |
|---|---|---|---|
| 0 | Repo bootstrap | `cmd/` skeleton, Makefile, CI | `make lint test build` green |
| 1 | OSI model parsing | `internal/model` | Table-driven parse + validate tests |
| 2 | Query IR | `internal/query` | Validate-against-model tests |
| 3 | Plan + join resolution + fan-out | `internal/planner` | Fan-out detection is a mandatory test case |
| 4 | Predicate pushdown | `internal/planner/pushdown` | Pushdown scope tests |
| 5 | PostgreSQL emitter | `internal/codegen/postgres` | Golden-file tests committed |
| 6 | BigQuery emitter | `internal/codegen/bigquery` | Golden-file tests committed |
| 7 | CLI integration | `cmd/` wired | E2E tests; `--help` renders |

Each phase builds on the previous. No phase may begin until the prior phase's gate is green.
