CREATE TABLE tenants (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL,
    active BOOLEAN NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE stores (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants (id),
    name TEXT NOT NULL,
    code TEXT NOT NULL,
    location TEXT NOT NULL,
    active BOOLEAN NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT uq_stores_tenant_code UNIQUE (tenant_id, code)
);

CREATE INDEX idx_stores_tenant_id ON stores (tenant_id);

CREATE TABLE users (
    id UUID PRIMARY KEY,
    tenant_id UUID NULL REFERENCES tenants (id),
    store_id UUID NULL REFERENCES stores (id),
    role TEXT NOT NULL CHECK (role IN ('SUPER_ADMIN', 'STORE_MANAGER')),
    name TEXT NOT NULL,
    email_or_phone TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    active BOOLEAN NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT uq_users_email_or_phone UNIQUE (email_or_phone)
);

CREATE INDEX idx_users_tenant_store ON users (tenant_id, store_id);

CREATE TABLE staff (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants (id),
    name TEXT NOT NULL,
    active BOOLEAN NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_staff_tenant_id ON staff (tenant_id);
CREATE INDEX idx_staff_tenant_active ON staff (tenant_id, active);

CREATE TABLE staff_store_mapping (
    id UUID PRIMARY KEY,
    staff_id UUID NOT NULL REFERENCES staff (id),
    store_id UUID NOT NULL REFERENCES stores (id),
    active BOOLEAN NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT uq_staff_store_mapping_staff_store UNIQUE (staff_id, store_id)
);

CREATE INDEX idx_staff_store_mapping_store_active ON staff_store_mapping (store_id, active);

CREATE TABLE catalogue_items (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants (id),
    name TEXT NOT NULL,
    category TEXT NOT NULL,
    list_price BIGINT NOT NULL,
    active BOOLEAN NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_catalogue_items_tenant_active ON catalogue_items (tenant_id, active);
CREATE INDEX idx_catalogue_items_tenant_id ON catalogue_items (tenant_id);

CREATE TABLE store_bill_counters (
    store_id UUID PRIMARY KEY REFERENCES stores (id),
    last_bill_seq BIGINT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE bills (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants (id),
    store_id UUID NOT NULL REFERENCES stores (id),
    bill_number TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('PAID', 'PAYMENT_PENDING', 'PAYMENT_FAILED', 'PARTIALLY_PAID', 'CANCELLED')),
    service_gross_amount BIGINT NOT NULL,
    discount_amount BIGINT NOT NULL DEFAULT 0,
    service_net_amount BIGINT NOT NULL,
    tip_amount BIGINT NOT NULL DEFAULT 0,
    taxable_base_amount BIGINT NOT NULL,
    tax_amount BIGINT NOT NULL,
    total_amount BIGINT NOT NULL,
    amount_paid BIGINT NOT NULL,
    amount_due BIGINT NOT NULL,
    payment_mode_summary TEXT NOT NULL CHECK (payment_mode_summary IN ('CASH', 'UPI', 'SPLIT')),
    created_by_user_id UUID NOT NULL REFERENCES users (id),
    created_at TIMESTAMPTZ NOT NULL,
    paid_at TIMESTAMPTZ NULL,
    cancelled_at TIMESTAMPTZ NULL,
    cancelled_by_user_id UUID NULL REFERENCES users (id),
    cancellation_reason TEXT NULL,
    CONSTRAINT uq_bills_store_bill_number UNIQUE (store_id, bill_number)
);

CREATE INDEX idx_bills_store_created_at_desc ON bills (store_id, created_at DESC);
CREATE INDEX idx_bills_tenant_created_at_desc ON bills (tenant_id, created_at DESC);
CREATE INDEX idx_bills_status_created_at_desc ON bills (status, created_at DESC);

CREATE TABLE bill_items (
    id UUID PRIMARY KEY,
    bill_id UUID NOT NULL REFERENCES bills (id),
    catalogue_item_id UUID NOT NULL REFERENCES catalogue_items (id),
    service_name_snapshot TEXT NOT NULL,
    unit_price_snapshot BIGINT NOT NULL,
    quantity INTEGER NOT NULL,
    line_gross_amount BIGINT NOT NULL,
    line_discount_amount BIGINT NOT NULL,
    line_net_amount BIGINT NOT NULL,
    taxable_base_amount BIGINT NOT NULL,
    tax_amount BIGINT NOT NULL,
    assigned_staff_id UUID NOT NULL REFERENCES staff (id),
    commission_base_amount BIGINT NOT NULL,
    commission_amount BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_bill_items_bill_id ON bill_items (bill_id);
CREATE INDEX idx_bill_items_staff_created_at_desc ON bill_items (assigned_staff_id, created_at DESC);

CREATE TABLE bill_tip_allocations (
    id UUID PRIMARY KEY,
    bill_id UUID NOT NULL REFERENCES bills (id),
    staff_id UUID NOT NULL REFERENCES staff (id),
    tip_amount BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT uq_bill_tip_allocations_bill_staff UNIQUE (bill_id, staff_id)
);

CREATE INDEX idx_bill_tip_allocations_bill_id ON bill_tip_allocations (bill_id);

CREATE TABLE payments (
    id UUID PRIMARY KEY,
    bill_id UUID NOT NULL REFERENCES bills (id),
    gateway TEXT NULL CHECK (gateway IS NULL OR gateway IN ('PAYTM', 'HDFC')),
    payment_method TEXT NOT NULL CHECK (payment_method IN ('CASH', 'UPI')),
    amount BIGINT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('INITIATED', 'PENDING', 'SUCCESS', 'FAILED')),
    gateway_order_id TEXT NULL,
    gateway_txn_id TEXT NULL,
    gateway_reference TEXT NULL,
    request_payload JSONB NULL,
    response_payload JSONB NULL,
    verified_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_payments_bill_id ON payments (bill_id);
CREATE INDEX idx_payments_status_created_at_desc ON payments (status, created_at DESC);
CREATE INDEX idx_payments_gateway_order_id ON payments (gateway_order_id);
CREATE INDEX idx_payments_gateway_txn_id ON payments (gateway_txn_id);
CREATE INDEX idx_payments_gateway_reference ON payments (gateway_reference);

CREATE TABLE commission_ledger (
    id UUID PRIMARY KEY,
    bill_id UUID NOT NULL REFERENCES bills (id),
    bill_item_id UUID NOT NULL REFERENCES bill_items (id),
    staff_id UUID NOT NULL REFERENCES staff (id),
    base_amount BIGINT NOT NULL,
    commission_percent_bps INTEGER NOT NULL,
    commission_amount BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_commission_ledger_bill_id ON commission_ledger (bill_id);
CREATE INDEX idx_commission_ledger_staff_created_at_desc ON commission_ledger (staff_id, created_at DESC);

CREATE TABLE idempotency_keys (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants (id),
    store_id UUID NOT NULL REFERENCES stores (id),
    idempotency_key TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('IN_PROGRESS', 'COMPLETED', 'FAILED')),
    response_bill_id UUID NULL REFERENCES bills (id),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT uq_idempotency_keys_tenant_store_key UNIQUE (tenant_id, store_id, idempotency_key)
);

CREATE TABLE audit_logs (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants (id),
    store_id UUID NULL REFERENCES stores (id),
    entity_type TEXT NOT NULL,
    entity_id UUID NOT NULL,
    action TEXT NOT NULL,
    performed_by_user_id UUID NULL REFERENCES users (id),
    metadata JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_audit_logs_entity ON audit_logs (entity_type, entity_id);
CREATE INDEX idx_audit_logs_created_at_desc ON audit_logs (created_at DESC);
