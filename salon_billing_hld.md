# Salon Billing Software HLD

## 1. Overview

This document describes the high-level design for a multi-tenant salon billing product with:

- Android tablet app for store billing operations
- Web admin app for salon administration
- Go backend as a modular monolith
- PostgreSQL as the system of record
- SQLite in the Android app as a local read cache for catalogue and staff snapshots
- Paytm and HDFC UPI integrations
- Android native thermal printer integration for receipt printing

The product is designed for operational simplicity, transactional correctness, and fast implementation.

---

## 2. Business Scope

### 2.1 Store App Scope

The store app is used only by a store manager and supports:

- create bill
- select services from catalogue
- assign staff per service line
- apply one bill-level discount
- enter total tip
- split tip across staff
- choose payment method: cash, UPI, or split cash + UPI
- generate and display dynamic QR for UPI
- retry failed UPI remainder for split bills
- print bill using native Android thermal printer integration
- cancel bill with mandatory reason
- add and deactivate staff

### 2.2 Admin App Scope

The admin app is a web application and supports:

- manage catalogue
- manage staff
- view bills
- view simple analytics
- view cancelled bill details and reasons

### 2.3 Explicitly Out of Scope for v1

- offline billing
- customer profiles
- loyalty programs
- appointment scheduling
- inventory
- payroll
- advanced refund workflows
- store-specific catalogue pricing
- multi-user billing workflows inside a store

---

## 3. Scale Assumptions

The system is designed for approximately:

- multiple salon tenants
- each tenant having multiple stores
- total of about 200 stores across all tenants
- each store creating about 1000 bills per day

This is comfortably within the range of a well-designed modular monolith with PostgreSQL.

---

## 4. Architecture Decisions

### 4.1 Overall Architecture

Use a modular monolith.

**Why:**

- strong consistency for billing and payments
- simpler deployment and operations
- easier transactional boundaries
- lower infrastructure cost
- faster implementation than microservices

### 4.2 Technology Choices

- **Backend:** Go
- **Store App:** Flutter Android tablet app
- **Admin App:** Web application
- **Primary Database:** PostgreSQL
- **Local Mobile Cache:** SQLite
- **Payment Gateways:** Paytm and HDFC
- **Printing:** Android native thermal printer integration

### 4.3 Client Split

- **Store operations:** Android tablet app only
- **Admin operations:** Web app only

This split is intentional:

- the store app needs optimized touch UX and hardware printer access
- the admin app is CRUD- and table-heavy, which is faster to build and easier to manage on the web

---

## 5. Multi-Tenant Model

The product uses the following hierarchy:

**Tenant -> Store -> Staff / Catalogue / Bills**

### 5.1 Tenant
Represents one salon client.

### 5.2 Store
Represents one salon branch/location belonging to a tenant.

All operational records are scoped to:

- one tenant
- one store where applicable

The backend derives tenant, store, and user context from authentication/session. The frontend never sends trusted scope fields as business input.

---

## 6. Roles and Access Model

### 6.1 Roles

- `SUPER_ADMIN`
- `STORE_MANAGER`

### 6.2 Access Rules

- The store app uses `STORE_MANAGER`
- The admin app uses `SUPER_ADMIN`
- Backend derives `tenant_id`, `store_id`, and `user_id` from auth/session
- The client does not manually provide trusted scope values

---

## 7. Functional Modules in the Monolith

### 7.1 Auth and Session
Responsibilities:

- authenticate users
- derive tenant/store/user scope
- authorize store or admin actions

### 7.2 Catalogue
Responsibilities:

- create services
- update services
- activate/deactivate services

### 7.3 Staff
Responsibilities:

- add staff
- deactivate staff
- assign staff to store

### 7.4 Billing
Responsibilities:

- create bill
- validate discount and tip split
- fetch live catalogue prices
- assign staff per service line
- compute tax and commission
- maintain bill state
- cancel bill with reason

### 7.5 Payments
Responsibilities:

- create UPI payment requests with Paytm/HDFC
- maintain payment rows per payment leg
- verify payment success via backend
- support UPI retry for outstanding remainder
- process payment webhooks

### 7.6 Analytics and Bills View
Responsibilities:

- list bills
- show statuses and totals
- show cancellations and reasons
- provide simple aggregated analytics

### 7.7 Audit and Idempotency
Responsibilities:

- deduplicate create-bill requests
- deduplicate retry-upi requests
- record important actions and state changes

---

## 8. Core Business Rules

### 8.1 Pricing and Tax

- Catalogue prices are **tax inclusive**
- Tax rate is **5%**
- Tax is back-calculated from the post-discount service amount

### 8.2 Discount

- One bill-level discount amount is supported
- Discount applies to services only
- Discount does not apply to tip
- Discount can be arbitrary, but backend validates:
  - discount >= 0
  - discount <= 30% of service subtotal

### 8.3 Tip

- Tip is non-taxable
- Tip is entered as a single bill-level amount
- Tip is split across staff and stored separately
- Tip split must exactly equal the entered tip amount

### 8.4 Commission

- Commission is fixed at 10%
- Commission is calculated per service line
- Commission base is the line net amount after discount allocation
- Tip is not part of commission

### 8.5 Payment Modes

v1 supports:

- `CASH`
- `UPI`
- `SPLIT` (cash + UPI)

For split payment:

- manager enters cash amount
- system computes remaining UPI amount
- manager can retry only the UPI remainder if it fails

---

## 9. Bill and Payment State Model

### 9.1 Bill Statuses

- `DRAFT`
- `PAYMENT_PENDING`
- `PARTIALLY_PAID`
- `PAID`
- `PAYMENT_FAILED`
- `CANCELLED`

### 9.2 Bill Status Meaning

- `DRAFT`: bill created, structure persisted
- `PAYMENT_PENDING`: UPI-only bill waiting for settlement
- `PARTIALLY_PAID`: some money collected, remainder still due
- `PAID`: full amount settled
- `PAYMENT_FAILED`: digital attempt failed and no amount settled
- `CANCELLED`: bill cancelled by store manager with mandatory reason

### 9.3 Payment Statuses

- `INITIATED`
- `PENDING`
- `SUCCESS`
- `FAILED`
- `CANCELLED`

### 9.4 State Rules

- Cash-only bill: `DRAFT -> PAID`
- UPI-only bill: `DRAFT -> PAYMENT_PENDING -> PAID` or `PAYMENT_FAILED`
- Split bill: `DRAFT -> PARTIALLY_PAID -> PAID`
- If split UPI remainder fails, bill remains `PARTIALLY_PAID`
- Manager can only retry UPI for remaining due amount

### 9.5 Cancellation Rules

- Cancellation happens immediately at store level
- Cancellation reason is mandatory
- Admin app only highlights the cancelled bill and reason
- For digitally paid or split-paid bills, refund handling happens offline by the store manager and is treated as done from the system perspective

---

## 10. Bill Numbering Strategy

Use two identifiers:

- **Internal ID:** UUID
- **Human bill number:** store-specific sequential number

Recommended format:

`STORECODE-YYYYMMDD-000123`

### Why store-specific numbering

- avoids global sequence bottlenecks
- is operationally meaningful at store level
- simplifies reconciliation and printed receipt usage

---

## 11. Data Model Overview

### 11.1 Core Tables

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

### 11.2 Main Table Intent

#### `tenants`
Salon client records.

#### `stores`
Branch records for each tenant.

#### `users`
Authenticated system users.

#### `staff`
Staff members performing services.

#### `staff_store_mapping`
Mapping of staff to store.

#### `catalogue_items`
Tenant-level service catalogue with tax-inclusive list price.

#### `store_bill_counters`
Counter table used to generate store-specific human bill numbers.

#### `bills`
Bill header storing totals, status, amount paid/due, and cancellation details.

#### `bill_items`
Immutable bill line snapshots including discount allocation, tax, staff assignment, and commission amounts.

#### `bill_tip_allocations`
Tip split rows per staff.

#### `payments`
One row per payment leg.

Examples:
- cash-only bill -> one cash row
- UPI-only bill -> one UPI row
- split bill -> one cash row + one UPI row

#### `commission_ledger`
Immutable per-line commission entries.

#### `idempotency_keys`
Deduplication for create bill and retry UPI requests.

#### `audit_logs`
Minimal audit trail for key system actions.

---

## 12. Local SQLite Cache Strategy

SQLite exists in the Android tablet app only as a **read cache**.

### 12.1 Cached Data

- active catalogue snapshot
- active staff snapshot
- active staff-store mappings
- optional recent lightweight receipt summaries if needed for local UI convenience

### 12.2 Not Stored as Truth

- authoritative billing state
- authoritative payment state
- transactional billing workflow state

### 12.3 Refresh Strategy

- full refresh on app launch
- full refresh once every day
- no versioning in v1
- replace local snapshot from backend bootstrap API

### 12.4 Important Rule

SQLite is never used as the source of truth for bill creation. The backend always validates against PostgreSQL.

---

## 13. Store App UX Design

### 13.1 Device Target

- Android tablet
- touch-first design
- landscape-oriented optimized layout
- thermal printer support through Android native integration

### 13.2 Primary Store Workflow

Three-step bill flow:

1. **Create Bill**
2. **Select Services and Staff**
3. **Confirm, Pay, and Print**

### 13.3 Confirm Screen Design

Use a single screen with two areas:

- **Left side:** bill items list
- **Right side:** totals, discount, tip, payment section, and print/payment controls

### 13.4 Tip Split UX

No new screen is needed.

Use an inline expandable tip split section in the confirm panel:

- if only one unique staff exists in the bill, tip auto-assigns to that staff
- if multiple staff exist, default split can be auto-generated
- manager can switch to custom split and edit amounts inline
- tip split is shown per unique staff, not per bill line

### 13.5 Split Payment UX

For split payment:

- manager enters cash amount
- remaining UPI amount is auto-calculated and shown
- UPI QR is displayed for the remainder

### 13.6 Staff Management UX

Store app also includes a simple staff management area to:

- add staff
- deactivate staff

---

## 14. Admin Web App Design

The admin interface is a web app because it is quicker to build and easier to manage for:

- CRUD forms
- table-based lists
- filters
- analytics panels
- cross-device browser access

### 14.1 Admin Features

- catalogue management
- staff management
- bill list with filters
- cancelled bill visibility
- simple analytics dashboard

### 14.2 Analytics Scope

Keep analytics simple in v1:

- bill counts
- total sales
- cancelled bills
- tax totals
- commission totals
- tip totals

No separate warehouse or BI stack is needed in v1.

---

## 15. API Surface

Keep the API surface intentionally small.

### 15.1 Store APIs

- `GET /store/bootstrap`
- `POST /bills`
- `GET /bills/{bill_id}`
- `POST /bills/{bill_id}/cancel`
- `POST /bills/{bill_id}/payments/retry-upi`

### 15.2 Admin APIs

- catalogue list/create/update endpoints
- staff list/create/update/deactivate endpoints
- bill list endpoint
- analytics summary endpoint

### 15.3 Payment Webhooks

- `POST /webhooks/paytm`
- `POST /webhooks/hdfc`

### 15.4 Payment Confirmation Model

- webhook-first confirmation model
- backend is the only authority for payment success
- tablet uses `GET /bills/{bill_id}` for payment refresh and polling

---

## 16. Create Bill API Design Summary

### 16.1 Request Principles

The create bill API request includes:

- `client_bill_ref`
- service items with staff assignments
- one bill-level discount amount
- bill-level tip amount
- tip allocations by staff
- payment mode details
- idempotency key

The frontend never sends trusted pricing or scope as authoritative values.

### 16.2 Response Principles

The create bill response includes:

- full bill header
- computed bill items
- tip allocations
- all payment legs
- active UPI payload if relevant
- print-friendly receipt payload

---

## 17. Payment Gateway Integration Strategy

### 17.1 Supported Gateways

- Paytm
- HDFC

### 17.2 Integration Pattern

Implement a payment adapter layer in the backend so billing logic is isolated from gateway-specific details.

Responsibilities of the payment module:

- create payment order / QR
- store gateway request/response data
- verify payment status
- normalize webhook handling

### 17.3 Retry Policy

- only one active pending UPI leg allowed at a time
- retry creates a new UPI payment row
- old failed UPI rows remain for history
- manager can retry UPI only

---

## 18. Printing Strategy

Use Android native thermal printer integration.

### 18.1 Why Native Integration

- best compatibility with Android-connected thermal printers
- lower friction for in-store printing
- more reliable than generic browser printing from tablet workflow

### 18.2 Printing Model

- backend returns a print-friendly receipt payload
- store app formats receipt for thermal printer
- no server-side PDF generation is required in v1 unless a printer integration constraint forces it later

---

## 19. Reliability and Safety Decisions

### 19.1 Idempotency

Required for:

- create bill
- retry UPI

### 19.2 Audit Logging

Audit key actions:

- bill created
- bill cancelled
- payment success/failure updates
- staff changes
- catalogue changes

### 19.3 Transaction Boundaries

Important design rule:

- persist bill and payment rows inside DB transaction
- call external payment gateway only after DB commit

Do not hold a DB transaction open around gateway network calls.

### 19.4 Backend Validation Authority

Backend must always validate:

- catalogue prices
- staff assignment
- discount rules
- tip split totals
- payment split rules

---

## 20. High-Level Bill Creation Flow

1. Manager selects services and staff in tablet app
2. Tablet app sends create bill request with item IDs, tip split, discount, and payment mode
3. Backend derives tenant/store/user scope from auth/session
4. Backend validates catalogue items and staff assignments from PostgreSQL
5. Backend computes service totals, discount allocation, tax, commission, tip validation, and final bill amount
6. Backend creates bill, bill items, tip allocation rows, commission rows, and payment rows
7. If cash-only, bill is marked paid immediately
8. If UPI or split, backend initiates payment with selected gateway after DB commit
9. Tablet displays QR if needed
10. Payment success is confirmed by backend using webhook-first processing
11. Tablet refreshes via `GET /bills/{bill_id}`
12. Receipt is printed through Android native thermal printer integration

---

## 21. High-Level Cancellation Flow

1. Manager opens bill details
2. Manager enters mandatory cancellation reason
3. Backend validates scope and bill state
4. Bill is marked `CANCELLED`
5. Cancellation metadata is stored
6. Admin web app highlights cancelled bill with bill details and reason
7. Any refund handling, if required operationally, is performed offline outside the system

---

## 22. Deployment Model

### 22.1 v1 Deployment

- one Go backend deployment
- one PostgreSQL database
- one Flutter Android tablet app build for stores
- one admin web app deployment

### 22.2 Infrastructure Philosophy

Keep v1 simple:

- no microservices
- no event bus
- no warehouse
- Redis optional, not foundational

This system should be optimized for correctness and speed of delivery, not premature distribution.

---

## 23. Key Design Principles

1. **Money logic stays on the backend**
2. **Frontend remains thin and operationally simple**
3. **PostgreSQL is the only source of truth**
4. **SQLite is only a local read cache**
5. **Payment success is backend-confirmed only**
6. **Store-level UX must stay extremely simple**
7. **History should remain immutable where money is involved**
8. **Keep v1 narrow and operationally useful**

---

## 24. Final Recommended System Shape

### Store Side
- Flutter Android tablet app
- SQLite read cache
- Android native thermal printer integration

### Admin Side
- Web app for catalogue, staff, bills, analytics

### Backend
- Go modular monolith
- PostgreSQL database
- Paytm and HDFC adapters
- webhook-first payment confirmation

This design is the right tradeoff for the stated scale, operational simplicity, and implementation speed.
