# Salon Billing Software HLD — Revised HDFC Payment Design

This document replaces the payment-related parts of the HLD and aligns the product design to the current implementation direction.

It is intentionally narrow.

It follows these rules:
- keep the operational flow simple for store staff
- keep backend as the money and state authority
- keep HDFC integration guide as the source of truth for the HDFC payment flow
- avoid speculative multi-gateway design while only HDFC is being implemented
- keep bill cancellation separate from provider-side payment-attempt cancellation

---

# 1. Overview

This design describes a multi-tenant salon billing product with:

- Android tablet app for store billing operations
- web admin app for salon administration
- Go backend as a modular monolith
- PostgreSQL as the system of record
- SQLite in the Android app as a local read cache for catalogue and staff snapshots
- HDFC terminal-backed online payment integration
- Android native thermal printer integration for receipt printing

The current payment implementation scope is HDFC-only.

Paytm is deferred and is not part of the active payment module.

The product is designed for operational simplicity, transactional correctness, and fast implementation.

---

# 2. Business Scope

## 2.1 Store App Scope

The store app is used only by a store manager and supports:

- create bill
- select services from catalogue
- assign staff per service line
- apply one bill-level discount
- enter total tip
- split tip across staff
- choose payment method:
  - cash
  - online
  - split cash + online
- send online payment request to HDFC terminal
- wait for terminal-owned payment completion
- retry failed online remainder for split bills
- cancel provider-side payment attempt when allowed
- print bill using native Android thermal printer integration
- cancel bill with mandatory reason after normal bill-completion rules are satisfied
- add and deactivate staff

## 2.2 Admin App Scope

The admin app is a web application and supports:

- manage catalogue
- manage staff
- view bills
- view simple analytics
- view cancelled bill details and reasons

## 2.3 Explicitly Out of Scope for Current Payment Implementation

- Paytm implementation
- backend-generated QR for HDFC
- payment webhooks
- HDFC Void Sale
- Bank EMI
- Brand EMI
- Cash@Pos
- UPI Collect
- terminal tip features
- customer profiles
- loyalty programs
- appointment scheduling
- inventory
- payroll
- advanced refund workflows
- store-specific catalogue pricing
- multi-user billing workflows inside a store

---

# 3. Scale Assumptions

The system is designed for approximately:

- multiple salon tenants
- each tenant having multiple stores
- total of about 200 stores across all tenants
- each store creating about 1000 bills per day

This remains comfortably within the range of a well-designed modular monolith with PostgreSQL.

---

# 4. Architecture Decisions

## 4.1 Overall Architecture

Use a modular monolith.

Why:
- strong consistency for billing and payments
- simpler deployment and operations
- easier transactional boundaries
- lower infrastructure cost
- faster implementation than microservices

## 4.2 Technology Choices

- **Backend:** Go
- **Store App:** Flutter Android tablet app
- **Admin App:** Web application
- **Primary Database:** PostgreSQL
- **Local Mobile Cache:** SQLite
- **Online Payment Integration:** HDFC BonusHub terminal integration
- **Printing:** Android native thermal printer integration

## 4.3 Client Split

- **Store operations:** Android tablet app only
- **Admin operations:** web app only

This split is intentional:
- the store app needs optimized touch UX and hardware printer access
- the admin app is CRUD- and table-heavy, which is faster to build and easier to manage on the web

---

# 5. Multi-Tenant Model

The product uses this hierarchy:

**Tenant -> Store -> Staff / Catalogue / Bills**

All operational records are scoped to:
- one tenant
- one store where applicable

The backend derives tenant, store, and user context from authentication/session.

The frontend never sends trusted scope values as business truth.

For HDFC online payments, terminal configuration is also store-scoped.

---

# 6. Roles and Access Model

## 6.1 Roles

- `SUPER_ADMIN`
- `STORE_MANAGER`

## 6.2 Access Rules

- the store app uses `STORE_MANAGER`
- the admin app uses `SUPER_ADMIN`
- backend derives `tenant_id`, `store_id`, and `user_id` from auth/session
- the client does not manually provide trusted scope values

---

# 7. Functional Modules in the Monolith

## 7.1 Auth and Session
Responsibilities:
- authenticate users
- derive tenant/store/user scope
- authorize store or admin actions

## 7.2 Catalogue
Responsibilities:
- create services
- update services
- activate/deactivate services

## 7.3 Staff
Responsibilities:
- add staff
- deactivate staff
- assign staff to store

## 7.4 Billing
Responsibilities:
- create bill
- validate discount and tip split
- fetch live catalogue prices
- assign staff per service line
- compute tax and commission
- maintain bill state
- cancel bill with reason

## 7.5 Payments
Responsibilities:
- initiate HDFC Sale request for online payment attempts
- maintain payment rows per payment leg
- synchronize HDFC transaction status using Status API
- support retry for remaining online due amount
- support provider-side payment-attempt cancellation using HDFC Cancel Sale API
- store actual completion mode returned by HDFC

The payment module does **not**:
- generate HDFC QR
- use webhooks
- implement Paytm
- use HDFC Void Sale

## 7.6 Analytics and Bills View
Responsibilities:
- list bills
- show statuses and totals
- show cancellations and reasons
- provide simple aggregated analytics

## 7.7 Audit and Idempotency
Responsibilities:
- deduplicate create-bill requests
- deduplicate retry-online requests
- record important actions and state changes

---

# 8. Core Business Rules

## 8.1 Pricing and Tax

- catalogue prices are **tax inclusive**
- tax rate is **5%**
- tax is back-calculated from the post-discount service amount

## 8.2 Discount

- one bill-level discount amount is supported
- discount applies to services only
- discount does not apply to tip
- backend validates:
  - discount >= 0
  - discount <= 30% of service subtotal

## 8.3 Tip

- tip is non-taxable
- tip is entered as a single bill-level amount
- tip is split across staff and stored separately
- tip split must exactly equal the entered tip amount

## 8.4 Commission

- commission is fixed at 10%
- commission is calculated per service line
- commission base is the line net amount after discount allocation
- tip is not part of commission

## 8.5 Payment Modes

Current store-facing payment modes are:
- `CASH`
- `ONLINE`
- `SPLIT`

Rules:
- `ONLINE` is the store-facing label for digital payment through HDFC terminal
- `SPLIT` means cash + online
- manager enters cash amount for split
- system computes remaining online amount
- manager can retry only the remaining online amount if it fails
- backend stores actual completion mode returned by HDFC even if cashier selected ONLINE

---

# 9. Bill and Payment State Model

## 9.1 Bill Statuses

- `DRAFT`
- `PAYMENT_PENDING`
- `PARTIALLY_PAID`
- `PAID`
- `PAYMENT_FAILED`
- `CANCELLED`

## 9.2 Bill Status Meaning

- `DRAFT`: bill structure created
- `PAYMENT_PENDING`: online-only bill waiting for confirmed settlement
- `PARTIALLY_PAID`: some money collected, remainder still due
- `PAID`: full amount settled
- `PAYMENT_FAILED`: digital attempt failed and no amount settled
- `CANCELLED`: bill cancelled by store manager with mandatory reason

## 9.3 Payment Statuses

- `INITIATED`
- `PENDING`
- `SUCCESS`
- `FAILED`
- `CANCELLED`

## 9.4 HDFC Confirmation Rules

- HDFC Status API is the confirmation mechanism
- webhook-first confirmation is not used
- backend remains the only authority for payment success
- tablet refreshes bill state via `GET /bills/{bill_id}`

---

# 10. Payment Experience Design

## 10.1 HDFC Terminal-Owned Flow

HDFC terminal owns the customer-facing payment interaction.

That means:
- backend sends sale JSON to HDFC
- terminal drives the customer interaction
- terminal owns QR generation or any on-device payment presentation
- backend does not render HDFC QR payload on the tablet

## 10.2 Online-Only Bill Flow

High-level flow:
1. manager creates bill and selects ONLINE
2. backend persists bill and payment leg
3. backend sends HDFC Sale request
4. tablet shows waiting state for terminal-driven payment
5. backend confirms payment later using HDFC Status API
6. bill settles to final state

## 10.3 Split Bill Flow

High-level flow:
1. manager enters cash amount
2. backend computes remaining online due
3. backend persists both payment legs
4. backend sends HDFC Sale request for remaining due
5. tablet shows split state with active online attempt
6. backend confirms settlement through Status API

## 10.4 Mobile Number

Customer mobile number is not part of the current store payment UX for HDFC.

No customer capture screen is added for this payment implementation.

## 10.5 Actual Completion Mode

Even when the cashier chooses ONLINE, the system stores the actual completion mode returned by HDFC for audit, diagnostics, and reconciliation.

---

# 11. Cancellation Model

This design uses two separate cancellation concepts.

## 11.1 Bill Cancellation

Bill cancellation means:
- the salon bill itself is cancelled
- cancellation reason is mandatory
- bill cancellation updates bill status and cancellation metadata
- historical totals do not change

Bill cancellation is not the same as cancelling an HDFC payment attempt.

## 11.2 Provider-Side Payment Attempt Cancellation

Provider-side payment-attempt cancellation means:
- cancel the active HDFC sale attempt at provider/terminal flow level
- this uses HDFC Cancel Sale API
- this updates payment state and bill payment aggregates
- this does not write bill cancellation metadata

## 11.3 Rule Between the Two

A bill with an active pending HDFC payment attempt must not be bill-cancelled until the payment attempt is resolved or provider-cancelled.

---

# 12. Bill Numbering Strategy

Use two identifiers:
- **Internal ID:** UUID
- **Human bill number:** store-specific sequential number

Recommended format:

`STORECODE-YYYYMMDD-000123`

Why:
- avoids global sequence bottlenecks
- is operationally meaningful at store level
- simplifies reconciliation and receipt usage

---

# 13. Data Model Overview — Payment-Relevant Changes

Core tables remain:
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

Payment-related additions:
- store-level HDFC terminal TID
- payment-level HDFC provider request and status fields
- payment-level actual completion mode
- payment-level provider response payload persistence

The `payments` table remains one row per payment leg.

Examples:
- cash-only bill -> one cash row
- online-only bill -> one HDFC-backed online row
- split bill -> one cash row + one HDFC-backed online row

---

# 14. Local SQLite Cache Strategy

SQLite remains a read cache in the Android tablet app only.

For payment design, important rule remains:
- SQLite is never the source of truth for bill creation or payment confirmation
- PostgreSQL plus backend-controlled provider sync remain authoritative

---

# 15. API Surface — Revised Payment Endpoints

## 15.1 Store APIs

- `GET /store/bootstrap`
- `POST /bills`
- `GET /bills/{bill_id}`
- `POST /bills/{bill_id}/cancel`
- `POST /bills/{bill_id}/payments/retry-online`
- `POST /bills/{bill_id}/payments/{payment_id}/cancel-attempt`

## 15.2 Admin APIs

- catalogue list/create/update endpoints
- staff list/create/update/deactivate endpoints
- bill list endpoint
- analytics summary endpoint

## 15.3 HDFC Confirmation Model

- no webhook endpoint in current payment scope
- HDFC payment confirmation happens through Status API
- tablet reads latest state through bill-read API

---

# 16. High-Level Bill Creation Flow — Revised

1. manager selects services and staff in tablet app
2. manager selects:
   - cash
   - online
   - or split
3. backend derives tenant/store/user scope
4. backend validates catalogue items and staff assignments
5. backend computes totals, discount allocation, tax, commission, and tip validation
6. backend creates bill, bill items, tip allocations, commission rows, and payment rows
7. if cash-only, bill is marked paid immediately
8. if online or split, backend initiates HDFC Sale after DB commit
9. tablet shows terminal-driven payment-in-progress state
10. backend confirms payment using HDFC Status API
11. tablet refreshes via `GET /bills/{bill_id}`
12. receipt is printed through Android native thermal printer integration

---

# 17. High-Level Provider-Side Payment Attempt Cancellation Flow

1. manager opens bill details
2. manager sees active pending online payment attempt
3. manager triggers payment-attempt cancellation
4. backend validates current payment state
5. backend calls HDFC Cancel Sale API
6. backend updates payment leg and bill payment aggregates
7. tablet shows updated bill/payment state

---

# 18. High-Level Bill Cancellation Flow

1. manager opens bill details
2. manager enters mandatory cancellation reason
3. backend validates scope and bill state
4. backend ensures no active pending HDFC payment attempt remains
5. bill is marked `CANCELLED`
6. cancellation metadata is stored
7. admin app highlights cancelled bill with bill details and reason

Any operational refund handling remains outside system scope.

---

# 19. Reliability and Safety Decisions

## 19.1 Idempotency

Required for:
- create bill
- retry online payment

## 19.2 Audit Logging

Audit key actions:
- bill created
- bill cancelled
- online payment initiated
- online payment success/failure/cancelled
- online payment retry
- provider-side payment-attempt cancellation
- staff changes
- catalogue changes

## 19.3 Transaction Boundaries

Design rule:
- persist bill and payment rows inside DB transaction
- call HDFC only after DB commit
- do not hold DB transactions open around network calls

## 19.4 Backend Validation Authority

Backend must always validate:
- catalogue prices
- staff assignment
- discount rules
- tip split totals
- split payment rules
- store HDFC terminal configuration before online initiation

---

# 20. Final Recommended System Shape

## Store Side
- Flutter Android tablet app
- SQLite read cache
- Android native thermal printer integration
- HDFC terminal-driven online payment experience

## Admin Side
- web app for catalogue, staff, bills, analytics

## Backend
- Go modular monolith
- PostgreSQL database
- HDFC-specific payment module using:
  - Sale API
  - Status API
  - Cancel Sale API
- no webhook dependency
- no Paytm implementation in current scope

This is the smallest clean design that fits the current HDFC integration direction while preserving transactional correctness and keeping blast radius low.
