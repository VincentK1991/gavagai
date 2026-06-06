# ADR-0001: Semantic-model-driven query compiler (Go CLI) as the business-question → SQL translation layer

- **Status:** Proposed
- **Date:** 2026-06-06
- **Deciders:** (TBD)
- **Working name:** `osiqr` (semantic query compiler)
- **Supersedes / Related:** —

> Assumptions made in this draft are tagged **[ASSUMPTION]** and collected in *Open Questions*. Confirm or correct them before moving status to *Accepted*.

---

## Context

We need to answer business questions ("monthly revenue by region for EMEA and APAC, where revenue > 1000") against the warehouse. Two existing approaches both fail us in different ways:

1. **Hand-written SQL per question.** Correct when written by an expert, but it does not scale, definitions drift across dashboards and reports ("revenue" means three different things), and every new question is net-new engineering work.
1. **LLM text-to-SQL (NL → SQL directly).** Flexible but unreliable. The model hallucinates joins, silently picks the wrong grain, double-counts on fan-out, and produces SQL that is hard to verify and unsafe to run unreviewed. Output is nondeterministic: the same question can compile to different SQL on different days. It also exposes an unbounded SQL surface to a probabilistic component.

We want a **deterministic translation layer** that sits between a *high-level business question* and *low-level dialect SQL*, and that moves the locus of correctness from model output to **human-owned, version-controlled definitions**.

The key reframing: an upstream agent (or BI tool) should produce a **structured query intermediate representation (IR)** — a constrained object naming metrics, dimensions, filters, ordering, limits — *not* SQL. The compiler turns `(semantic model, query IR)` into correct SQL for a target dialect. Metric/dimension/join semantics live in the semantic model, which is **owned by analytics engineers and business stakeholders**, reviewed, and versioned like code.

This narrows the agent's job from "write correct SQL" (open-ended, flaky) to "select from a known, validatable vocabulary" (bounded, checkable). The compiler is the trusted last mile.

## Decision

Build a **Go CLI** that accepts a semantic-model file and a query IR (JSON) and emits SQL for a target dialect, initially **BigQuery and PostgreSQL**.

Architecturally:

- **Neutral relational-algebra plan.** The CLI resolves the IR against the semantic model into a dialect-independent plan (`Scan / Join / Aggregate / Having / Order / Limit`) built from neutral expression nodes. All resolution, join planning, and optimization happen here, with zero dialect awareness.
- **Predicate pushdown and join resolution at the plan layer.** Filters are pushed to the lowest scope that exposes their columns; the join tree is derived from declared relationships. This is the correctness core and is dialect-independent.
- **Per-dialect emitter (`Dialect` strategy), not a SQL transpiler.** The only dialect-specific code walks the finished plan and prints syntax (identifier quoting, table-path quoting, function shape such as `DATE_TRUNC`, literal rendering, row-limiting). Adding a dialect is adding one emitter.
- **No polyglot/transpilation dependency.** We *generate* SQL from a neutral plan we construct; we never *parse* foreign SQL. Transpilation is a read-side capability we do not need. Any raw SQL embedded in a semantic-model source binding is treated as opaque pass-through and is the author's responsibility for the target engine.
- **The semantic model is the contract.** It is the single source of truth for what metrics/dimensions/relationships mean, owned and reviewed by humans, decoupled from the agent.
- **The query IR is the stable interface** between the agent (or any caller) and the compiler. It is validatable: every referenced metric/dimension must exist in the model, or compilation fails loudly.

## Decision drivers

- **Reliability over cleverness.** Deterministic, pure-function compilation: same `(model, IR)` → same SQL, every time. Correctness is reviewed once in the model, not re-derived per query by a stochastic component.
- **Flexibility without code change.** New questions are new IR combinations over existing definitions — no engineering. New warehouses are new emitters.
- **Governance / ownership.** Business and analytics own definitions; the metric layer is auditable and versioned.
- **Safety / bounded surface.** The agent cannot emit arbitrary SQL. Its output is a constrained IR that is validated against the model before any SQL exists.
- **Operational fit.** [ASSUMPTION] Go is chosen for a single static binary, trivial deployment as a CLI/sidecar in our service mesh, fast cold start, and concurrency fit — over an equivalent Python implementation.

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
- **Possible divergence from OSI / MetricFlow** as those standards evolve; we may re-converge later (see Alternatives).

### Neutral

- An upstream NL→IR component is still required; we have moved the probabilistic step, not eliminated it. Its job is now far easier to constrain and validate.

## Alternatives considered

1. **LLM text-to-SQL (direct NL → SQL).** *Rejected.* Nondeterministic, unverifiable, unsafe surface, silent correctness failures. This ADR exists to avoid it.
1. **SQLGlot / transpiler-based rewriter.** *Rejected for the core.* A transpiler's value is parsing arbitrary input dialects (read side). We only ever generate from a neutral plan (write side), so a transpiler adds a dependency without buying a needed capability. We may use a transpiler *narrowly* later, only to translate raw scalar expressions embedded in semantic-model source bindings between dialects, if we choose to allow non-canonical exprs.
1. **dbt MetricFlow "right out."** *Deferred, not dismissed.* MetricFlow is the OSI initiative's reference planner and already solves fan-out-safe joins, multi-hop resolution, complex metric types, and seven dialect renderers. But it requires a dbt project and a dbt adapter, its CLI must run from a project root, its model input is the compiled `semantic_manifest.json` (not a loose file), its query input is CLI flags (not JSON), and dialect = installed adapter. Using it as a pure "file + JSON → SQL" library means driving `MetricFlowEngine.explain()` against an in-memory manifest and coding against unstable internals. We keep MetricFlow as (a) a reference design for our join resolver and (b) a candidate engine to adopt later if our metric-type needs outgrow our own planner. [ASSUMPTION] Our near-term metric needs are simple aggregates + grain, not ratio/cumulative/period-over-period.
1. **JVM stack (Apache Calcite) or Substrait IR.** *Rejected.* Calcite is the gold standard for multi-dialect planning + optimization but is wrong-ecosystem (JVM) for a Go service and far heavier than needed. Substrait fits when consumers *execute plans*; our consumers want *SQL text*, where Substrait→SQL emission is immature.
1. **Python implementation.** *Viable; not chosen.* Same architecture works in Python (and a prototype exists). Go preferred for [ASSUMPTION] deployment/ops reasons above. Revisit if the team's primary stack or the chosen engine (e.g. MetricFlow, which is Python) pulls us back.

## Scope (v1)

- **In:** single fact dataset per query; metrics as simple aggregates (`sum/count/avg/min/max`); dimensions with optional time grain; equality/range/IN/null filters; HAVING on metrics; order/limit; BigQuery + Postgres emitters; predicate pushdown; fan-out **detection** with explicit refusal.
- **Out (v1):** multi-fact symmetric aggregates; ratio/cumulative/derived/period-over-period metrics; automatic pre-aggregation for fan-out; dialects beyond BQ/PG; raw-expr cross-dialect translation.

## Open questions (please confirm)

1. **Semantic model schema:** is the input file an **OSI document**, or our own format? (dbt Core v1.12+ can ingest OSI natively — this materially affects future MetricFlow interop and whether we write a converter.)
1. **Who authors the query IR** — an LLM agent, a BI/UI layer, both? Determines how strict IR validation and the IR contract must be.
1. **Multi-dialect simultaneously**, or one target dialect per deployment? (Affects whether single-target tools like MetricFlow could ever substitute.)
1. **CLI only, or also a library/long-running service?** Affects the interface and the Go-vs-Python weighting.
1. **Correctness bar for v1 fan-out:** is *detect-and-refuse* acceptable, or do we need correct pre-aggregation from day one?
