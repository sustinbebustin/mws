# Clean Code Architecture: An Informational Guide

A source-grounded reference for the principles, patterns, and critiques that sit under the umbrella phrase "clean code architecture." Written for internal reference.

Scope note: this document covers two distinct but frequently-conflated bodies of work.

- **Clean Code** (Robert C. Martin, 2008) — code-level hygiene: names, functions, comments, error handling.
- **Clean Architecture** (Martin, 2012/2017) — structural organization: layers, dependency direction, boundary crossings.

It also surveys adjacent schools (Hexagonal, Onion, BCE, DDD, Vertical Slice), the complementary practice of functional-core / imperative-shell design, and the most widely cited critiques. Every claim is attributed to a primary source where possible.

---

## 1. Why architecture matters (the economic case)

Martin Fowler frames the economic case plainly:

> "High internal quality reduces the cost of future features, meaning that putting the time into writing good code actually reduces cost."
> — Fowler, *Is High Quality Software Worth the Cost?*

His core mechanism: **cruft** (the gap between code as it is and code as it could be) imposes a compounding drag on every subsequent change. The productivity curve of a low-quality codebase degrades within weeks, not years. That tax — paid on every future edit — is what architecture is trying to keep small.

What "architecture" is *for* is therefore narrow and concrete:

1. Keep the cost of change roughly flat over time.
2. Defer decisions about volatile details (UI, DB, framework, cloud) so the business rules don't have to be re-written when those details change.
3. Make the thing the program *does* legible at the top level, not buried under the shape of the framework.

The remainder of this document is about how different authors have tried to achieve those three outcomes.

---

## 2. SOLID and the broader principle canon

### 2.1 SOLID

The acronym was coined ~2004 by Michael Feathers; the constituent principles were formalized in Robert C. Martin's 2000 paper *Design Principles and Design Patterns*.

**S — Single Responsibility Principle.** Martin's canonical phrasing: "A module should have one, and only one, reason to change" — where *reason* means an **actor**: a single tightly-coupled group of people (or, more operationally, one business role) that can request changes to that module. The folk reading "a class should do one thing" is *not* Martin's. Two methods that both "do one thing" but serve two different stakeholders still violate SRP. Martin's clarification: "[SRP] is about people."

Sketch (SRP violation with three actors):

```ts
class Employee {
  calculatePay()   { /* CFO rules */ }
  reportHours()    { /* COO rules */ }
  save()           { /* CTO rules */ }
}
// SRP-aligned split: PayrollCalculator, HourReporter, EmployeeRepo.
```

**O — Open/Closed Principle.** Bertrand Meyer (1988): a module is "open for extension" but "closed for modification." Meyer's mechanism was implementation inheritance; Martin's refinement is polymorphic substitution — depend on a stable abstraction, add new behavior by writing new implementations. Strategy is the archetypal pattern that honors OCP.

Misapplication: speculative extension points for every imagined axis of change. OCP is a response to *observed* variability, not a preemptive defense.

**L — Liskov Substitution Principle.** Barbara Liskov (1988 OOPSLA) and Liskov & Wing (1994 ACM TOPLAS) give the rigorous version: a subtype must honor the supertype's contract so that a caller written against the supertype cannot tell the difference. Operationally:

- Preconditions cannot be *strengthened* in the subtype.
- Postconditions cannot be *weakened*.
- Invariants cannot be weakened.
- History constraint: subtypes must not permit state changes the supertype forbade.
- Method parameters are contravariant; returns covariant; thrown exceptions narrowed.

The classic `Square extends Rectangle` example violates LSP because `setWidth` silently sets height, strengthening an invariant the caller did not expect. LSP applies to any subtyping relation — classical inheritance, interfaces, structural typing, duck typing.

**I — Interface Segregation Principle.** "No code should be forced to depend on methods it does not use." The origin is Martin's consulting work at Xerox, where a single fat `Job` class forced hour-long recompiles for trivial changes. The fix is **role interfaces**: one small interface per client responsibility, all implemented by the same class if needed.

**D — Dependency Inversion Principle.**

> (1) High-level modules should not import anything from low-level modules. Both should depend on abstractions.
> (2) Abstractions should not depend on details. Details should depend on abstractions.

The most misunderstood piece is **ownership**: the abstraction is owned by the *higher* (policy) layer, not by the implementor. The interface lives with the consumer. This is what "inversion" refers to — the dependency arrow reverses from the intuitive top-down direction. DIP is a design principle; Dependency Injection is a mechanism. Injecting a concrete class satisfies DI but violates DIP.

### 2.2 Other foundational principles

**DRY — Don't Repeat Yourself.** Hunt & Thomas, *The Pragmatic Programmer* (Tip 15): "Every piece of knowledge must have a single, unambiguous, authoritative representation within a system." DRY is about **knowledge**, not characters. Two functions that look alike but represent different business concepts are *not* duplication; merging them couples unrelated ideas. Sandi Metz's "The Wrong Abstraction" (2016) is the canonical refinement: "Duplication is far cheaper than the wrong abstraction." Dan Abramov's "The Wet Codebase" (2020) echoes it: similarity of appearance is not similarity of meaning.

**Rule of Three.** Don Roberts, via Fowler's *Refactoring*: "Three strikes and you refactor." Two occurrences do not yet justify abstraction; the third is the signal that the shape is real.

**YAGNI — You Aren't Gonna Need It.** Kent Beck / Extreme Programming. Build for today's demand, not tomorrow's guess. Operational twin of the anti-speculative reading of OCP.

**KISS — Keep It Simple.** Lockheed Skunk Works (Kelly Johnson). Prefer the simplest design that solves the present problem.

**Law of Demeter.** Lieberherr & Holland, *IEEE Software* (1989). A method `m` of `a` may only call methods on: `a` itself, `m`'s parameters, objects instantiated in `m`, and `a`'s direct attributes. Shorthand: "use only one dot." Method chaining on a single conceptual object (fluent builders) does *not* violate LoD — LoD forbids reaching *through* an object to manipulate a *different* object's internals.

**Tell, Don't Ask.** The behavioral form of LoD. Move the decision to the data rather than pulling state out, deciding externally, and pushing state back in. `account.withdraw(amount)` beats `if (account.getBalance() > amount) account.setBalance(account.getBalance() - amount)`.

**Command-Query Separation.** Meyer (1988). Every method is either a **command** (changes state, returns nothing) or a **query** (returns a value, no observable side effects). "Asking a question should not change the answer." Fowler notes one pragmatic exception: `stack.pop()`.

**Composition over Inheritance.** Gang of Four (1994): "Favor object composition over class inheritance." Implementation inheritance creates tight coupling between base and subclass (the fragile base class problem); composition exposes a smaller, more stable surface.

**Principle of Least Astonishment.** Every construct should behave as its syntax/name suggests. A method named `getUser` should not also send an email.

**Make Illegal States Unrepresentable.** Yaron Minsky (Jane Street, "Effective ML", 2010); popularized in the OO world by Scott Wlaschin's *Domain Modeling Made Functional*. Use algebraic data types / discriminated unions so the *compiler* rejects invalid combinations, not runtime guards. `type LoadState = { kind: "idle" } | { kind: "loading" } | { kind: "ok"; data: T } | { kind: "error"; err: E }` is strictly safer than `{ isLoading, isError, data?, err? }`, which has combinations like `isLoading && isError && data` that should be impossible but aren't.

**Parse, Don't Validate.** Alexis King (2019). Validation checks and discards; parsing checks and returns a *more precisely typed* value so downstream code never re-verifies the same invariant. King: "Push the burden of proof upward as far as possible." The return type of the parse is the proof.

**Beck's Four Rules of Simple Design**, in priority order: (1) passes the tests, (2) reveals intention, (3) no duplication, (4) fewest elements.

---

## 3. Clean Architecture (Martin, 2012/2017)

### 3.1 The Dependency Rule

The one rule the entire architecture serves:

> "Source code dependencies can only point inwards. Nothing in an inner circle can know anything at all about something in an outer circle. In particular, the name of something declared in an outer circle must not be mentioned by the code in an inner circle."
> — Martin, "The Clean Architecture", 2012

Every other convention in Clean Architecture — concentric circles, DTOs at boundaries, input/output ports, the humble object — is a mechanism for preserving this invariant. What it buys you: independence from frameworks/DB/UI, testability without I/O, and localization of change (volatile things can't force edits to stable things).

Enforcement in practice:

- **Compile-time**: inner packages import nothing from outer ones. In Go, `app/usecase` must not import `app/postgres` or `app/http`. In TypeScript, `core/` must not import from `infrastructure/`.
- **Tooling**: `dependency-cruiser` (TS), `ts-arch`, `go-cleanarch`, `ArchUnit` (JVM) can fail the build on illegal imports.
- **Runtime wiring**: a composition root (`main`, `cmd/app/main.go`, Next.js root layout) injects concrete adapters into interfaces owned by the core.

### 3.2 The concentric circles

Martin draws four. The count is schematic; the rule is not.

| Circle | Contents | Forbidden to know about |
|---|---|---|
| **Entities** | Enterprise-wide business rules. Pure types and invariants. Least likely to change for external reasons. | Everything outer. |
| **Use Cases** | Application-specific business rules. Orchestrates entities for one request. | Controllers, presenters, gateways, frameworks. |
| **Interface Adapters** | Converts data between use-case/entity form and external form. Controllers, presenters, gateways, view models, ORM mappers. MVC lives here. | Frameworks, DBs, I/O internals. |
| **Frameworks & Drivers** | "This is where all the details go." Web frameworks, DBs, UI toolkits, SDKs, message brokers. | Nothing inward. |

### 3.3 Crossing boundaries

Martin is strict about what crosses:

> "Typically the data that crosses the boundaries is simple data structures. You can use basic structs or simple Data Transfer objects … We don't want to cheat and pass Entities or Database rows."

Concretely:

- No ORM row objects reaching the use case.
- No framework request/response types leaking inward.
- No entity classes serving as API responses.
- DTOs are plain data; they carry no framework annotations that would tie them to an outer library.

### 3.4 Input / output ports and the humble object

Control flows outward even though source dependencies point inward. That is resolved by **ports** — interfaces owned by the use-case layer:

```
HTTP Request → Controller → InputPort (iface) → Interactor
                                                   │
                                                   ▼
                                               OutputPort (iface) ← Presenter → View
```

The **Humble Object pattern** (Meszaros; adopted in *Clean Architecture*) is the mechanism for keeping I/O-touching code at the edge: split a behavior into two pieces — one that is hard to test (touches framework/UI/IO) and one that is easy to test (pure logic). The "humble" half has almost no logic; all the interesting code sits in its testable partner.

- Views are humble; presenters format view models and are fully testable.
- DB gateways are humble; the translation logic is tested separately.
- Webhook listeners are humble; the decoding/dispatching logic is tested in isolation.

Go sketch:

```go
// inner: app/usecase — owns the interface.
type OrderRepo interface { Save(ctx context.Context, o Order) error }

func PlaceOrder(ctx context.Context, repo OrderRepo, o Order) error {
    // pure business rules here; no http/db imports
    return repo.Save(ctx, o)
}

// outer: app/postgres — implements the inner interface.
type PGOrderRepo struct{ db *sql.DB }
func (r PGOrderRepo) Save(ctx context.Context, o usecase.Order) error { /* ... */ }
```

### 3.5 Screaming Architecture

Martin, 2011:

> "Architectures are not (or should not be) about frameworks. … If your system architecture is all about the use cases, and if you have kept your frameworks at arm's length, then you should be able to unit-test all those use cases without any of the frameworks in place."
>
> "The fact that your application is delivered over the web is a *detail* and should not dominate your system structure."

Practical consequence: top-level folders should announce the **domain** (`ordering/`, `proposals/`, `billing/`) rather than the framework (`controllers/`, `services/`, `models/`). A stranger opening the repo should see the business, not the stack.

### 3.6 Component principles

Martin defines six principles at the component level (*Agile Software Development*, 2002; *Clean Architecture*, 2017), split into cohesion (what goes in a component) and coupling (how components relate).

**Cohesion — REP, CCP, CRP.**

- **REP (Reuse/Release Equivalence Principle).** "The granule of reuse is the granule of release." A component can only be reused if it is tracked, versioned, and released as a unit.
- **CCP (Common Closure Principle).** SRP at component scale: gather into one component classes that change for the same reasons and at the same times, so a requirement change touches one component.
- **CRP (Common Reuse Principle).** "Don't force clients to depend on things they don't need." Classes that aren't reused together belong in different components. (CRP is ISP at component scale.)

These pull against each other: REP and CCP are inclusive (push toward larger components); CRP is exclusive (push toward smaller ones). Early-stage projects typically favor CCP (ease of change); mature projects drift toward REP and CRP (reusability, minimal incidental coupling).

**Coupling — ADP, SDP, SAP.**

- **ADP (Acyclic Dependencies Principle).** The component dependency graph must be a DAG. Cycles make every component in the cycle share a release lifecycle and destroy isolated testing. Break cycles by extracting a shared component or by inverting a dependency with an interface.
- **SDP (Stable Dependencies Principle).** "Depend in the direction of stability." Volatile components depend on stable ones, never the reverse. Martin's instability metric `I = Ce / (Ca + Ce)` (efferent coupling over total coupling) makes this measurable.
- **SAP (Stable Abstractions Principle).** "A component should be as abstract as it is stable." Stable components must consist mostly of interfaces/abstract classes so they can be extended without being modified. SDP + SAP together are DIP at component scale.

---

## 4. Hexagonal, Onion, BCE — the same conviction from different angles

All four architectures — Clean, Hexagonal, Onion, BCE — share one conviction: **domain at the center, frameworks at the edge, dependencies point inward.** They differ mostly in vocabulary, diagrammatic emphasis, and era.

### 4.1 Hexagonal (Ports & Adapters), Cockburn, 2005

Cockburn's one-sentence intent:

> "Allow an application to equally be driven by users, programs, automated test or batch scripts, and to be developed and tested in isolation from its eventual run-time devices and databases."

- **Ports** are technology-agnostic interfaces at the application boundary.
- **Adapters** translate between a specific technology (HTTP, SQL, Kafka, GUI) and a port.
- **Primary / driving** side: tests, CLI, HTTP handlers, UIs drive the app via input ports.
- **Secondary / driven** side: DBs, brokers, external APIs are driven via output ports.

The hexagon shape is arbitrary — Cockburn chose it because it gave room for many sides without implying a fixed number. "A common misconception is that … we must have exactly 6 ports — this is not correct."

Primary vs. secondary is about *who initiates the conversation*. On the primary side, adapters invoke ports implemented by the core. On the secondary side, the core invokes ports implemented by adapters. That asymmetry is the essential insight.

### 4.2 Onion Architecture, Palermo, 2008

Palermo's rule:

> "All code can depend on layers more central, but code cannot depend on layers further out from the core."

Rings: Domain Model → Domain Services → Application Services → Outer (UI, Infrastructure, Tests). Repository *interfaces* live in the core; *implementations* live at the edge. Palermo's memorable framing: "The database is not the center. It is external."

Onion added explicit concentric layering to Cockburn's inside/outside split and made the dependency-inversion-at-edges pattern first-class.

### 4.3 BCE (Boundary / Control / Entity), Jacobson, 1992

From *Object-Oriented Software Engineering: A Use Case Driven Approach*.

- **Entity**: long-lived, stakeholder-relevant domain data and rules.
- **Boundary**: anything that speaks to an external actor (UI, service clients, hardware).
- **Control**: coordinates the behavior needed to realize a use case.

Communication rules: actors → boundaries; boundaries → controls; controls → entities, other controls, boundaries. BCE predates the others and explicitly centers **use cases** as the design unit.

### 4.4 Why multiple names exist

Each author attacked the same anti-pattern ("business logic tangled with UI/DB/framework") from a different angle and era. Martin (2012) explicitly says Clean Architecture is a synthesis of Hexagonal, Onion, BCE, DCI, and Screaming Architecture.

| Emphasis | Clean | Hexagonal | Onion | BCE |
|---|---|---|---|---|
| Named rings | Many | Just in/out | Many | Roles not rings |
| Use case as explicit layer | Yes | No | Kind of (Application Services) | Central (Control) |
| Primary/secondary symmetry | Implicit | Explicit | Implicit | Implicit (Boundary) |
| First to publish | 2012 | 2005 | 2008 | 1992 |

---

## 5. Code-level Clean Code (Martin, 2008) and its refinements

### 5.1 Names

- Intention-revealing (`elapsedTimeInDays`, not `d`).
- Pronounceable and searchable (avoid single letters outside short scopes, avoid magic numbers).
- No disinformation (don't call something `accountList` if it isn't a `List`).
- One word per concept (pick `fetch`, `get`, or `retrieve` and commit).

### 5.2 Functions

Martin's recommendations (*Clean Code*, Ch. 3):

- Small — "hardly ever 20 lines long"; his own examples are often 2–4 lines.
- Do one thing, at one level of abstraction.
- Stepdown rule: read top-to-bottom as descending abstraction.
- Few arguments (0–2 ideal); avoid flag arguments (they declare the function does more than one thing).
- No side effects; CQS at the function level.

**Ousterhout's pushback (*A Philosophy of Software Design*, 2018/2021).** Ousterhout argues Martin's length rules produce **shallow modules** and **entanglement**. His counter-model:

- The best modules are **deep**: powerful functionality behind a simple interface.
- Shallow modules — interface complexity close to implementation complexity — don't hide enough to pay for themselves.
- Ousterhout: "Methods containing hundreds of lines of code are fine if they have a simple signature and are easy to read."
- On "Do One Thing": "vague and easy to abuse — anything can be named."

The practical reconciliation: extract a function when the extraction produces a *deep* abstraction (rich behavior behind a narrower interface); don't extract when it yields a shallow one. The trigger is depth, not line count.

### 5.3 Comments

Default per Martin: prefer intent in code (names, types, small functions) before writing prose. He allows "good" comments — legal headers, intent, clarification of an unchangeable API, warnings, `TODO`s, public-API docstrings — and forbids "bad" comments — redundant paraphrase, commented-out code, journal/changelog entries, noise.

Ousterhout dissents on interface/docstring comments: "The cost of missing comments is easily 10–100x the cost of incorrect comments." His view: interface comments are required because they define the contract; types and names rarely fully express it.

Synthesis for this project's rules file: prefer intent in code; keep comments for *non-obvious external constraints* (vendor limits, compliance rules, incident history, upstream bugs). Interface comments on exported APIs are worthwhile when the contract isn't fully expressed by types.

### 5.4 Error handling

Martin's book advocates exceptions over return codes and "don't return null, don't pass null." The invariants he defends — explicitness, no silent failure, no sentinel nulls — travel well. The *mechanism* (exceptions) does not always.

**Errors as values (Go, Rust, OCaml, functional TS).** For expected failure paths, prefer a typed result (`Result<T, E>`, `T | Error`, Go's `(T, error)`, a tagged union). Reserve thrown exceptions for truly exceptional, unrecoverable, or framework-boundary cases. Propagate errors explicitly; don't swallow them or substitute a success-shaped fallback.

**Don't return / pass null.** Use `Option` / `Maybe`, empty collections, Null Object, or discriminated unions. In TypeScript, `T | null` is acceptable *if checked at the boundary*; silent `!` non-null assertions aren't.

Pair with "Make illegal states unrepresentable" (§2.2) and "Parse, don't validate" (§2.2) — together they pull error handling upstream into type definitions.

### 5.5 Boundaries (wrapping third-party APIs)

Martin's Ch. 8. Wrap vendor SDKs at the edge so vendor types never reach the core. This is DIP in practice: the core depends on an interface it owns; the wrapper is the adapter. It is also DDD's **Anti-Corruption Layer** under a different name.

---

## 6. Domain-Driven Design (Evans, 2003)

DDD is often treated as a separate school, but it is the most direct source of clean-architecture-style concepts at the domain layer.

### 6.1 Strategic patterns

- **Ubiquitous Language.** A shared, rigorous vocabulary co-developed by engineers and domain experts. Fowler: "By using the model-based language pervasively and not being satisfied until it flows, we approach a model that is complete and comprehensible." The language is enforced in code — names of types, methods, modules.
- **Bounded Context.** A boundary within which a model is internally consistent. Different contexts can use the same word for different concepts ("Order" in Sales vs. Fulfillment) without confusion.
- **Context Map.** A diagram of how bounded contexts relate and integrate (shared kernel, customer/supplier, conformist, anti-corruption layer, open host service, published language, separate ways, big ball of mud).
- **Anti-Corruption Layer.** An isolating translator between your bounded context and an upstream system's model. Prevents the upstream vocabulary from corrupting your domain.

### 6.2 Tactical patterns

| Pattern | Definition |
|---|---|
| **Entity** | An object whose identity — not its attributes — defines what it is. |
| **Value Object** | Immutable, attribute-defined, no identity. |
| **Aggregate** | A cluster of domain objects bound by an aggregate root that governs internal consistency. |
| **Aggregate Root** | The single entry point through which outside code interacts with an aggregate. |
| **Domain Event** | A record of something meaningful that happened in the domain. |
| **Service** | Stateless logic that doesn't belong to any single object, expressed as a standalone operation. |
| **Repository** | Retrieval/persistence abstraction for an aggregate. Interface in the core, implementation at the edge. |
| **Factory** | Encapsulates construction of complex domain objects. |
| **Module** | A cohesive grouping of related domain concepts. |

### 6.3 Domain Services vs. Application Services

Khorikov's useful distinction: **domain services hold domain logic; application services don't.** Application services orchestrate — gather inputs, invoke the domain model, commit results. When business logic *requires* external data to make a decision and can't cleanly sit on an entity, a domain service is the middle ground.

### 6.4 Anemic Domain Model (anti-pattern)

Fowler:

> "By pulling all the behavior out into services, you essentially end up with Transaction Scripts … If all your logic is in services, you've robbed yourself blind."

An anemic model — objects that are bags of getters and setters with all logic in services — pays the full O/R mapping cost of a domain model without getting any of the benefit. In clean-architecture terms: entities should enforce their own invariants; services coordinate but don't own the rules.

---

## 7. Complementary patterns

### 7.1 Functional Core, Imperative Shell (Bernhardt, 2012)

Gary Bernhardt's *Boundaries* talk, SCNA 2012. Split a system into two halves:

- **Functional core**: pure logic, no side effects, no I/O. All decisions live here. Exhaustively unit-testable without test doubles.
- **Imperative shell**: a thin wrapper that performs I/O, state changes, and world-orchestration. Few decision paths; tested at the integration level.

The pattern is the same instinct as the humble object at a smaller scale. Push *all decisions inward* (pure functions) and *all effects outward* (thin shell). The payoff is high test coverage of the interesting code without mocks.

### 7.2 CQRS — Command Query Responsibility Segregation

Greg Young's pattern, distinct from Meyer's CQS:

> "The notion that you can use a different model to update information than the model you use to read information." — Fowler

Useful when (a) the read and write models diverge materially, (b) independent scaling matters, (c) the domain has rich complexity on both sides. Fowler is explicit that "for most systems CQRS adds risky complexity." Apply it at a specific bounded context, not system-wide.

### 7.3 Event Sourcing

Store the sequence of state-changing events, derive current state by replay. Composes naturally with CQRS and with domain events from DDD. Trade-off: schema evolution of historical events and projection rebuilds are real operational costs.

### 7.4 Vertical Slice Architecture (Bogard)

> "Minimize coupling between slices, and maximize coupling in a slice."

Organize code around features (the HTTP request, the command, the query) rather than horizontal layers. Each slice owns its entire stack front-to-back; shared abstractions emerge only after three repetitions show their shape. CQRS falls out naturally because GET and POST handlers can implement exactly what they need.

Bogard's critique of forced Clean/Onion layering: "mock-heavy, with rigid rules around dependency management that are rarely useful." His trade-off warning: the pattern requires a team that can recognize when a slice has grown into something worth extracting — "if your team does not understand when a service is doing too much, this pattern is likely not for you."

### 7.5 Make Illegal States Unrepresentable + Parse Don't Validate

Covered in §2.2. These are architectural decisions at the type level. Types are the earliest, cheapest, and most durable enforcement mechanism for invariants; runtime checks are a fallback. In a TypeScript or Go codebase, this often looks like:

- Discriminated unions instead of optional-field bags.
- Branded / nominal types for validated strings (`UserId`, `Email`, `E164Phone`).
- Constructors that parse at system boundaries; internal code operates on the precise types.

---

## 8. Dependency Injection and the Composition Root

DIP (§2.1) is a *principle* about the direction of source-code dependencies. Dependency Injection (DI) is the *mechanism* that realizes it at runtime. Four forms, from Seemann & van Deursen's *Dependency Injection Principles, Practices, and Patterns* (Manning, 2019):

- **Constructor injection.** Dependencies are mandatory; passed at construction. The default, strongly preferred.
- **Method injection.** A dependency specific to one operation is passed to that method.
- **Property (setter) injection.** Dependency is optional with a safe default. Rarely appropriate.
- **Service locator / ambient context.** Anti-patterns (Seemann): dependencies are hidden, testability collapses, the compiler can't tell you what a class needs.

**Composition Root (Seemann, 2011).** "A Composition Root is a (preferably) unique location in an application where modules are composed together." Place it as close to the entry point as possible — `main()`, `cmd/app/main.go`, the Next.js route bootstrap, the HTTP handler registration. One root per process. DI containers, if used at all, live only here; application code should not know they exist.

**Functional alternative — "pass the function" (Wlaschin).** In F#, Elm, and functional-leaning TypeScript/Go, dependencies are ordinary parameters. A function `saveUser: (conn: DbConn, user: User) => Result<void, Error>` is partially applied at the root with a real `conn`, producing a `(user: User) => Result<void, Error>` that downstream code uses with no knowledge of the DB. This generalizes constructor injection — a class with one method and constructor-injected deps is isomorphic to a closure over those deps. DI containers are one choice among many; in functional code they are usually overkill.

**Where DI meets Clean Architecture.** The Dependency Rule is implemented *through* DI: the inner layer declares the interface; the outer layer implements it; the composition root wires them up. But the common failure mode is teams that mistake "has lots of DI" for "follows Clean Architecture" and produce shallow interfaces (`IHtmlSanitizer.Sanitize(string): string`) that add indirection without encapsulating anything — the shallow-module anti-pattern (§5.2).

---

## 9. Testing architecture

### 9.1 The Test Pyramid (Cohn, popularized by Fowler)

- **Base — Unit tests.** Fast, focused, numerous.
- **Middle — Service / integration / contract tests.** Test through an API or service boundary, bypassing UI.
- **Top — End-to-end / UI tests.** Fewest; most brittle; slowest.

> "Tests that run end-to-end through the UI are brittle, expensive to write, and time consuming to run." — Fowler

The **ice-cream-cone** anti-pattern inverts the pyramid by leaning heavily on UI tests; record-playback tooling accelerates this drift.

### 9.2 The Testing Trophy (Kent C. Dodds)

A four-layer reordering for modern typed front-ends:

1. **Static** (ESLint, TypeScript)
2. **Unit**
3. **Integration** (the largest slice)
4. **End-to-end**

Rauch's one-line summary: "Write tests. Not too many. Mostly integration." Dodds' argument: unit tests in isolation can give false confidence when components pass individually but break at their seams; integration tests hit the seams where the interesting bugs live. Corollary: "Stop mocking so much. When you mock something you're removing all confidence in the integration between what you're testing and what's being mocked."

### 9.3 Test doubles (Meszaros taxonomy, via Fowler)

| Kind | Behavior |
|---|---|
| Dummy | Fills parameter lists; never used. |
| Fake | Working implementation with shortcuts (e.g., in-memory DB). |
| Stub | Returns canned answers. State verification. |
| Spy | A stub that also records calls. |
| Mock | Pre-programmed with call expectations. Behavior verification. |

Fowler's classical vs. mockist distinction: classical TDD uses doubles only for awkward collaborators and verifies state; mockist TDD mocks every collaborator and verifies protocol. Mockist tests couple tightly to implementation and break under refactoring; classical tests behave more like mini-integration tests and catch seam bugs.

### 9.4 "Don't mock what you don't own"

Steve Freeman and Nat Pryce, *Growing Object-Oriented Software, Guided by Tests* (2009). Mock only your *own* interfaces, not a third party's. Mocking a third-party library encodes your *guess* about its behavior; when the library upgrades or behaves differently in reality, the test silently diverges from truth. The architectural implication is Hexagonal: wrap the third party behind an interface you own, then mock the wrapper where testing requires it. Pair with a small suite of contract tests against the real third party to keep the mock honest.

### 9.5 Property-based testing as an architectural signal

QuickCheck (Claessen & Hughes, ICFP 2000), Hypothesis (Python), fast-check (TS/JS). Property tests assert universal laws (`reverse(reverse(xs)) === xs`, `parse(render(x)) === x`) over arbitrary inputs and shrink counterexamples to minimal failures. They require determinism and referential transparency, which means a **pure functional core is the ideal subject**; anything entangled with clocks, network, or global state must be isolated or injected before property tests become practical.

The architectural consequence: codebases that already separate I/O from computation (Bernhardt's functional core, Wlaschin's pipelines, Hexagonal's inside/outside split) unlock property-based testing as a nearly free byproduct. Codebases that mix domain logic with ORMs, HTTP clients, or module-level globals can't — they'd need heavy test doubles, which defeat the shrinking and determinism that make property testing powerful. If your architecture can support property-based testing, that's a signal it also supports the other benefits of a pure core.

### 9.6 F.I.R.S.T.

Unit tests should be Fast, Independent, Repeatable, Self-validating, Timely. Martin's shorthand.

---

## 10. Critiques and practical guidance

### 10.1 The "Wrong Abstraction" trap

Metz's pattern (2016): a programmer spots duplication, extracts it, leaves. A later programmer needs to handle an almost-matching case and adds a parameter and a conditional. Loop until the abstraction is incomprehensible. Sunk-cost thinking preserves the abstraction long after it has turned toxic.

Her recovery prescription: **inline the abstraction back into every caller, strip each caller to what it actually uses, then re-derive.** "The fastest way forward is back."

### 10.2 Over-abstraction / useless indirection

Derek Comartin (2023), on Clean Architecture in .NET:

> "A class with one method is a function. And we've now gone two levels deep of a function, calling a function and doing nothing else. … You're still just as coupled, but with more indirection."

Wrapping `DbContext` behind `IRepository<T>` while still exposing `DbSet<T>` is the canonical case: zero decoupling, more files. The principle Comartin offers as a counterweight: "Coupling isn't bad on its own. What you should be paying attention to is the *degree* of coupling" — coupling to a stable dependency (React, Postgres) is often fine; investing in abstractions to avoid it can be pure cost.

### 10.3 DTO fatigue and layer tax

Strict Clean Architecture layering tends to re-declare the same logical record as a DB entity, a domain entity, a use-case input DTO, a use-case output DTO, and an API DTO. Teams with 50+ DTOs pay real maintenance cost (see critiques from 2024 by Arend van Beelen Jr., Jeremy D. Miller).

### 10.4 The modern-framework tension

Martin's "the web is a detail" held up well in 2012 but is strained by Next.js, Remix, and serverless runtimes that deeply shape where code can run (server components vs. client components, edge vs. node, streaming boundaries, cold starts). Clean Architecture still works here — keep the framework in the outer ring, put use cases in pure modules, inject infrastructure at the composition root — but it requires discipline and acknowledges that the framework shapes runtime topology even when it doesn't shape the domain.

### 10.5 The Case Against Clean Architecture (Miller, 2024)

Jeremy D. Miller's five concrete criticisms:

1. Prescriptive rules over outcomes.
2. Inflexibility once the rules calcify.
3. Code organized by technical stereotype rather than by feature.
4. Hidden coupling *within* layers.
5. Over-abstraction driven by mock-based testing.

His proposed alternative is Vertical Slice Architecture plus teaching judgment instead of rules. Notably, he frames the critique as targeted at *common misapplications* rather than the concept itself.

### 10.6 Kent Beck's Tidy First

*Tidy First?* (2024): separate **structural changes** (rearrange, rename, extract — no behavior change) from **behavioral changes** (new feature, bug fix). Never mix them in the same commit. "Tidyings" are small, bounded structural changes that make the next behavior change cheaper. The question mark in the title is deliberate: sometimes you tidy first, sometimes after, sometimes never. The unifying frame is optionality — well-structured code preserves future choices; the value of those options often exceeds the cost of the tidy.

### 10.7 Conceptual compression (DHH)

David Heinemeier Hansson's counter-thesis to layered architecture: opinionated frameworks (Rails, Django, Next.js) absorb infrastructure complexity so small teams can ship full systems. For many teams, *tight* framework coupling is cheaper than an abstract hexagonal scaffold — the framework is a stable dependency whose churn is someone else's problem. This is not an argument against Clean Architecture per se; it is an argument that the premium Clean Architecture pays on swappability is often a premium not worth paying. It lands most clearly in small product teams on a single long-lived codebase.

### 10.8 When not to use Clean Architecture

The community's practical heuristics, gathered from 2023–2025 posts:

- Small teams on a single product under known constraints often pay more in ceremony than they save in decoupling.
- Prototypes and throwaway scripts should optimize for time-to-useful-feedback.
- Stable, long-lived infrastructure dependencies (e.g., Postgres on a backend you own) rarely justify wrapping.
- Modern typed frameworks (Next.js, Rails 7, Django) already impose significant structure; adding Clean Architecture on top can double-layer.
- Vertical Slice Architecture is a common middle ground: the feature is the unit of organization; shared abstractions emerge only when a third instance demands them.

### 10.9 When to use it

- Multiple delivery mechanisms (web + CLI + queue consumer + scheduled job) for the same business rules.
- Long-lived systems where the database/ORM/framework will likely outlive no decision about them.
- Regulated domains where business rules must be auditable and testable without I/O.
- Teams with genuinely different stakeholders per module, where SRP-as-actor-alignment pays off.

### 10.10 Signs you have "too much architecture"

- Interfaces with exactly one implementation and no test double of real value.
- Named roles that track your layer template (`IFooService`, `IFooRepository`, `IFooValidator`) rather than your domain.
- Adding a single field to a domain concept requires touching 5+ files across 3+ layers.
- Mocks of your own collaborators everywhere; tests fail on refactor, not on behavior change.
- "Primary domain logic" files that contain only orchestration, no rules.
- Shallow modules (Ousterhout): interfaces about as complex as the code behind them.

### 10.11 Signs you have "not enough architecture"

- Business rules embedded in controllers, views, or ORM callbacks.
- Conditional logic that branches on framework state (`if (request.method === "POST")`) instead of on domain state.
- Swapping any I/O subsystem requires rewriting core logic.
- No pure core; every function needs a database to test.
- Third-party types (ORM models, vendor SDK objects) leaking into business logic.
- The same business rule duplicated across modules that should share one model.

---

## 11. A compact heuristic

When adding or reviewing architecture, ask four questions in order:

1. **Is there an explicit dependency pointing from stable to volatile?** If yes, invert it with an interface owned by the stable side.
2. **Do boundary crossings leak framework/DB types?** If yes, insert a DTO or adapter at the edge.
3. **Can I unit-test the business rule without starting the framework, DB, or network?** If no, something is mislabeled.
4. **Would a senior engineer opening the top-level folder structure see the *domain*?** If no, rename folders before adding code.

These are enough to capture most of the value of Clean Architecture without the ceremony of applying it uniformly.

---

## References

### Primary sources — Robert C. Martin

- "The Clean Architecture" (2012). <https://blog.cleancoder.com/uncle-bob/2012/08/13/the-clean-architecture.html>
- "Screaming Architecture" (2011). <https://blog.cleancoder.com/uncle-bob/2011/09/30/Screaming-Architecture.html>
- "The Single Responsibility Principle" (2014). <https://blog.cleancoder.com/uncle-bob/2014/05/08/SingleReponsibilityPrinciple.html>
- "Solid Relevance" (2020). <https://blog.cleancoder.com/uncle-bob/2020/10/18/Solid-Relevance.html>
- *Clean Code: A Handbook of Agile Software Craftsmanship* (Prentice Hall, 2008).
- *Agile Software Development, Principles, Patterns, and Practices* (Prentice Hall, 2002). Component principles.
- *Clean Architecture: A Craftsman's Guide to Software Structure and Design* (Prentice Hall, 2017).

### Hexagonal, Onion, BCE

- Cockburn, "Hexagonal Architecture (Ports and Adapters)." <https://alistair.cockburn.us/hexagonal-architecture/>
- Wikipedia, *Hexagonal Architecture (software)*. <https://en.wikipedia.org/wiki/Hexagonal_architecture_(software)>
- Palermo, "The Onion Architecture: Part 1" (2008). <https://jeffreypalermo.com/2008/07/the-onion-architecture-part-1/>
- Jacobson, *Object-Oriented Software Engineering: A Use Case Driven Approach* (1992).
- Wikipedia, *Entity-Control-Boundary*. <https://en.wikipedia.org/wiki/Entity-control-boundary>

### SOLID and principle canon

- Liskov & Wing, "A Behavioral Notion of Subtyping" (ACM TOPLAS, 1994). <https://www.cs.cmu.edu/~wing/publications/LiskovWing94.pdf>
- Meyer, *Object-Oriented Software Construction* (1988). Source for OCP and CQS.
- Hunt & Thomas, *The Pragmatic Programmer* (1999). DRY (Tip 15).
- Lieberherr & Holland, "Assuring Good Style for Object-Oriented Programs," *IEEE Software* 6(5), 1989. Law of Demeter.
- Wikipedia, *SOLID*. <https://en.wikipedia.org/wiki/SOLID>
- Fowler, "CommandQuerySeparation." <https://martinfowler.com/bliki/CommandQuerySeparation.html>
- Fowler, "TellDontAsk." <https://martinfowler.com/bliki/TellDontAsk.html>
- Fowler, "BeckDesignRules." <https://martinfowler.com/bliki/BeckDesignRules.html>
- Gamma, Helm, Johnson, Vlissides, *Design Patterns* (Addison-Wesley, 1994). "Favor composition over inheritance."

### DDD and related

- Evans, *Domain-Driven Design: Tackling Complexity in the Heart of Software* (Addison-Wesley, 2003).
- Vernon, *Implementing Domain-Driven Design* (Addison-Wesley, 2013).
- Fowler, "Domain-Driven Design." <https://martinfowler.com/bliki/DomainDrivenDesign.html>
- Fowler, "UbiquitousLanguage." <https://martinfowler.com/bliki/UbiquitousLanguage.html>
- Fowler, "AnemicDomainModel." <https://martinfowler.com/bliki/AnemicDomainModel.html>
- Khorikov, "Domain vs Application Services." <https://enterprisecraftsmanship.com/posts/domain-vs-application-services/>

### Complementary patterns

- Bernhardt, "Boundaries" (SCNA 2012). <https://www.destroyallsoftware.com/talks/boundaries>
- Bernhardt, "Functional Core, Imperative Shell" screencast. <https://www.destroyallsoftware.com/screencasts/catalog/functional-core-imperative-shell>
- Fowler, "Humble Object." <https://martinfowler.com/bliki/HumbleObject.html>
- Young, "CQRS Documents" (2010). <https://cqrs.wordpress.com/wp-content/uploads/2010/11/cqrs_documents.pdf>
- Young, "CQRS and Event Sourcing" (CodeBetter, 2010). <http://codebetter.com/gregyoung/2010/02/13/cqrs-and-event-sourcing/>
- Fowler, "CQRS." <https://martinfowler.com/bliki/CQRS.html>
- Fowler, "Event Sourcing" (2005). <https://martinfowler.com/eaaDev/EventSourcing.html>
- Bogard, "Vertical Slice Architecture" (2018). <https://www.jimmybogard.com/vertical-slice-architecture/>
- Minsky, "Effective ML" (Jane Street / Harvard, 2010). "Make illegal states unrepresentable." <https://blog.janestreet.com/effective-ml/>
- Wlaschin, *Domain Modeling Made Functional* (Pragmatic Bookshelf, 2017). <https://pragprog.com/titles/swdddf/domain-modeling-made-functional/>
- Wlaschin, "Designing with Types: Making Illegal States Unrepresentable." <https://fsharpforfunandprofit.com/posts/designing-with-types-making-illegal-states-unrepresentable/>
- Wlaschin, "Six approaches to dependency injection." <https://fsharpforfunandprofit.com/posts/dependencies/>
- King, "Parse, don't validate" (2019). <https://lexi-lambda.github.io/blog/2019/11/05/parse-don-t-validate/>

### Testing

- Cohn, *Succeeding with Agile* (Addison-Wesley, 2009). Origin of the Test Pyramid.
- Fowler, "TestPyramid." <https://martinfowler.com/bliki/TestPyramid.html>
- Fowler, "The Practical Test Pyramid" (2018). <https://martinfowler.com/articles/practical-test-pyramid.html>
- Fowler, "Mocks Aren't Stubs." <https://martinfowler.com/articles/mocksArentStubs.html>
- Kent C. Dodds, "Write tests. Not too many. Mostly integration." <https://kentcdodds.com/blog/write-tests>
- Kent C. Dodds, "The Testing Trophy and Testing Classifications" (2021). <https://kentcdodds.com/blog/the-testing-trophy-and-testing-classifications>
- Freeman & Pryce, *Growing Object-Oriented Software, Guided by Tests* (Addison-Wesley, 2009).
- Claessen & Hughes, "QuickCheck: A Lightweight Tool for Random Testing of Haskell Programs" (ICFP 2000). Property-based testing origin.
- fast-check (TypeScript/JavaScript). <https://github.com/dubzzz/fast-check>

### Dependency injection

- Seemann, "Composition Root" (2011). <https://blog.ploeh.dk/2011/07/28/CompositionRoot/>
- Seemann & van Deursen, *Dependency Injection Principles, Practices, and Patterns* (Manning, 2019).
- Fowler, "Inversion of Control Containers and the Dependency Injection pattern" (2004). <https://martinfowler.com/articles/injection.html>

### Critiques and refinements

- Ousterhout, *A Philosophy of Software Design* (2nd ed., 2021). <https://web.stanford.edu/~ouster/cgi-bin/book.php>
- Metz, "The Wrong Abstraction" (2016). <https://sandimetz.com/blog/2016/1/20/the-wrong-abstraction>
- Abramov, "The Wet Codebase" (2020). <https://overreacted.io/the-wet-codebase/>
- Beck, *Tidy First?* (O'Reilly, 2024).
- Fowler, "Is High Quality Software Worth the Cost?" <https://martinfowler.com/articles/is-quality-worth-cost.html>
- Fowler, Beck, DHH, "Is TDD Dead?" (2014). <https://martinfowler.com/articles/is-tdd-dead/>
- DHH, "Conceptual compression means beginners don't need to know SQL" (2016). <https://medium.com/signal-v-noise/conceptual-compression-means-beginners-dont-need-to-know-sql-hallelujah-661c1eaed983>
- Comartin, "'Clean Architecture' and indirection. No thanks." (CodeOpinion, 2023). <https://codeopinion.com/clean-architecture-and-indirection-no-thanks/>
- Miller, "The Case Against Clean Architecture" (2024). <https://jeremydmiller.com/2024/02/12/the-case-against-clean-architecture/>
- Smith (Ardalis), "Clean Architecture Sucks" (2024). <https://ardalis.com/clean-architecture-sucks/>
- Bosepchuk, "Why I Can't Recommend Clean Architecture by Robert C. Martin." <https://dev.to/bosepchuk/why-i-cant-recommend-clean-architecture-by-robert-c-martin-ofd>
- van Beelen Jr., "Post-Architecture: Premature Abstraction Is the Root of All Evil" (2024). <https://arendjr.nl/blog/2024/07/post-architecture-premature-abstraction-is-the-root-of-all-evil/>
- Three Dots Labs, "Introducing Clean Architecture" (Go). <https://threedots.tech/post/introducing-clean-architecture/>
- Three Dots Labs, "Is Clean Architecture Overengineering?" <https://threedots.tech/episode/is-clean-architecture-overengineering/>
