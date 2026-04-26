# API Examples

Example UUIDs and tokens are fake. All money values are paise. Store endpoints use a STORE_MANAGER bearer token; admin endpoints use a SUPER_ADMIN bearer token.

## Auth

### POST /auth/login

```http
POST /auth/login
Content-Type: application/json

{
  "email_or_phone": "manager@example.com",
  "password": "example-password"
}
```

```json
{
  "token": "fake.jwt.token"
}
```

## Store App

### GET /store/bootstrap

```http
GET /store/bootstrap
Authorization: Bearer fake-store-manager-token
```

### POST /bills

```http
POST /bills
Authorization: Bearer fake-store-manager-token
Content-Type: application/json

{
  "client_bill_ref": "tablet-20260425-0001",
  "items": [
    {
      "catalogue_item_id": "88888888-8888-4888-8888-888888888888",
      "quantity": 1,
      "assigned_staff_id": "44444444-4444-4444-8444-444444444444"
    }
  ],
  "discount_amount": 0,
  "tip_amount": 0,
  "payment": {
    "mode": "ONLINE"
  },
  "idempotency_key": "tablet-20260425-0001-create"
}
```

For split payment, use `"mode": "SPLIT"` and include `"cash_amount"` less than the bill total. The backend calculates all totals from authoritative catalogue prices.

### GET /bills/{bill_id}

```http
GET /bills/bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb
Authorization: Bearer fake-store-manager-token
```

### POST /bills/{bill_id}/cancel

```http
POST /bills/bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb/cancel
Authorization: Bearer fake-store-manager-token
Content-Type: application/json

{
  "reason": "Customer requested cancellation before payment completion"
}
```

### POST /bills/{bill_id}/payments/retry-online

```http
POST /bills/bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb/payments/retry-online
Authorization: Bearer fake-store-manager-token
Content-Type: application/json

{
  "client_retry_ref": "tablet-20260425-0001-retry-1",
  "idempotency_key": "tablet-20260425-0001-retry-1"
}
```

### POST /bills/{bill_id}/payments/{payment_id}/cancel-attempt

```http
POST /bills/bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb/payments/cccccccc-cccc-4ccc-8ccc-cccccccccccc/cancel-attempt
Authorization: Bearer fake-store-manager-token
```

## Admin App

### GET /admin/catalogue

```http
GET /admin/catalogue
Authorization: Bearer fake-super-admin-token
```

### POST /admin/catalogue

```http
POST /admin/catalogue
Authorization: Bearer fake-super-admin-token
Content-Type: application/json

{
  "name": "Haircut",
  "category": "Hair",
  "list_price": 49900
}
```

### PUT /admin/catalogue/{id}

```http
PUT /admin/catalogue/88888888-8888-4888-8888-888888888888
Authorization: Bearer fake-super-admin-token
Content-Type: application/json

{
  "name": "Haircut",
  "category": "Hair",
  "list_price": 54900
}
```

### POST /admin/catalogue/{id}/deactivate

```http
POST /admin/catalogue/88888888-8888-4888-8888-888888888888/deactivate
Authorization: Bearer fake-super-admin-token
```

### GET /admin/staff

```http
GET /admin/staff
Authorization: Bearer fake-super-admin-token
```

### POST /admin/staff

```http
POST /admin/staff
Authorization: Bearer fake-super-admin-token
Content-Type: application/json

{
  "name": "Aarav"
}
```

### POST /admin/staff/{id}/deactivate

```http
POST /admin/staff/44444444-4444-4444-8444-444444444444/deactivate
Authorization: Bearer fake-super-admin-token
```

### POST /admin/staff/{id}/stores/{store_id}

```http
POST /admin/staff/44444444-4444-4444-8444-444444444444/stores/22222222-2222-4222-8222-222222222222
Authorization: Bearer fake-super-admin-token
```

### GET /admin/bills

```http
GET /admin/bills?store_id=22222222-2222-4222-8222-222222222222&date_from=2026-04-01&date_to=2026-05-01&status=PAID&limit=50&offset=0
Authorization: Bearer fake-super-admin-token
```

### GET /admin/analytics/summary

```http
GET /admin/analytics/summary?store_id=22222222-2222-4222-8222-222222222222&date_from=2026-04-01&date_to=2026-05-01
Authorization: Bearer fake-super-admin-token
```
