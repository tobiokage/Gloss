INSERT INTO tenants (id, name, active, created_at, updated_at)
VALUES (
    '11111111-1111-1111-1111-111111111111',
    'Demo Tenant',
    TRUE,
    NOW(),
    NOW()
);

INSERT INTO stores (id, tenant_id, name, code, location, active, created_at, updated_at)
VALUES (
    '22222222-2222-2222-2222-222222222222',
    '11111111-1111-1111-1111-111111111111',
    'Demo Store',
    'STORE001',
    'Bangalore',
    TRUE,
    NOW(),
    NOW()
);

INSERT INTO users (
    id,
    tenant_id,
    store_id,
    role,
    name,
    email_or_phone,
    password_hash,
    active,
    created_at,
    updated_at
)
VALUES (
    '33333333-3333-3333-3333-333333333333',
    '11111111-1111-1111-1111-111111111111',
    '22222222-2222-2222-2222-222222222222',
    'STORE_MANAGER',
    'Demo Manager',
    'manager@example.com',
    '$2a$10$.xoIIvLtvarehUlVABIMfOUZZJCmQf8l8f8IkhxsaARzFhz4psPbe',
    TRUE,
    NOW(),
    NOW()
);

INSERT INTO staff (id, tenant_id, name, active, created_at, updated_at)
VALUES
    (
        '44444444-4444-4444-4444-444444444444',
        '11111111-1111-1111-1111-111111111111',
        'Aarav',
        TRUE,
        NOW(),
        NOW()
    ),
    (
        '55555555-5555-5555-5555-555555555555',
        '11111111-1111-1111-1111-111111111111',
        'Diya',
        TRUE,
        NOW(),
        NOW()
    );

INSERT INTO staff_store_mapping (id, staff_id, store_id, active, created_at, updated_at)
VALUES
    (
        '66666666-6666-6666-6666-666666666666',
        '44444444-4444-4444-4444-444444444444',
        '22222222-2222-2222-2222-222222222222',
        TRUE,
        NOW(),
        NOW()
    ),
    (
        '77777777-7777-7777-7777-777777777777',
        '55555555-5555-5555-5555-555555555555',
        '22222222-2222-2222-2222-222222222222',
        TRUE,
        NOW(),
        NOW()
    );

INSERT INTO catalogue_items (id, tenant_id, name, category, list_price, active, created_at, updated_at)
VALUES
    (
        '88888888-8888-8888-8888-888888888888',
        '11111111-1111-1111-1111-111111111111',
        'Haircut',
        'Hair',
        49900,
        TRUE,
        NOW(),
        NOW()
    ),
    (
        '99999999-9999-9999-9999-999999999999',
        '11111111-1111-1111-1111-111111111111',
        'Beard Trim',
        'Hair',
        19900,
        TRUE,
        NOW(),
        NOW()
    ),
    (
        'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa',
        '11111111-1111-1111-1111-111111111111',
        'Facial',
        'Skin',
        89900,
        TRUE,
        NOW(),
        NOW()
    );

INSERT INTO store_bill_counters (store_id, last_bill_seq, updated_at)
VALUES (
    '22222222-2222-2222-2222-222222222222',
    0,
    NOW()
);
