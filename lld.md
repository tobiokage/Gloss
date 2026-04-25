# Salon Billing Backend LLD — Revised HDFC-Only Payment Module

This document replaces the payment-related parts of the backend LLD.

It is intentionally narrow and implementation-first.

It follows these rules:
- keep billing math unchanged unless payment integration forces a change
- keep HDFC integration guide as the source of truth for API shape, transport, request fields, response fields, and status handling
- prefer the smallest correct design over speculative multi-gateway abstractions
- isolate HDFC-specific transport and crypto details inside the payments module
- keep external gateway calls outside DB transactions

---

# 1. Scope of This Revision

This revision changes only the backend payment design and the payment-adjacent contracts that must change with it.

This revision does **not** redesign:
- auth
- catalogue
- staff
- bill math
- analytics shape
- audit table shape
- offline workflow
- Paytm implementation

Paytm is **fully out of scope** for the current implementation pass.

The payment module is HDFC-only for now.

---

# 2. Updated Locked Design Decisions

1. Backend is a **Go modular monolith**
2. Database is **PostgreSQL**
3. Auth is **JWT-based**
4. One `STORE_MANAGER` belongs to exactly one store in v1
5. All money values are stored and computed in **paise**
6. Backend is the only source of truth for bill totals, payment state, and bill state
7. Store-side frontend label for digital payment selection is **ONLINE**
8. Backend bill payment modes are:
   - `CASH`
   - `ONLINE`
   - `SPLIT`
9. `SPLIT` means **cash + online**
10. HDFC terminal owns customer interaction and QR generation
11. Backend never generates or returns HDFC QR payload
12. HDFC payment confirmation uses **Status API only**
13. HDFC webhook handling does not exist in current scope
14. HDFC integration uses only:
   - Sale API
   - Transaction Status API
   - Cancel Sale API
15. HDFC Void Sale, Bank EMI, Brand EMI, Cash@Pos, UPI Collect, terminal tip, and all other HDFC optional capabilities are out of scope
16. HDFC `tid` is mandatory and store-scoped
17. HDFC `mobileNo` is not compulsory for this integration path and is omitted from the active request mapping
18. One unique HDFC `saleTxnId` is generated per online payment attempt
19. HDFC `bhTxnId` is the provider transaction identifier used for later status checks and provider-side cancellation
20. Retry creates a new online payment row only for the remaining due amount
21. Old failed or cancelled online payment rows remain immutable for history
22. Bill cancellation and provider-side payment-attempt cancellation are different operations
23. No external gateway call may happen inside a DB transaction
24. No speculative multi-provider abstraction is allowed in this implementation pass

---

# 3. Backend Scope Changes

The backend covers:

- authentication and session scope
- store bootstrap for local cache refresh
- catalogue management
- staff management
- bill creation
- bill retrieval
- bill cancellation
- online payment initiation through HDFC
- online payment retry through HDFC
- provider-side payment-attempt cancellation through HDFC
- HDFC payment status synchronization
- bill list and summary analytics
- audit logging
- database persistence and transaction handling

The backend does **not** cover:
- payment webhooks
- Paytm implementation
- dynamic QR generation by backend
- HDFC Void Sale
- EMI flows
- UPI Collect
- customer profile management

---

# 4. Project Structure — Revised Payments Module

```text
internal/
  payments/
    handler.go
    service.go
    repo.go
    models.go
    dto.go
    mapper.go
    hdfc/
      client.go
      crypto.go
      types.go
      mapper.go
```

Do not add:
- `webhook.go`
- `providers/provider.go`
- `providers/paytm.go`

## Rationale

This is intentionally concrete.

A shared provider interface is not justified because:
- only HDFC is being implemented now
- HDFC does not fit the old QR-plus-webhook abstraction
- preserving a fake common interface would add drift and unnecessary code

---

# 5. Module Responsibilities — Revised Payments Ownership

## Payments
Owns:
- HDFC sale initiation
- HDFC status synchronization
- HDFC provider-side payment-attempt cancellation
- payment row state transitions
- bill paid/due/status recomputation based on confirmed provider state
- storage of provider request/response metadata
- storage of actual completion mode returned by HDFC

Does **not** own:
- bill math
- tax/discount/commission logic
- bill cancellation semantics
- webhook handling
- Paytm logic

## Billing
Still owns:
- create bill
- get bill
- cancel bill
- totals computation
- bill state transitions at the domain level
- insertion of initial payment rows inside bill creation transaction

Billing does **not** know HDFC request/response field details.

---

# 6. Store-Facing API Contract — Revised Payment Semantics

## 6.1 Payment Modes

The store app shows these payment modes:
- `CASH`
- `ONLINE`
- `SPLIT`

Rules:
- `ONLINE` means a digital payment attempt routed through HDFC terminal
- actual provider completion mode is stored separately from cashier intent
- `SPLIT` means cash collected immediately plus one HDFC online leg for remaining due amount

## 6.2 Create Bill Request Shape

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
    "cash_amount": 30000
  },
  "idempotency_key": "uuid"
}
```

Rules:
- there is no `upi_gateway` field in the active implementation
- gateway is implicitly HDFC for online payment paths in this revision
- frontend label is ONLINE
- `cash_amount` exists only for `SPLIT`
- no HDFC QR payload is returned

## 6.3 Mobile Number Mapping

Although the original guide marks `mobileNo` in the sale payload, the confirmed integration path for this implementation does not require it.

Rule:
- `mobileNo` is omitted from the active HDFC sale request mapping
- no customer mobile input is required in the current store flow
- do not add customer-profile or customer-capture logic for this payment module

## 6.4 Create Bill Response Shape

```json
{
  "bill": {},
  "items": [],
  "tip_allocations": [],
  "payments": [],
  "receipt": {},
  "active_online_payment": {
    "payment_id": "uuid",
    "gateway": "HDFC",
    "status": "PENDING",
    "terminal_flow": "HDFC_TERMINAL_OWNED",
    "provider_request_id": "saleTxnId",
    "provider_txn_id": "bhTxnId",
    "can_cancel_attempt": true
  }
}
```

Rules:
- response must not contain QR payload for HDFC
- response must communicate that cashier/customer must complete the payment on terminal
- `provider_txn_id` may be null if create-sale failed before `bhTxnId` was obtained
- `can_cancel_attempt` is true only when provider-side cancellation is valid

## 6.5 Get Bill Response

`GET /bills/{bill_id}` returns:
- bill header
- bill items
- tip allocations
- payment legs
- active online payment context if any
- receipt payload

For HDFC pending payments it may also return:
- latest normalized provider status
- actual completion mode if known
- `can_cancel_attempt`

It must not return any HDFC QR payload.

---

# 7. Store Bootstrap Changes

`GET /store/bootstrap` must include store payment capability metadata needed by tablet workflow.

Include:
- whether HDFC online payment is enabled for the store
- whether store has valid HDFC terminal configuration
- frontend payment modes available for the store

Do not expose secrets.

Optional UI-safe field:
- masked or display-safe TID indicator

---

# 8. Database Schema Changes

## 8.1 `stores` table

Add:

- `hdfc_terminal_tid` varchar(8) nullable

Rules:
- required for stores that use HDFC online payments
- stored as string, not integer
- leading zeros must be preserved

## 8.2 `payments` table

Keep existing business fields:
- `id`
- `bill_id`
- `gateway`
- `payment_method`
- `amount`
- `status`
- `request_payload`
- `response_payload`
- `verified_at`
- timestamps

Add these HDFC-aligned fields:

- `provider_request_id` text nullable
- `provider_txn_id` text nullable
- `terminal_tid` varchar(8) nullable
- `provider_status_code` text nullable
- `provider_status_message` text nullable
- `provider_txn_status` text nullable
- `provider_txn_message` text nullable
- `actual_completion_mode` text nullable
- `status_details_payload` jsonb nullable
- `cancel_response_payload` jsonb nullable
- `last_status_checked_at` timestamptz nullable
- `provider_sale_requested_at` timestamptz nullable
- `provider_confirmed_at` timestamptz nullable

### Field Mapping

- `provider_request_id` -> HDFC `saleTxnId`
- `provider_txn_id` -> HDFC `bhTxnId`
- `terminal_tid` -> HDFC `tid`
- `actual_completion_mode` -> HDFC `paymentMode`
- `provider_status_code` -> response `statusCode`
- `provider_status_message` -> response `statusMessage`
- `provider_txn_status` -> response `txnStatus`
- `provider_txn_message` -> response `txnMessage`

### Indexes

Add:
- unique index on `provider_request_id` where not null
- index on `provider_txn_id`
- index on `(bill_id, status)`
- index on `last_status_checked_at`

## 8.3 Money Model

No change:
- all money remains stored in paise
- HDFC amount conversion to `"10.00"` rupee string happens only inside HDFC adapter code

---

# 9. Payment State Model — Revised

## 9.1 Bill Statuses

Bill statuses remain:
- `DRAFT`
- `PAYMENT_PENDING`
- `PARTIALLY_PAID`
- `PAID`
- `PAYMENT_FAILED`
- `CANCELLED`

## 9.2 Payment Statuses

Payment statuses remain:
- `INITIATED`
- `PENDING`
- `SUCCESS`
- `FAILED`
- `CANCELLED`

## 9.3 HDFC Provider Status Mapping

Map HDFC `txnStatus` to internal payment state:

- `InProgress` -> `PENDING`
- `Success` -> `SUCCESS`
- `Failed` -> `FAILED`
- `Canceled` -> `CANCELLED`

## 9.4 Completion Mode Recording

Even if cashier selected `ONLINE`, backend stores actual provider-reported completion mode from HDFC.

Examples:
- `CardPayment`
- any other provider-returned value

This value is informational and audit-oriented.

It does not rewrite historical cashier input.

---

# 10. HDFC Transport and Crypto LLD

## 10.1 Endpoints Used

Use only:

- `POST {baseurl}/API/ecr/v2/saletxn`
- `POST {baseurl}/API/ecr/v2/txnstatus`
- `POST {baseurl}/API/ecr/v2/canceltxn`

Do not implement:
- `voidsaletxn`
- any EMI endpoint
- any webhook endpoint

## 10.2 Required Config

Application config:
- `HDFC_BASE_URL`
- `HDFC_CLIENT_API_KEY`
- `HDFC_CLIENT_SECRET_KEY`
- `HDFC_AUTHORIZATION_TOKEN`
- `HDFC_IV`

Store config:
- `hdfc_terminal_tid`

## 10.3 Required Headers

Every HDFC request must send:
- `bh_client_apikey`
- `bh_client_traceid`
- `bh_client_timestamp`
- `authorizationToken`

## 10.4 Payload Envelope

Outer request JSON shape:

```json
{
  "payLoadData": "<encrypted-hex-string>",
  "tid": "63000019"
}
```

## 10.5 Encryption Rules

Implement exactly as per HDFC guide:
- AES-256
- CBC mode
- PKCS5 padding
- Client secret key is hex-decoded to bytes
- IV is encoded as UTF-8 bytes
- request inner JSON is UTF-8 encoded before encryption
- encrypted bytes are hex-encoded into `payLoadData`

Decryption follows the inverse process.

---

# 11. HDFC Request and Response Mapping

## 11.1 Sale Request

Inner payload fields used:

- `saleTxnId` required
- `saleAmount` required
- `mobileNo` omitted in the active integration mapping
- `email` null
- `customerName` null
- `description` optional
- `skuIds` null
- `field1` null
- `field2` null
- `field3` null
- `field4` null
- `field5` null

### Field Rules

- `saleTxnId` must be unique per online payment row
- `saleAmount` is decimal rupee string with two fractional digits
- `description` may contain bill number for reconciliation if length fits
- no speculative use of reserved fields

## 11.2 Sale Response

Persist and normalize:
- `statusCode`
- `statusMessage`
- `saleTxnId`
- `saleAmount`
- `saleDateTime`
- `txnStatus`
- `txnMessage`
- `bhTxnId`

## 11.3 Status Request

Use:
- `bhTxnId` required
- `saleTxnId` optional

Use stored `provider_txn_id` as primary source.

## 11.4 Status Response

Persist and normalize:
- request status fields
- transaction status fields
- payment mode
- terminal identifiers and payment details if present
- raw response payload

At minimum read:
- `statusCode`
- `statusMessage`
- `saleTxnId`
- `saleAmount`
- `bhTxnId`
- `saleDateTime`
- `txnStatus`
- `txnMessage`
- `PaymentStatusDetails`

From `PaymentStatusDetails`, map if present:
- `paymentMode`
- `txnTID`
- `batchNo`
- `invoiceNo`
- `stan`
- `txnAmount`
- `txnDateTime`
- `mTxnid`
- `pgwTxnid`
- `txnAuthCode`
- `txnRRN`
- `maskedCardNo`
- `issuerRefNo`
- `tipAmount`
- `txnType`
- `partnerTxnId`
- `refPartnerTxnId`
- `cashAmount`

Unknown fields must be tolerated and preserved in raw payload.

## 11.5 Cancel Sale Request

Use:
- `bhTxnId` required
- `saleTxnId` optional
- `tid` required

## 11.6 Cancel Sale Response

Persist and normalize:
- `statusCode`
- `statusMessage`
- `saleTxnId`
- `bhTxnId`
- `txnStatus`
- `txnMessage`

Map HDFC `Canceled` to internal `CANCELLED`.

---

# 12. Main Payment Flows

## 12.1 New ONLINE Bill Flow

1. billing validates items, staff mappings, and totals
2. billing inserts bill graph inside DB transaction
3. billing inserts one payment row:
   - `gateway = HDFC`
   - `payment_method = ONLINE`
   - `amount = total_amount`
   - `status = INITIATED`
4. billing commits
5. payments service loads store `hdfc_terminal_tid`
6. payments service generates unique `saleTxnId` for this payment row
7. payments service converts paise amount to rupee string
8. payments service builds HDFC sale request
9. payments service encrypts payload and calls HDFC Sale API
10. payments service stores request/response metadata
11. if response is accepted and `txnStatus = InProgress`, payment -> `PENDING`
12. if response is accepted and `txnStatus = Success`, payment -> `SUCCESS`, bill aggregates recomputed immediately
13. if response is accepted and `txnStatus = Failed`, payment -> `FAILED`
14. if response is accepted and `txnStatus = Canceled`, payment -> `CANCELLED`
15. if transport or request-level failure occurs before usable provider confirmation, mark failure only when it is safe to do so; otherwise leave payment unresolved for controlled reconciliation

## 12.2 New SPLIT Bill Flow

1. billing validates split rules
2. billing inserts bill graph inside DB transaction
3. billing inserts:
   - one cash payment row already successful
   - one HDFC online payment row for remaining due with `INITIATED`
4. billing commits
5. payments service initiates HDFC Sale only for remaining due amount
6. if HDFC sale becomes pending, bill remains `PARTIALLY_PAID`
7. later status synchronization updates bill to `PAID`, `PARTIALLY_PAID`, or `PAYMENT_FAILED` as appropriate

## 12.3 Retry ONLINE Flow

1. validate bill belongs to current store
2. validate amount due > 0
3. validate no active pending HDFC payment already exists
4. create new online payment row with `INITIATED` inside transaction
5. commit
6. initiate new HDFC Sale for remaining due amount
7. persist provider identifiers and statuses
8. return active online payment context

Old failed/cancelled payment rows remain unchanged.

## 12.4 Status Synchronization Flow

Status synchronization is the primary confirmation path.

Trigger points:
- `GET /bills/{bill_id}` for pending HDFC payments
- explicit read/refresh path if added later

Flow:
1. load active HDFC payment row
2. skip sync if `last_status_checked_at` is too recent
3. require `provider_txn_id` (`bhTxnId`) for status call
4. call HDFC Status API
5. persist raw status response
6. normalize internal payment status
7. recompute bill `amount_paid`, `amount_due`, `status`
8. set `provider_confirmed_at` when success/failure/cancelled becomes terminal
9. return latest bill graph

## 12.5 Provider-Side Payment Attempt Cancellation Flow

This is not bill cancellation.

Endpoint:
- `POST /bills/{bill_id}/payments/{payment_id}/cancel-attempt`

Rules:
- payment must belong to current store bill
- payment must be HDFC-backed
- payment must still be provider-cancellable
- stored `provider_txn_id` is required
- HDFC cancel is called outside DB transaction
- result updates only payment/bill payment state, not bill cancellation metadata

Flow:
1. validate bill and payment scope
2. validate payment state is cancellable
3. build HDFC cancel request using `bhTxnId`, optional `saleTxnId`, and `tid`
4. call HDFC Cancel Sale API
5. persist cancel response metadata
6. if HDFC says cancelled, payment -> `CANCELLED`
7. recompute bill amounts/status
8. return updated payment/bill summary

## 12.6 Bill Cancellation Flow

Bill cancellation is separate.

Rules:
- bill cancellation updates bill status and cancellation metadata
- historical bill totals remain unchanged
- bill with active pending HDFC payment attempt must not be bill-cancelled until the payment attempt is resolved or provider-cancelled

---

# 13. Repository Ownership — Revised Payments Repo

Payments repo owns:

- persist payment initiation identifiers
- persist HDFC request payload
- persist HDFC response payload
- fetch active payment legs by bill
- fetch payment by `provider_txn_id`
- fetch payment by internal id
- lock payment row
- update payment state from normalized HDFC result
- update bill `amount_paid`, `amount_due`, `status`
- persist `last_status_checked_at`
- persist actual completion mode
- persist cancel response payload

Billing repo still owns:
- insert initial payment rows during bill creation transaction
- fetch bill graph
- cancel bill

---

# 14. Transaction Boundaries — Revised

## 14.1 Create Bill Transaction

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
11. call HDFC Sale API if online leg exists
12. update payment row with provider identifiers and normalized state

## 14.2 Retry ONLINE Transaction

Inside one transaction:
1. check idempotency
2. lock bill row
3. validate retry eligibility
4. insert new online payment row with `INITIATED`
5. commit

After commit:
6. call HDFC Sale API
7. update payment row with provider identifiers and normalized state

## 14.3 Status Sync Transaction

Inside one transaction:
1. lock payment row
2. lock related bill row
3. update payment status and provider metadata
4. recompute bill amounts/status
5. commit

## 14.4 Provider-Side Cancel Transaction

Before transaction:
1. validate payment attempt should be cancelled
2. call HDFC Cancel Sale API

Inside one transaction:
3. lock payment row
4. lock related bill row
5. update payment state and cancel metadata
6. recompute bill amounts/status
7. commit

## 14.5 Bill Cancel Transaction

Inside one transaction:
1. load/lock bill
2. validate no active pending HDFC payment exists
3. validate bill cancellation rule
4. update bill status and cancellation metadata
5. commit

---

# 15. Failure Handling Rules

## 15.1 Safe Failure Rules

- no external call inside DB transaction
- no auto-generation of duplicate payment rows on network ambiguity
- no silent reuse of `saleTxnId`
- no silent conversion of unresolved HDFC create-sale timeout into hard failure without evidence
- preserve raw provider payloads for audit/debug
- preserve old payment rows for history

## 15.2 Ambiguous Create-Sale Failure

If HDFC sale request times out or transport fails before usable `bhTxnId` is received:
- do not auto-create a second payment row
- do not generate a new `saleTxnId` for the same attempt
- do not silently retry blindly
- keep the attempt in controlled unresolved state until explicit reconciliation path is defined

## 15.3 Invalid Store Config

If store `hdfc_terminal_tid` is missing or invalid:
- block online payment initiation before provider call
- return validation/configuration error
- do not create a second payment attempt outside normal retry flow

---

# 16. Logging and Audit Rules

Audit these payment events:
- online payment initiated
- online payment pending
- online payment success
- online payment failed
- online payment cancelled by provider
- online payment retry initiated
- bill cancelled
- payment-attempt cancellation requested
- payment-attempt cancellation completed

Logging rules:
- never log secrets
- never log authorization token
- never log raw secret key or IV
- mask or omit customer mobile if later added
- raw provider payload storage is allowed in DB payload columns, but secrets must not be written there

---

# 17. Simplicity Rules for This Payment Module

1. HDFC guide wins in every conflict involving request/response/API shape
2. Do not preserve Paytm abstractions in current implementation
3. Do not add webhook code
4. Do not add background workers just for payment polling
5. Do not add generalized provider registries
6. Do not leak HDFC crypto logic into billing package
7. Do not leak bill math into payments package
8. Keep one obvious ownership path:
   - billing persists initial money graph
   - payments orchestrates HDFC calls and provider-state sync
   - repo persists state changes
9. Keep DTOs explicit
10. Keep transaction boundaries short and visible
