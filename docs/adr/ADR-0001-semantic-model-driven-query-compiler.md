# ADR-0001: Semantic-model-driven query compiler (Go CLI) as the business-question → SQL translation layer

- **Status:** Accepted
- **Date:** 2026-06-06
- **Decisions recorded:** 2026-06-06
- **Deciders:** (TBD)
- **Working name:** `osiqr` (semantic query compiler)
- **Supersedes / Related:** —

---

## Context

We need to answer business questions ("monthly revenue by region for EMEA and APAC, where revenue > 1000") against the warehouse. Two existing approaches both fail us in different ways:

1. **Hand-written SQL per question.** Correct when written by an expert, but it does not scale, definitions drift across dashboards and reports ("revenue" means three different things), and every new question is net-new engineering work.
1. **LLM text-to-SQL (NL → SQL directly).** Flexible but unreliable. The model hallucinates joins, silently picks the wrong grain, double-counts on fan-out, and produces SQL that is hard to verify and unsafe to run unreviewed. Output is nondeterministic: the same question can compile to different SQL on different days. It also exposes an unbounded SQL surface to a probabilistic component.

We want a **deterministic translation layer** that sits between a *high-level business question* and *low-level dialect SQL*, and that moves the locus of correctness from model output to **human-owned, version-controlled definitions**.

The key reframing: an upstream agent (or BI tool) should produce a **structured query intermediate representation (IR)** — a constrained object naming metrics, dimensions, filters, ordering, limits — *not* SQL. The compiler turns `(semantic model, query IR)` into correct SQL for a target dialect. Metric/dimension/join semantics live in the semantic model, which is **owned by analytics engineers and business stakeholders**, reviewed, and versioned like code.

This narrows the agent's job from "write correct SQL" (open-ended, flaky) to "select from a known, validatable vocabulary" (bounded, checkable). The compiler is the trusted last mile.

## Decision

Build a **Go CLI** that accepts an **OSI-format semantic model** and a query IR (JSON) and emits SQL for a target dialect. Initial target dialects are **BigQuery and PostgreSQL**, both supported simultaneously from v1 (the `--dialect` flag selects per invocation).

Architecturally:

- **OSI as the semantic model format.** The input semantic model is a valid [Open Semantic Interchange (OSI)](https://github.com/open-semantic-interchange/OSI) document (`SemanticModel` → `datasets` → `fields` / `metrics` / `relationships`). Adopting OSI ensures vendor neutrality, interchangeability with dbt Core ≥ v1.12 (which ingests OSI natively), and alignment with the MetricFlow reference planner. Field and metric expressions in OSI carry per-dialect SQL already; see *Dialect mapping* note below.
- **Neutral relational-algebra plan.** The CLI resolves the query IR against the semantic model into a dialect-independent plan (`Scan / Join / Aggregate / Having / Order / Limit`) built from neutral expression nodes. All resolution, join planning, and optimization happen here, with zero dialect awareness.
- **Predicate pushdown and join resolution at the plan layer.** Filters are pushed to the lowest scope that exposes their columns; the join tree is derived from declared OSI `relationships`. This is the correctness core and is dialect-independent.
- **Per-dialect emitter (`Dialect` strategy), not a SQL transpiler.** The only dialect-specific code walks the finished plan and prints syntax (identifier quoting, table-path quoting, function shape such as `DATE_TRUNC`, literal rendering, row-limiting). Adding a dialect is adding one emitter.
- **No polyglot/transpilation dependency.** We *generate* SQL from a neutral plan we construct; we never *parse* foreign SQL. Any raw SQL embedded in an OSI source binding is opaque pass-through and is the author's responsibility for the target engine.
- **The semantic model is the contract.** It is the single source of truth for what metrics/dimensions/relationships mean, owned and reviewed by humans, decoupled from the agent.
- **The query IR is the stable interface** between the caller and the compiler. Every referenced metric/dimension must exist in the model, or compilation fails loudly. The IR authoring tool (LLM agent, BI layer, or human) is out of scope for v1.
- **Fan-out detection is a hard correctness gate.** Multi-join fan-out (double-counting additive measures across one-to-many joins) must be detected and the compiler must refuse with a clear error. This gate is enforced by unit tests; no query that would produce incorrect aggregation may silently succeed.

### Dialect mapping note

OSI's built-in dialect enum (`ANSI_SQL`, `SNOWFLAKE`, `MDX`, `TABLEAU`, `DATABRICKS`, `MAQL`) does not include `BIGQUERY` or `POSTGRES`. Within OSI field/metric expressions, authors should use `ANSI_SQL` for expressions portable across these engines, or use OSI `custom_extensions` with `vendor_name: COMMON` to carry `BIGQUERY` / `POSTGRES` dialect variants. The `gavagai` compiler accepts `--dialect bigquery` or `--dialect postgres` as its target, and when selecting a dialect expression from an OSI field it prefers: exact match → `ANSI_SQL` fallback → compilation error if neither is present.

## Decision drivers

- **Reliability over cleverness.** Deterministic, pure-function compilation: same `(model, IR)` → same SQL, every time. Correctness is reviewed once in the model, not re-derived per query by a stochastic component.
- **Flexibility without code change.** New questions are new IR combinations over existing definitions — no engineering. New warehouses are new emitters.
- **Governance / ownership.** Business and analytics own definitions; the metric layer is auditable and versioned.
- **Safety / bounded surface.** The agent cannot emit arbitrary SQL. Its output is a constrained IR that is validated against the model before any SQL exists.
- **Operational fit.** Go is chosen for a single static binary, trivial deployment as a CLI or sidecar, fast cold start, and concurrency fit — over an equivalent Python implementation. The interface is CLI-only; no long-running service is required at this stage.

## Consequences

### Positive

- Deterministic, unit-testable compilation; golden-file tests per dialect.
- The flaky NL→SQL step is replaced by a checkable NL→IR step; failures are explicit (unknown metric) rather than silent (wrong join).
- Definitions are reusable across questions, tools, and agents, with consistent numbers everywhere.
- Clean separation of concerns: agent (intent), model (semantics, human-owned), compiler (deterministic SQL).
- Dialect coverage grows additively and in isolation.

### Negative / costs we are accepting

- **We own the hard correctness.** Fan-out / chasm-trap avoidance, multi-hop join resolution, and time-grain aggregation are *our* burden. A naive star-join planner will double-count additive measures across one-to-many joins. v1 must at minimum detect and refuse (or correctly pre-aggregate) fan-out cases. This is exactly the machinery MetricFlow already solves — see Alternatives.
- **We own every dialect emitter.** Each new warehouse is real work, and subtle dialect semantics (NULL ordering, casting, date functions) are a long tail.
- **The IR is a new interface to design and version.** It must be expressive enough to be useful and constrained enough to be safe; changes ripple to every caller.
- **Coverage is bounded by the model — by design.** Questions that need a metric/dimension not yet defined simply fail. This is the intended reliability trade (no silent improvisation), but it shifts work to model authoring and means the model must keep pace with business needs.
- **OSI dialect enum does not cover BigQuery/Postgres natively.** Field expressions in OSI documents must use `ANSI_SQL` or custom extensions to carry BigQuery/Postgres variants. Model authors must be aware of this convention; the compiler enforces it at parse time.

### Neutral

- An upstream NL→IR component is still required; we have moved the probabilistic step, not eliminated it. Its job is now far easier to constrain and validate.

## Alternatives considered

1. **LLM text-to-SQL (direct NL → SQL).** *Rejected.* Nondeterministic, unverifiable, unsafe surface, silent correctness failures. This ADR exists to avoid it.
1. **SQLGlot / transpiler-based rewriter.** *Rejected for the core.* A transpiler's value is parsing arbitrary input dialects (read side). We only ever generate from a neutral plan (write side), so a transpiler adds a dependency without buying a needed capability. We may use a transpiler *narrowly* later, only to translate raw scalar expressions embedded in semantic-model source bindings between dialects, if we choose to allow non-canonical exprs.
1. **dbt MetricFlow "right out."** *Deferred, not dismissed.* MetricFlow is the OSI initiative's reference planner and already solves fan-out-safe joins, multi-hop resolution, complex metric types, and seven dialect renderers. But it requires a dbt project and a dbt adapter, its CLI must run from a project root, its model input is the compiled `semantic_manifest.json` (not a loose file), its query input is CLI flags (not JSON), and dialect = installed adapter. Using it as a pure "file + JSON → SQL" library means driving `MetricFlowEngine.explain()` against an in-memory manifest and coding against unstable internals. We keep MetricFlow as (a) a reference design for our join resolver and fan-out logic and (b) a candidate engine to adopt later if our metric-type needs outgrow our own planner. Near-term needs are confirmed as simple aggregates + grain; ratio/cumulative/period-over-period are out of v1 scope.
1. **JVM stack (Apache Calcite) or Substrait IR.** *Rejected.* Calcite is the gold standard for multi-dialect planning + optimization but is wrong-ecosystem (JVM) for a Go service and far heavier than needed. Substrait fits when consumers *execute plans*; our consumers want *SQL text*, where Substrait→SQL emission is immature.
1. **Python implementation.** *Viable; not chosen.* Same architecture works in Python. Go preferred for deployment/ops reasons (single binary, CLI-only interface) confirmed above. Revisit if the chosen engine (e.g. MetricFlow, which is Python) pulls us back.

## Scope (v1)

- **In:** OSI-format semantic model; single fact dataset per query; metrics as simple aggregates (`sum/count/avg/min/max`); dimensions with optional time grain; equality/range/IN/null filters; HAVING on metrics; order/limit; BigQuery + Postgres emitters (selectable per invocation); predicate pushdown; fan-out detection with explicit refusal enforced as a unit-test correctness gate.
- **Out (v1):** multi-fact symmetric aggregates; ratio/cumulative/derived/period-over-period metrics; automatic pre-aggregation for fan-out; dialects beyond BQ/PG; raw-expr cross-dialect translation; NL→IR authoring tooling.

## Resolved decisions

| # | Question | Answer |
|---|---|---|
| 1 | Semantic model schema | **OSI format** ([open-semantic-interchange/OSI](https://github.com/open-semantic-interchange/OSI)). Ensures vendor neutrality and native dbt Core ≥ v1.12 interop. BigQuery/Postgres dialect expressions use `ANSI_SQL` or `custom_extensions` within OSI documents. |
| 2 | Who authors the query IR | **Human with AI assistance.** Authoring toolchain is deferred; the IR contract must be clean and well-documented but the exact tool is out of v1 scope. |
| 3 | Multi-dialect simultaneously | **Yes — multi-dialect from v1.** `--dialect bigquery` and `--dialect postgres` are both supported; dialect is selected per CLI invocation, not per deployment. |
| 4 | CLI only or service | **CLI only** for now. Single static binary. No long-running server interface in scope. |
| 5 | Correctness bar for fan-out | **Detect-and-refuse is the v1 bar, enforced as a unit-test gate.** Any query that would produce incorrect aggregation due to fan-out must fail at compile time with a clear error. Correct pre-aggregation is out of v1 scope. |
