# AGENTS.md

## Purpose

This file is the execution guide for implementing the salon billing backend.

It exists to keep the build:
- simple
- consistent
- easy to review
- easy to extend later without rewriting core money flows

This backend must follow:
- **KISS**: choose the simplest design that fully solves the current requirement
- **YAGNI**: do not add future-proof abstractions unless the current design explicitly needs them
- **SOLID**: keep modules focused, dependencies one-way, and interfaces narrow

This file is implementation-first. It is not a marketing document.

---

## Source Documents

Treat these as the source of truth:
- `salon_billing_hld.md`
- `salon_billing_backend_only_lld.md`

If code and docs disagree, align code to the docs unless a later approved change updates the design.

---

## Product Goal

Build a backend for a multi-tenant salon billing system with:
- JWT auth
- store-scoped operations for managers
- admin CRUD for catalogue and staff
- bill creation with backend-owned money calculation
- cash, UPI, and split cash + UPI payment modes
- dynamic QR through Paytm and HDFC
- webhook-first payment confirmation with status fallback
- bill cancellation
- bill list and summary analytics
- minimal audit logging
- PostgreSQL as the only source of truth

---

## In Scope for v1

Implement only these backend capabilities:
- auth and request scope derivation
- store bootstrap snapshot
- catalogue CRUD needed by admin
- staff CRUD / deactivate / mapping needed by admin and store workflows
- create bill
- get bill
- cancel bill
- initiate UPI
- retry failed or pending-due UPI remainder
- payment webhooks
- payment status fallback check
- bills list
- analytics summary
- audit logging
- idempotency for create bill and retry UPI

---

## Out of Scope for v1

Do not build any of these unless the design is explicitly changed:
- offline billing
- customer profiles
- loyalty
- appointments
- inventory
- payroll
- refunds workflow inside the system
- event bus
- microservices
- reporting warehouse
- store-specific catalogue pricing
- multi-user billing workflow inside one store
- server-side PDF generation
- Redis as a required dependency
- background workflow engine 

---

## Locked Design Decisions

These are not open for redesign during implementation:
- Backend is a **Go modular monolith**
- Database is **PostgreSQL**
- Auth is **JWT**
- One `STORE_MANAGER` belongs to exactly one store in v1
- Backend derives `tenant_id`, `store_id`, `user_id`, `role` from auth
- All money values are stored and computed in **paise**
- Backend is the only source of truth for prices, tax, totals, and payment state
- One bill-level discount only
- Discount defaults to `0`
- Tip defaults to `0`
- Tip allocations default to empty when tip is zero
- Tip is non-taxable
- Commission is 10% of line net after discount
- UPI uses **dynamic QR**
- Gateways are **Paytm + HDFC**
- Payment confirmation is **webhook-first + status fallback**
- Retry creates a new UPI payment row only for outstanding due amount
- Idempotency is required for create bill and retry UPI
- No external gateway call may happen inside a DB transaction
- No optional abstraction layers

---

## Architecture Shape

### Module Structure

Use this structure:

```text
cmd/
  api/
    main.go

internal/
  auth/
  bootstrap/
  catalogue/
  staff/
  billing/
  payments/
  analytics/
  audit/
  platform/
    config/
    db/
    http/
    logger/
  shared/
    money/
    enums/
    errors/
    idempotency/
```

### Dependency Rules

Allowed direction:
- `handler -> service`
- `service -> repo, shared, platform, narrow interfaces`
- `repo -> platform/db, models`
- `payments/providers -> external gateway API`

Not allowed:
- handler calling repo directly
- one module importing another module's repo directly
- circular dependencies
- payment gateway logic inside billing
- generic base repository
- giant service objects that own unrelated logic

---

## Practical Interpretation of KISS, YAGNI, SOLID

### KISS
- Prefer plain structs and explicit functions over frameworks and clever patterns
- Keep each module small and obvious
- Keep transactions short
- Keep request and response DTOs explicit
- Use direct SQL or straightforward query builders; do not introduce heavy ORM complexity if not needed

### YAGNI
- Do not add plugins, strategy registries, event sourcing, or CQRS
- Do not add async job systems for work that can be done in-request safely
- Do not build generalized pricing engines
- Do not build generalized rule engines
- Do not introduce multi-store manager support in v1
- Do not add versioned bootstrap snapshots yet

### SOLID
- **S**: each module owns one clear business area
- **O**: add new payment gateway by implementing provider interface, not by rewriting billing
- **L**: provider implementations must satisfy the same contract and normalized outputs
- **I**: use small interfaces at service boundaries
- **D**: services depend on interfaces where replacement is useful; do not over-interface internal helper code without a concrete need

---

## Domain Rules That Must Never Be Broken

### Auth and Scope
- Client never sends trusted scope as business input
- JWT must resolve `tenant_id`, `store_id`, `user_id`, `role`
- `STORE_MANAGER` is store-scoped
- `SUPER_ADMIN` is tenant-scoped or broader per design, but admin operations still enforce tenant boundaries

### Catalogue and Staff
- Billing uses only active catalogue items
- Billing uses only active staff mapped to the bill's store
- Frontend-provided prices are ignored

### Billing Math
- All money in paise
- Catalogue prices are tax inclusive
- Tax rate is 5%
- Discount applies only to service subtotal
- Discount cap is 30% of service subtotal
- Tip does not affect tax
- Tip does not affect commission
- Commission base is line net amount after discount allocation
- Bill-level discount is allocated across lines proportionally using largest remainder

### Payment Rules
- `CASH`, `UPI`, `SPLIT` only
- Split means `cash + UPI` only
- For split, `cash_amount > 0` and `cash_amount < total_amount`
- Only one active pending UPI leg at a time
- Retry creates a new UPI leg for the remaining due amount only
- Old failed payment rows remain immutable for history
- Payment success is backend-confirmed only

### Bill State Rules
- Cash-only create flow ends as `PAID`
- UPI-only ends as `PAYMENT_PENDING` until confirmed, or `PAYMENT_FAILED`
- Split starts as `PARTIALLY_PAID`
- Cancellation updates state and metadata only
- Cancellation does not recompute historical totals

### Invariants
- sum of line discounts = bill discount
- sum of line net amounts = service net total
- sum of taxable bases + sum of tax amounts = service net total
- sum of tip allocations = tip amount
- amount paid + amount due = total amount

---

## Delivery Rules for Coding Agents

### Always Do
- implement the thinnest thing that satisfies the current milestone
- keep handlers small
- keep business logic in services and calculators
- keep repo methods explicit and business-oriented
- write schema migrations before or alongside repo code that depends on them
- keep response and error shapes consistent
- log important state changes and gateway outcomes
- preserve immutability for money history rows

### Never Do
- do not redesign the architecture while implementing a milestone
- do not add hidden coupling between modules
- do not call payment gateways before bill/payment rows are committed
- do not compute prices from client-supplied values
- do not mutate old payment rows to represent retries
- do not silently swallow webhook verification errors
- do not add optional infra because it “may help later”

### When Unsure
Choose the option with:
1. fewer moving parts
2. fewer hidden side effects
3. clearer transaction boundaries
4. easier reasoning for money correctness

---

## Recommended Build Order

Build in the sequence below. Do not jump ahead unless an earlier milestone is blocked by a concrete dependency.

---

# Milestone 1 — Project Skeleton and Platform Foundation

## Goal
Create the minimum backend skeleton that every later feature will rely on.

## Build
- `cmd/api/main.go`
- config loading
- PostgreSQL connection setup
- transaction helper
- HTTP router
- health endpoint
- standard JSON response helpers
- standard error model
- structured logging setup
- environment variable contract
- graceful shutdown

## Files to Create
- `cmd/api/main.go`
- `internal/platform/config/config.go`
- `internal/platform/db/postgres.go`
- `internal/platform/db/tx.go`
- `internal/platform/http/router.go`
- `internal/platform/http/response.go`
- `internal/platform/http/error.go`
- `internal/platform/logger/logger.go`
- `internal/shared/errors/codes.go`
- `internal/shared/errors/errors.go`
- `internal/shared/enums/*.go`
- `internal/shared/money/money.go`

## Required Decisions
- choose one HTTP router and keep it for the whole codebase
- choose one DB access style and keep it consistent
- define one app container / dependency wiring style
- define one error format and do not drift from it

## Deliverables
- app starts locally
- health route works
- DB connection works
- router registration pattern exists
- shared enums and error package exist

## Done Criteria
- backend boots with config validation
- health check returns success
- DB connection is established on startup
- project can be compiled without feature modules
- no business logic exists in `main.go`

---

# Milestone 2 — Database Schema, Migrations, and Seed Data

## Goal
Create the full database foundation before feature logic starts depending on it.

## Build
- SQL migrations for all v1 tables
- indexes and unique constraints
- enum strategy if using DB enums, otherwise app-level text enums with checks
- seed data for one tenant, one store, one manager, sample staff, sample catalogue, store bill counter

## Tables
- `tenants`
- `stores`
- `users`
- `staff`
- `staff_store_mapping`
- `catalogue_items`
- `store_bill_counters`
- `bills`
- `bill_items`
- `bill_tip_allocations`
- `payments`
- `commission_ledger`
- `idempotency_keys`
- `audit_logs`

## Required Constraints
- unique `(tenant_id, code)` on stores
- unique login identity on users
- unique `(staff_id, store_id)` on staff mapping
- unique `(store_id, bill_number)` on bills
- unique `(bill_id, staff_id)` on bill tip allocations
- unique `(tenant_id, store_id, idempotency_key)` on idempotency keys

## Deliverables
- migration runner wired into local workflow
- initial seed script or migration
- schema matches the LLD exactly

## Done Criteria
- empty database can be created from scratch
- seeded database can support login and bootstrap flows later
- indexes exist for the read/write paths defined in the LLD
- no unused tables are added

---

# Milestone 3 — Auth and Request Scope

## Goal
Make every request tenant-safe and store-safe before business endpoints are built.

## Build
- login endpoint
- password hash verification
- JWT issuance
- JWT validation middleware
- auth context creation
- role enforcement helpers

## Files
- `internal/auth/handler.go`
- `internal/auth/service.go`
- `internal/auth/repo.go`
- `internal/auth/middleware.go`
- `internal/auth/models.go`

## Endpoints
- `POST /auth/login`

## Core Behavior
- authenticate by `email_or_phone` + password
- return JWT with identity claims needed to derive scope
- middleware attaches `AuthContext`
- handlers do not accept trusted tenant/store IDs from clients

## Deliverables
- login for `STORE_MANAGER`
- login for `SUPER_ADMIN`
- middleware-protected routes

## Done Criteria
- invalid tokens are rejected consistently
- role checks are centralized
- request handlers can read `tenant_id`, `store_id`, `user_id`, `role` from context only
- store manager cannot access another store’s records by passing IDs manually

---

# Milestone 4 — Bootstrap Snapshot API

## Goal
Support tablet cache refresh with one clean store bootstrap API.

## Build
- bootstrap repo queries for store details, active catalogue, active staff mappings
- bootstrap service response shaping
- bootstrap handler and route

## Files
- `internal/bootstrap/handler.go`
- `internal/bootstrap/service.go`
- `internal/bootstrap/repo.go`
- `internal/bootstrap/dto.go`

## Endpoint
- `GET /store/bootstrap`

## Response Must Include
- store details
- active catalogue items
- active staff
- active staff-store mappings if needed by tablet workflow

## Rules
- store manager gets only their store snapshot
- no pagination in v1 for bootstrap
- no diff sync in v1
- data is read-only snapshot data

## Done Criteria
- endpoint returns only active data
- store scope is derived from auth, not request params
- response is stable enough to power SQLite full refresh

---

# Milestone 5 — Catalogue Admin Module

## Goal
Implement the simplest admin module for service catalogue management.

## Build
- create catalogue item
- update catalogue item
- deactivate catalogue item
- list catalogue items
- active-item fetch support for billing service

## Files
- `internal/catalogue/handler.go`
- `internal/catalogue/service.go`
- `internal/catalogue/repo.go`
- `internal/catalogue/models.go`
- `internal/catalogue/dto.go`
- `internal/catalogue/validator.go`

## Endpoints
- `GET /admin/catalogue`
- `POST /admin/catalogue`
- `PUT /admin/catalogue/{id}`
- `POST /admin/catalogue/{id}/deactivate`

## Rules
- catalogue belongs to tenant
- list price stored in paise
- list price is tax inclusive
- deactivation is soft, not destructive
- billing uses active items only

## Done Criteria
- super admin can CRUD catalogue items within tenant scope
- deactivated items do not appear in active bootstrap response
- billing repo can later fetch active items by ID without new schema changes

---

# Milestone 6 — Staff Admin Module and Store Mapping

## Goal
Implement staff management and store assignment rules needed for billing.

## Build
- create staff
- deactivate staff
- assign staff to store
- list staff
- validate active mapping for billing

## Files
- `internal/staff/handler.go`
- `internal/staff/service.go`
- `internal/staff/repo.go`
- `internal/staff/models.go`
- `internal/staff/dto.go`
- `internal/staff/validator.go`

## Endpoints
- `GET /admin/staff`
- `POST /admin/staff`
- `PUT /admin/staff/{id}` if needed for basic edits
- `POST /admin/staff/{id}/deactivate`
- `POST /admin/staff/{id}/stores/{store_id}` or equivalent mapping endpoint

## Rules
- staff is tenant-scoped
- mapping is store-scoped
- inactive staff cannot be billed
- unmapped staff cannot be billed for that store

## Done Criteria
- super admin can manage staff and mappings
- bootstrap returns active staff relevant to store
- billing module will be able to validate `assigned_staff_id` using current mappings

---

# Milestone 7 — Billing Domain Core and Calculator

## Goal
Build the core money logic before wiring the create bill endpoint.

## Build
- request DTOs
- response DTOs
- validator
- pure calculator functions
- discount allocation logic
- tax back-calculation logic
- commission calculation logic
- tip validation logic
- bill number formatting helper
- state derivation helper

## Files
- `internal/billing/models.go`
- `internal/billing/dto.go`
- `internal/billing/validator.go`
- `internal/billing/calculator.go`
- `internal/billing/mapper.go`

## Calculator Must Handle
- line gross = unit price * quantity
- service gross total
- discount cap validation
- proportional discount allocation using largest remainder
- line net amount
- taxable base = floor(line net * 100 / 105)
- tax amount = line net - taxable base
- commission = floor(line net * 10 / 100)
- tip exact-sum validation
- total amount
- amount paid / amount due by payment mode
- resulting bill state by payment mode

## Rules
- no DB calls inside calculator
- no gateway logic inside calculator
- calculator must accept authoritative input only
- calculator output must already enforce invariants

## Done Criteria
- calculator can produce a complete bill draft result from validated input and authoritative catalogue/staff data
- all invariants hold for representative cases: no discount, discount, no tip, split tip, cash, UPI, split
- code is deterministic and side-effect free

---

# Milestone 8 — Create Bill API for Cash and Shared Persistence Path

## Goal
Implement the full create-bill persistence path first, then use the same foundation for UPI and split.

## Build
- create bill handler
- create bill service
- billing repo methods
- idempotency claim/check logic
- bill number generation using `store_bill_counters` row lock
- insertion of bill header, items, tip allocations, commission ledger, payment rows
- receipt response mapping
- audit log creation for bill created

## Files
- `internal/billing/handler.go`
- `internal/billing/service.go`
- `internal/billing/repo.go`
- `internal/shared/idempotency/idempotency.go`
- `internal/audit/service.go`
- `internal/audit/repo.go`
- `internal/audit/models.go`

## Endpoint
- `POST /bills`

## First Delivery Scope Inside This Milestone
1. support `CASH` fully end-to-end first
2. keep the service path generic enough that UPI and split can reuse the same persistence path

## Required Transaction Steps
- claim/check idempotency
- load authoritative catalogue items
- validate staff mappings
- lock store bill counter row
- generate bill number
- compute totals via calculator
- insert `bills`
- insert `bill_items`
- insert `bill_tip_allocations`
- insert `commission_ledger`
- insert initial `payments`
- commit

## Rules
- create bill must be idempotent
- frontend price values are ignored if sent
- handler normalizes missing `discount_amount`, `tip_amount`, `tip_allocations`
- service owns transaction
- repo methods stay explicit

## Done Criteria
- cash-only create bill works end-to-end
- repeated request with same idempotency key does not create duplicate bill
- bill numbers are sequential per store
- receipt payload is returned from backend
- audit row exists for bill creation

---

# Milestone 9 — Payments Module and Dynamic QR Initiation

## Goal
Add UPI and split flows without contaminating billing logic.

## Build
- payment provider interface
- normalized payment DTOs/models
- Paytm adapter
- HDFC adapter
- payment initiation service
- payment repo updates for QR metadata and payment status
- UPI and split create-bill post-commit flow
- audit rows for payment initiated / payment failed

## Files
- `internal/payments/handler.go`
- `internal/payments/service.go`
- `internal/payments/repo.go`
- `internal/payments/models.go`
- `internal/payments/dto.go`
- `internal/payments/webhook.go`
- `internal/payments/mapper.go`
- `internal/payments/providers/provider.go`
- `internal/payments/providers/paytm.go`
- `internal/payments/providers/hdfc.go`

## Provider Interface
Support:
- create dynamic QR
- get payment status
- verify webhook
- parse webhook

## Create Bill Behavior Added in This Milestone
- UPI-only bill:
  - persist bill + payment rows in transaction
  - commit
  - call selected gateway
  - update payment row to `PENDING` with gateway refs and QR payload
- Split bill:
  - persist cash leg + UPI leg in transaction
  - commit
  - call gateway for UPI leg only
  - bill remains `PARTIALLY_PAID`

## Failure Rules
- if QR creation fails:
  - payment row becomes `FAILED`
  - UPI-only bill becomes `PAYMENT_FAILED`
  - split bill remains `PARTIALLY_PAID`

## Done Criteria
- create bill supports `CASH`, `UPI`, `SPLIT`
- gateway call happens only after commit
- payment row stores gateway metadata cleanly
- billing module knows nothing about gateway-specific request/response details

---

# Milestone 10 — Bill Read, Cancel, and Retry UPI

## Goal
Complete the store-facing bill lifecycle APIs.

## Build
- get bill endpoint
- cancel bill endpoint
- retry-upi endpoint
- retry idempotency path
- payment-leg response mapping
- audit rows for bill cancelled and payment retry initiated

## Endpoints
- `GET /bills/{bill_id}`
- `POST /bills/{bill_id}/cancel`
- `POST /bills/{bill_id}/payments/retry-upi`

## Get Bill Must Return
- bill header
- bill items
- tip allocations
- payment legs
- active UPI payload if pending
- receipt payload

## Cancel Rules
- reason required
- bill must be in cancellable state
- cancellation updates status and metadata only
- historical amounts do not change

## Retry Rules
- bill must belong to current store
- amount due must be greater than zero
- no active pending UPI leg may already exist
- new payment row is inserted with `INITIATED`
- gateway call occurs after commit
- old failed rows remain unchanged

## Done Criteria
- store manager can read current bill state cleanly
- cancellation is immediate and auditable
- retry creates exactly one new UPI leg for outstanding due amount
- duplicate retry request with same idempotency key does not duplicate payment rows

---

# Milestone 11 — Webhooks and Status Fallback

## Goal
Make payment confirmation reliable and backend-authoritative.

## Build
- `POST /webhooks/paytm`
- `POST /webhooks/hdfc`
- webhook verification and parsing
- normalized webhook event mapping
- payment row lookup by gateway identifiers
- lock payment row and related bill row
- recompute bill `amount_paid`, `amount_due`, `status`
- persist verified gateway payloads
- optional status fallback trigger from `GET /bills/{bill_id}` when payment is stale/pending
- audit rows for payment success / failure

## Rules
- verification failure returns failure response and logs securely
- duplicate webhooks must be safe
- updates must be idempotent at payment row level
- webhook path owns settlement updates, not the tablet client
- status fallback is a read-triggered safety path, not the primary path

## Required Transaction Steps
- find target payment row
- lock payment row
- lock related bill row
- update payment status
- recompute bill amounts and state
- save verified metadata
- commit

## Done Criteria
- successful payment settles bill correctly
- delayed webhook can be recovered through status fallback
- duplicate or reordered provider callbacks do not corrupt bill totals
- payment truth is backend-confirmed only

---

# Milestone 12 — Analytics and Admin Read APIs

## Goal
Expose the minimum admin visibility required for operations.

## Build
- bill list query
- filters by store, date range, status where applicable
- analytics summary query
- cancelled bill visibility

## Files
- `internal/analytics/handler.go`
- `internal/analytics/service.go`
- `internal/analytics/repo.go`
- `internal/analytics/dto.go`

## Endpoints
- `GET /admin/bills`
- `GET /admin/analytics/summary`

## Outputs
### Bill list
- bill id
- bill number
- created at
- store id / name
- status
- total amount
- amount paid
- amount due
- payment mode summary
- cancellation reason if cancelled

### Summary
- total bills
- total sales
- cancelled bill count
- cancelled amount
- total tax
- total commission
- total tip

## Rules
- analytics is read-only
- query from PostgreSQL directly
- no reporting DB
- do not build fancy charts or pre-aggregations in backend v1

## Done Criteria
- super admin can view bill list and summary totals
- cancelled bills and reasons are visible
- queries use practical indexes and do not require schema redesign

---

# Milestone 13 — Hardening, Validation Pass, and Delivery Readiness

## Goal
Make the system safe to ship without expanding scope.

## Build
- final validation review for all request DTOs
- final authorization review for all endpoints
- final transaction boundary review
- structured logs for key money and payment events
- DB timeout / context usage review
- gateway timeout and retry policy review at adapter level
- production-safe config validation
- seed cleanup for non-production environments
- API examples / collection for store app and admin app teams

## Review Checklist
- every money value is paise
- every write path is tenant-safe and store-safe
- every gateway call happens after commit
- idempotency exists where required
- payment rows remain historical and immutable enough for audit
- cancellation preserves historical totals
- no unused abstractions slipped in
- no module reaches across boundaries improperly

## Done Criteria
- backend is deployable as one service with one PostgreSQL database
- all v1 endpoints are wired and protected
- logs are enough to debug billing and payment issues
- the codebase is still small, obvious, and maintainable

---

## Endpoint Map

### Auth
- `POST /auth/login`

### Store
- `GET /store/bootstrap`
- `POST /bills`
- `GET /bills/{bill_id}`
- `POST /bills/{bill_id}/cancel`
- `POST /bills/{bill_id}/payments/retry-upi`

### Admin
- `GET /admin/catalogue`
- `POST /admin/catalogue`
- `PUT /admin/catalogue/{id}`
- `POST /admin/catalogue/{id}/deactivate`
- `GET /admin/staff`
- `POST /admin/staff`
- `PUT /admin/staff/{id}` if basic edits are needed
- `POST /admin/staff/{id}/deactivate`
- mapping endpoint for staff to store
- `GET /admin/bills`
- `GET /admin/analytics/summary`

### Webhooks
- `POST /webhooks/paytm`
- `POST /webhooks/hdfc`

---

## Implementation Notes by Module

### Auth
Keep it boring. Login, verify, issue JWT, attach context. No session store required in v1.

### Bootstrap
Read-only module. No write logic.

### Catalogue
Use soft deactivation. Do not hard-delete catalogue rows used by historical bills.

### Staff
Use soft deactivation. Keep mapping validation explicit.

### Billing
This is the core module. Keep calculator pure. Keep service authoritative. Keep repo explicit.

### Payments
Payment adapter boundary must stay clean. Normalize provider outputs immediately.

### Analytics
Read-only. Avoid premature aggregation systems.

### Audit
Minimal but reliable. Record only meaningful actions.

---

## What Good Code Looks Like Here

Good code in this backend has these properties:
- one obvious place for each piece of logic
- a handler that mostly parses input and returns output
- a service that owns the transaction and business rules
- a repo that performs explicit persistence operations
- a calculator that is deterministic and side-effect free
- a payment provider adapter that hides gateway-specific details
- no speculative architecture

---

## What Bad Code Looks Like Here

Avoid these patterns:
- billing math inside handlers
- gateway API calls inside billing repo or DB transaction
- duplicated tax or commission logic across modules
- generic “base service” or “base repository” layers
- one file doing auth, billing, and payment together
- leaking provider-specific fields across the whole codebase
- adding async systems to avoid writing clear synchronous flow
- over-configurable workflows that the product does not yet need

---

## Final Rule

When choosing between two implementations, prefer the one that:
- makes money flow easier to reason about
- makes failure handling more explicit
- reduces module coupling
- removes future cleanup work
- is easier for the next engineer to read in one pass

-------

Code Review rules - 

Check cross-module boundaries, not just milestone-local correctness.

Treat every extra field, helper, layer, or dependency as suspicious unless explicitly justified by HLD, LLD, or agents.md.

Always audit for hidden coupling, inconsistent patterns, and configuration bleed between binaries/modules.

Require one clear ownership path for routing, logging, auth, and role enforcement; no ad hoc duplicates.

Review against all docs together and flag anything that cannot be directly defended.
