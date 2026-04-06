# Salon Billing Backend LLD

This document contains only the backend Low Level Design for the salon billing system.

It is written with these priorities:

- simplicity
- quick delivery
- easy maintenance
- clear ownership
- minimal unnecessary abstraction

It intentionally excludes:
- Android app LLD
- admin web app LLD
- printer integration client-side details
- non-essential optional sections

---

# 1. Backend Scope

The backend covers:

- authentication and session scope
- store bootstrap for local cache refresh
- catalogue management
- staff management
- bill creation
- bill retrieval
- bill cancellation
- UPI payment initiation
- UPI retry
- payment webhook handling
- payment status fallback checking
- bill list and summary analytics
- audit logging
- database persistence and transaction handling

---

# 2. Locked Design Decisions

1. Backend is a **Go modular monolith**
2. Database is **PostgreSQL**
3. Auth is **JWT-based** for both store manager and super admin
4. One `STORE_MANAGER` belongs to exactly one store in v1
5. Payment integrations are **Paytm + HDFC**
6. UPI flow uses **dynamic QR**
7. Payment confirmation is **webhook-first + status fallback**
8. `discount_amount` is optional and defaults to `0`
9. `tip_amount` is optional and defaults to `0`
10. `tip_allocations` defaults to empty if tip is zero
11. One bill-level discount only
12. Discount cap is 30% of service subtotal
13. Tip is non-taxable
14. Commission is 10% of line net after discount
15. Split payment supports **cash + UPI**
16. Retry only creates a new UPI leg for the outstanding due amount
17. Idempotency uses a dedicated `idempotency_keys` table
18. Backend is the only source of truth for prices, tax, payment state, and totals

---

# 3. Backend Package / Module Design

## 3.1 Project Structure

```text
cmd/
  api/
    main.go

internal/
  auth/
    handler.go
    service.go
    repo.go
    middleware.go
    models.go

  bootstrap/
    handler.go
    service.go
    repo.go
    dto.go

  catalogue/
    handler.go
    service.go
    repo.go
    models.go
    dto.go
    validator.go

  staff/
    handler.go
    service.go
    repo.go
    models.go
    dto.go
    validator.go

  billing/
    handler.go
    service.go
    repo.go
    models.go
    dto.go
    validator.go
    calculator.go
    mapper.go

  payments/
    handler.go
    service.go
    repo.go
    models.go
    dto.go
    webhook.go
    mapper.go
    providers/
      provider.go
      paytm.go
      hdfc.go

  analytics/
    handler.go
    service.go
    repo.go
    dto.go

  audit/
    service.go
    repo.go
    models.go

  platform/
    config/
      config.go
    db/
      postgres.go
      tx.go
    http/
      router.go
      response.go
      error.go
    logger/
      logger.go

  shared/
    money/
      money.go
    enums/
      bill.go
      payment.go
      roles.go
    errors/
      codes.go
      errors.go
    idempotency/
      idempotency.go
```

---

## 3.2 Dependency Rules

Preferred dependency direction:

- `handler` -> `service`
- `service` -> `repo`, `shared`, `platform`, narrow interfaces
- `repo` -> `platform/db`, models
- provider adapters -> external gateway APIs

Avoid:

- handlers calling repos directly
- one module importing another module’s repo directly
- circular dependencies
- gateway logic inside billing
- generic mega-repositories

---

## 3.3 Module Responsibilities

## Auth
Owns:
- login
- JWT issuance
- JWT validation
- auth middleware
- request scope derivation

Outputs:
- `AuthContext` with:
  - `UserID`
  - `TenantID`
  - `StoreID`
  - `Role`

## Bootstrap
Owns:
- `GET /store/bootstrap`
- loading store info
- loading active catalogue snapshot
- loading active staff snapshot

## Catalogue
Owns:
- create catalogue item
- update catalogue item
- deactivate catalogue item
- list catalogue items
- fetch active items for billing

## Staff
Owns:
- create staff
- deactivate staff
- assign/match staff to store
- list staff
- validate staff-store mapping for billing

## Billing
Owns:
- create bill
- get bill
- cancel bill
- bill numbering
- totals computation
- discount allocation
- tax calculation
- commission calculation
- tip validation
- bill state transitions

## Payments
Owns:
- initiate dynamic QR
- retry UPI
- webhook handling
- status fallback checking
- payment status normalization
- bill payment aggregate updates

## Analytics
Owns:
- bill list
- filtered bill queries
- summary totals
- cancelled bill counts
- tax / tip / commission totals

## Audit
Owns:
- writing audit log records for major actions

---

# 4. Auth and Session LLD

## 4.1 Auth Model

Use simple JWT auth for both:
- `STORE_MANAGER`
- `SUPER_ADMIN`

JWT contains enough identity to resolve user, role, tenant, and store context.

Backend middleware derives:
- `user_id`
- `tenant_id`
- `store_id`
- `role`

Client never sends trusted scope fields.

---

## 4.2 User Model Rule

For v1:

- one `STORE_MANAGER` maps to exactly one store
- one manager cannot switch stores

This keeps auth and scope simple.

---

## 4.3 Main Auth Flow

1. user submits credentials
2. auth service validates credentials
3. backend returns JWT
4. client sends JWT in authenticated requests
5. middleware validates token and attaches auth context

---

## 4.4 Main Auth Files

- `auth/handler.go`
- `auth/service.go`
- `auth/repo.go`
- `auth/middleware.go`

---

# 5. Core Backend APIs and Flows

## 5.1 Common API Rules

- all money in paise
- backend derives scope from JWT
- backend is source of truth for prices and payment state
- structured error responses
- idempotency required for create bill and retry UPI

Example error shape:

```json
{
  "error": {
    "code": "INVALID_DISCOUNT",
    "message": "Discount exceeds the maximum allowed limit.",
    "details": {}
  }
}
```

---

## 5.2 `GET /store/bootstrap`

### Purpose
Returns the active snapshot used by the tablet to refresh local SQLite cache.

### Allowed role
- `STORE_MANAGER`

### Response
Returns:
- store details
- active catalogue items
- active staff

### Flow
1. auth middleware resolves store scope
2. bootstrap service loads store
3. bootstrap repo loads active catalogue
4. bootstrap repo loads active store staff
5. response returned

---

## 5.3 `POST /bills`

### Purpose
Creates a bill, computes totals, persists bill data, and initiates UPI if needed.

### Allowed role
- `STORE_MANAGER`

### Request shape
```json
{
  "client_bill_ref": "tablet-01-temp-1743840102",
  "items": [
    {
      "catalogue_item_id": "uuid",
      "quantity": 1,
      "assigned_staff_id": "uuid"
    }
  ],
  "discount_amount": 1000,
  "tip_amount": 500,
  "tip_allocations": [
    {
      "staff_id": "uuid",
      "tip_amount": 300
    },
    {
      "staff_id": "uuid",
      "tip_amount": 200
    }
  ],
  "payment": {
    "mode": "SPLIT",
    "cash_amount": 30000,
    "upi_gateway": "PAYTM"
  },
  "idempotency_key": "uuid"
}
```

### Input defaults
- `discount_amount` missing -> `0`
- `tip_amount` missing -> `0`
- `tip_allocations` missing and `tip_amount = 0` -> `[]`

### Validations
- items must not be empty
- quantity > 0
- catalogue items must be active and tenant-scoped
- staff must be active and mapped to current store
- discount <= 30% of service gross total
- tip allocations must exactly sum to tip amount if tip > 0
- split cash amount must be > 0 and < final total
- idempotency key required

### Response
Returns:
- bill header
- computed bill items
- tip allocations
- payment legs
- active UPI payment payload if pending
- receipt payload

### Flow
1. parse request
2. normalize missing discount/tip values
3. validate request
4. load authoritative catalogue items
5. validate staff-store mappings
6. compute totals
7. perform idempotency check
8. persist bill graph in transaction
9. if UPI involved, call payment initiation after commit
10. return final response
11. write audit log

---

## 5.4 `GET /bills/{bill_id}`

### Purpose
Returns latest bill state for:
- payment refresh
- details view
- reprint
- cancellation context

### Allowed roles
- `STORE_MANAGER`
- `SUPER_ADMIN`

### Response
Returns:
- bill header
- bill items
- tip allocations
- payment legs
- active UPI payment if pending
- receipt payload

### Flow
1. validate scope
2. load bill graph
3. map to response DTO
4. return

---

## 5.5 `POST /bills/{bill_id}/cancel`

### Purpose
Cancels a bill immediately with mandatory reason.

### Allowed role
- `STORE_MANAGER`

### Request
```json
{
  "reason": "Customer requested cancellation due to duplicate bill."
}
```

### Validations
- reason required
- bill must exist
- bill must belong to current store
- bill must be cancellable

### Response
Returns:
- bill id
- bill number
- cancelled status
- cancellation reason
- cancelled timestamp

### Flow
1. parse request
2. load bill
3. validate cancellation
4. update bill status and cancellation metadata
5. write audit log
6. return updated bill summary

---

## 5.6 `POST /bills/{bill_id}/payments/retry-upi`

### Purpose
Creates a fresh UPI leg for the outstanding due amount.

### Allowed role
- `STORE_MANAGER`

### Request
```json
{
  "upi_gateway": "PAYTM",
  "client_retry_ref": "tablet-01-retry-1743849999",
  "idempotency_key": "uuid"
}
```

### Validations
- bill exists
- bill belongs to current store
- bill is retryable
- amount due > 0
- no active pending UPI leg already exists
- idempotency key required

### Response
Returns:
- bill summary
- active retry UPI payment payload
- payment legs

### Flow
1. parse request
2. validate bill retry state
3. create new UPI payment row in transaction
4. commit
5. call provider to create dynamic QR
6. update new payment row to pending
7. return response
8. write audit log

---

# 6. Database Schema LLD

## 6.1 `tenants`
Fields:
- `id` UUID PK
- `name`
- `active`
- `created_at`
- `updated_at`

---

## 6.2 `stores`
Fields:
- `id` UUID PK
- `tenant_id` UUID FK
- `name`
- `code`
- `location`
- `active`
- `created_at`
- `updated_at`

Constraints:
- unique `(tenant_id, code)`

Indexes:
- `tenant_id`
- unique `(tenant_id, code)`

---

## 6.3 `users`
Fields:
- `id` UUID PK
- `tenant_id` UUID nullable FK
- `store_id` UUID nullable FK
- `role`
- `name`
- `email_or_phone`
- `password_hash`
- `active`
- `created_at`
- `updated_at`

Rule:
- one store manager belongs to one store in v1

Indexes:
- unique login identity
- `(tenant_id, store_id)`

---

## 6.4 `staff`
Fields:
- `id` UUID PK
- `tenant_id` UUID FK
- `name`
- `active`
- `created_at`
- `updated_at`

Indexes:
- `tenant_id`
- `(tenant_id, active)`

---

## 6.5 `staff_store_mapping`
Fields:
- `id` UUID PK
- `staff_id` UUID FK
- `store_id` UUID FK
- `active`
- `created_at`
- `updated_at`

Constraints:
- unique `(staff_id, store_id)`

Indexes:
- unique `(staff_id, store_id)`
- `(store_id, active)`

---

## 6.6 `catalogue_items`
Fields:
- `id` UUID PK
- `tenant_id` UUID FK
- `name`
- `category`
- `list_price` bigint
- `active`
- `created_at`
- `updated_at`

Rule:
- tax inclusive list price
- stored in paise

Indexes:
- `(tenant_id, active)`
- `tenant_id`

---

## 6.7 `store_bill_counters`
Fields:
- `store_id` UUID PK/FK
- `last_bill_seq` bigint
- `updated_at`

Usage:
- lock row `FOR UPDATE` during bill creation

---

## 6.8 `bills`
Fields:
- `id` UUID PK
- `tenant_id` UUID FK
- `store_id` UUID FK
- `bill_number`
- `status`
- `service_gross_amount` bigint
- `discount_amount` bigint default `0`
- `service_net_amount` bigint
- `tip_amount` bigint default `0`
- `taxable_base_amount` bigint
- `tax_amount` bigint
- `total_amount` bigint
- `amount_paid` bigint
- `amount_due` bigint
- `payment_mode_summary`
- `created_by_user_id` UUID FK
- `created_at`
- `paid_at` nullable
- `cancelled_at` nullable
- `cancelled_by_user_id` UUID nullable FK
- `cancellation_reason` nullable

Constraints:
- unique `(store_id, bill_number)`

Indexes:
- unique `(store_id, bill_number)`
- `(store_id, created_at desc)`
- `(tenant_id, created_at desc)`
- `(status, created_at desc)`

---

## 6.9 `bill_items`
Fields:
- `id` UUID PK
- `bill_id` UUID FK
- `catalogue_item_id` UUID FK
- `service_name_snapshot`
- `unit_price_snapshot` bigint
- `quantity` integer
- `line_gross_amount` bigint
- `line_discount_amount` bigint
- `line_net_amount` bigint
- `taxable_base_amount` bigint
- `tax_amount` bigint
- `assigned_staff_id` UUID FK
- `commission_base_amount` bigint
- `commission_amount` bigint
- `created_at`

Indexes:
- `bill_id`
- `(assigned_staff_id, created_at desc)`

---

## 6.10 `bill_tip_allocations`
Fields:
- `id` UUID PK
- `bill_id` UUID FK
- `staff_id` UUID FK
- `tip_amount` bigint
- `created_at`

Constraints:
- unique `(bill_id, staff_id)`

Indexes:
- unique `(bill_id, staff_id)`
- `bill_id`

---

## 6.11 `payments`
Fields:
- `id` UUID PK
- `bill_id` UUID FK
- `gateway` nullable
- `payment_method`
- `amount` bigint
- `status`
- `gateway_order_id` nullable
- `gateway_txn_id` nullable
- `gateway_reference` nullable
- `request_payload` JSONB nullable
- `response_payload` JSONB nullable
- `verified_at` nullable
- `created_at`
- `updated_at`

Indexes:
- `bill_id`
- `(status, created_at desc)`
- `gateway_order_id`
- `gateway_txn_id`
- `gateway_reference`

---

## 6.12 `commission_ledger`
Fields:
- `id` UUID PK
- `bill_id` UUID FK
- `bill_item_id` UUID FK
- `staff_id` UUID FK
- `base_amount` bigint
- `commission_percent_bps` integer
- `commission_amount` bigint
- `created_at`

Rule:
- 10% stored as `1000` basis points

Indexes:
- `bill_id`
- `(staff_id, created_at desc)`

---

## 6.13 `idempotency_keys`
Fields:
- `id` UUID PK
- `tenant_id` UUID
- `store_id` UUID
- `idempotency_key`
- `request_hash`
- `status`
- `response_bill_id` nullable UUID
- `created_at`
- `updated_at`

Constraints:
- unique `(tenant_id, store_id, idempotency_key)`

---

## 6.14 `audit_logs`
Fields:
- `id` UUID PK
- `tenant_id` UUID
- `store_id` UUID nullable
- `entity_type`
- `entity_id`
- `action`
- `performed_by_user_id` UUID nullable
- `metadata` JSONB
- `created_at`

Indexes:
- `(entity_type, entity_id)`
- `created_at desc`

---

# 7. Billing Calculation and State LLD

## 7.1 Input Defaults

Request normalization:
- missing `discount_amount` -> `0`
- missing `tip_amount` -> `0`
- missing `tip_allocations` with zero tip -> empty array

---

## 7.2 Authoritative Data Source

Billing always uses:
- catalogue prices from PostgreSQL
- staff-store validation from PostgreSQL

Frontend never supplies authoritative pricing.

---

## 7.3 Calculation Order

### Step 1: line gross
`line_gross_amount = unit_price_snapshot * quantity`

### Step 2: service gross total
`service_gross_total = sum(all line_gross_amount)`

### Step 3: discount validation
`discount_amount = 0` if absent

Validate:
- `discount_amount >= 0`
- `discount_amount <= floor(service_gross_total * 30 / 100)`

### Step 4: discount allocation
Allocate bill-level discount proportionally across lines using largest remainder.

### Step 5: line net
`line_net_amount = line_gross_amount - line_discount_amount`

### Step 6: tax after discount
`line_taxable_base_amount = floor(line_net_amount * 100 / 105)`

`line_tax_amount = line_net_amount - line_taxable_base_amount`

### Step 7: commission
`commission_base_amount = line_net_amount`

`commission_amount = floor(commission_base_amount * 10 / 100)`

### Step 8: tip validation
`tip_amount = 0` if absent

Rules:
- tip non-taxable
- tip not part of commission
- if tip is zero, allocations empty or zero-total
- if tip > 0, allocations must sum exactly to tip

### Step 9: totals
`service_net_total = service_gross_total - discount_amount`

`taxable_base_total = sum(line_taxable_base_amount)`

`tax_amount_total = sum(line_tax_amount)`

`bill_total = service_net_total + tip_amount`

---

## 7.4 Payment Mode Calculation

### CASH
- `amount_paid = bill_total`
- `amount_due = 0`
- bill status = `PAID`

### UPI
- `amount_paid = 0`
- `amount_due = bill_total`
- bill status = `PAYMENT_PENDING`

### SPLIT
Validate:
- `cash_amount > 0`
- `cash_amount < bill_total`

Then:
- `upi_amount = bill_total - cash_amount`
- `amount_paid = cash_amount`
- `amount_due = upi_amount`
- bill status = `PARTIALLY_PAID`

---

## 7.5 Invariants

- sum of line discounts = bill discount
- sum of line net amounts = service net total
- sum of taxable bases + sum of tax amounts = service net total
- sum of tip allocations = tip amount
- amount paid + amount due = total amount

State invariants:
- `PAID -> amount_due = 0`
- `PAYMENT_PENDING -> amount_paid = 0`
- `PARTIALLY_PAID -> amount_paid > 0 and amount_due > 0`

---

## 7.6 Cancellation Rule

Cancellation does not recompute amounts.

It only updates:
- bill status
- cancellation metadata

Historical bill totals remain immutable.

---

# 8. Payment Integration LLD

## 8.1 Goal

Implement the simplest reliable payment layer using:
- dynamic QR
- Paytm
- HDFC
- one provider abstraction
- webhook-first confirmation
- status fallback

---

## 8.2 Package Shape

```text
internal/payments/
  handler.go
  service.go
  repo.go
  models.go
  dto.go
  webhook.go
  mapper.go
  providers/
    provider.go
    paytm.go
    hdfc.go
```

---

## 8.3 Provider Interface

```go
type DynamicQRProvider interface {
    CreateDynamicQR(ctx context.Context, req CreateDynamicQRRequest) (CreateDynamicQRResult, error)
    GetPaymentStatus(ctx context.Context, req GetPaymentStatusRequest) (GetPaymentStatusResult, error)
    ParseWebhook(ctx context.Context, headers map[string]string, body []byte) (WebhookEvent, error)
    VerifyWebhook(ctx context.Context, headers map[string]string, body []byte) error
}
```

---

## 8.4 Main Flows

### New UPI bill flow
1. billing creates UPI payment row with `INITIATED`
2. DB transaction commits
3. payments service calls provider
4. provider creates dynamic QR
5. payment row updated to `PENDING`
6. QR payload returned

If QR creation fails:
- payment row -> `FAILED`
- bill -> `PAYMENT_FAILED` for UPI-only
- bill stays `PARTIALLY_PAID` for split

### Retry UPI flow
1. lock bill row
2. validate retry eligibility
3. insert new payment row with `INITIATED`
4. commit
5. create fresh QR
6. update new row to `PENDING`

### Webhook flow
1. provider endpoint receives webhook
2. provider verifies + parses payload
3. payments service finds payment row
4. lock payment + bill rows
5. update payment status
6. recompute bill `amount_paid`, `amount_due`, `status`
7. write audit log

### Status fallback
If webhook is delayed:
- use provider `GetPaymentStatus`
- update payment row if changed
- return latest bill state via `GET /bills/{bill_id}`

---

## 8.5 Key Rules

- webhook-first confirmation
- status fallback supported
- retry creates a new UPI row
- old failed rows remain unchanged
- no gateway-specific logic inside billing
- no external gateway call inside DB transaction

---

# 9. Repository and Transaction LLD

## 9.1 Repository Ownership

### Auth repo
- user lookup
- auth metadata
- scope lookup

### Bootstrap repo
- store snapshot
- active catalogue for bootstrap
- active staff for bootstrap

### Catalogue repo
- create/update/deactivate item
- list items
- fetch active items by ids

### Staff repo
- create/deactivate staff
- manage staff-store mapping
- fetch active mapping

### Billing repo
- lock store bill counter
- insert bill header
- insert bill items
- insert tip allocations
- insert commission rows
- insert initial payment rows
- fetch bill graph
- cancel bill

### Payments repo
- insert retry payment row
- update payment row refs/status
- fetch payment by provider ref/order id
- fetch payment legs by bill
- lock payment row
- update bill paid/due/status from payment result

### Analytics repo
- bill list queries
- aggregate totals

### Audit repo
- insert audit rows

---

## 9.2 Transaction Boundaries

## Create bill transaction
Inside one transaction:
1. claim/check idempotency
2. load/validate required rows
3. lock `store_bill_counters`
4. generate bill number
5. insert `bills`
6. insert `bill_items`
7. insert `bill_tip_allocations`
8. insert `commission_ledger`
9. insert initial `payments`
10. commit

After commit:
11. call gateway if UPI involved
12. update payment row with QR result

## Retry UPI transaction
Inside one transaction:
1. check idempotency
2. lock bill row
3. validate retry eligibility
4. insert new payment row with `INITIATED`
5. commit

After commit:
6. call gateway
7. update payment row to `PENDING`

## Webhook transaction
Inside one transaction:
1. find target payment row
2. lock payment row
3. lock related bill row
4. update payment status
5. recompute bill amounts/status
6. persist verified metadata
7. commit

## Cancel bill transaction
Inside one transaction:
1. load/lock bill if needed
2. validate cancellation
3. update bill status and reason
4. commit

---

## 9.3 Locking Points

Required locks:
- `store_bill_counters` during bill number generation
- bill row during retry validation
- payment + bill rows during webhook update

Avoid:
- table locks
- long transactions
- lock + network call patterns

---

## 9.4 Simplicity Rules

- one repo per module
- no generic repository layer
- transaction ownership stays in service layer
- repo methods remain explicit

---

# 10. Analytics Backend LLD

## 10.1 Scope

Include:
- bill list
- store/date/status filters
- cancelled bill counts
- tax totals
- commission totals
- tip totals
- sales totals

---

## 10.2 Main API Outputs

### Bill list
Return:
- bill id
- bill number
- created_at
- store id/name
- status
- total amount
- amount paid
- amount due
- payment mode summary
- cancellation reason if cancelled

### Summary totals
Return:
- total bills
- total sales
- cancelled bill count
- cancelled amount
- total tax
- total commission
- total tip

---

## 10.3 Query Rules

- keep analytics read-only
- query directly from PostgreSQL
- no separate reporting database in v1
- use indexed filters only where practical

---

# 11. Minimal Audit LLD

Audit events to store:
- bill created
- bill cancelled
- payment initiated
- payment success
- payment failed
- staff created/deactivated
- catalogue item created/updated/deactivated

Audit record fields:
- tenant_id
- store_id
- entity_type
- entity_id
- action
- performed_by_user_id
- metadata
- created_at

---

# 12. Final Backend Simplicity Rules

1. Go modular monolith only
2. PostgreSQL only
3. JWT auth only
4. one manager -> one store
5. all money in paise
6. backend owns all billing math
7. dynamic QR only for UPI
8. Paytm + HDFC behind one provider interface
9. webhook-first with status fallback
10. idempotency for create bill and retry UPI
11. no gateway call inside DB transaction
12. no optional abstraction layers
13. no client-side truth for prices or payment state
