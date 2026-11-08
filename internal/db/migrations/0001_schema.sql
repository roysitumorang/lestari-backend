-- Lestari schema (PostgreSQL 18+). Keys are UUID v7 from uuidv7(): time-ordered, so they index well.
-- Money is NUMERIC(18,2); quantities NUMERIC(18,3).
-- Tenancy: stores own branches; operational data is scoped by branch_id.

CREATE TABLE stores (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    name                TEXT NOT NULL,
    plan                TEXT NOT NULL DEFAULT 'free',
    plan_expires_at     TIMESTAMPTZ,
    coin_balance        NUMERIC(18,2) NOT NULL DEFAULT 0,
    reward_balance      NUMERIC(18,2) NOT NULL DEFAULT 0,
    referral_code       TEXT UNIQUE,
    referred_by_store_id UUID REFERENCES stores(id),
    storage_quota_mb    NUMERIC(12,2) NOT NULL DEFAULT 500,
    storage_extra_expires_at DATE,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE branches (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    store_id                UUID NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    name                    TEXT NOT NULL,
    code                    TEXT,
    manager_name            TEXT,
    phone                   TEXT,
    email                   TEXT,
    address                 TEXT,
    is_head_office          BOOLEAN NOT NULL DEFAULT false,
    subscription_expires_at TIMESTAMPTZ,
    deleted_at              TIMESTAMPTZ,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX ON branches (store_id);

CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    store_id            UUID NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    branch_id           UUID REFERENCES branches(id),
    name                TEXT NOT NULL,
    username            TEXT,
    email               TEXT NOT NULL,
    password_hash       TEXT NOT NULL,
    phone               TEXT,
    phone_verified      BOOLEAN NOT NULL DEFAULT false,
    role                TEXT NOT NULL CHECK (role IN ('owner','administrator','manager','cashier')),
    address             TEXT,
    birth_year          INT,
    gender              TEXT,
    avatar_url          TEXT,
    ppob_pin_hash       TEXT,
    status              TEXT NOT NULL DEFAULT 'active',
    permissions         TEXT[] NOT NULL DEFAULT '{}',
    two_factor_secret   TEXT,
    two_factor_pending  TEXT,
    two_factor_enabled  BOOLEAN NOT NULL DEFAULT false,
    recovery_codes      TEXT[] NOT NULL DEFAULT '{}',
    notifications       JSONB NOT NULL DEFAULT '{}',
    last_login_at       TIMESTAMPTZ,
    deleted_at          TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX users_email_uq ON users (lower(email)) WHERE deleted_at IS NULL;
CREATE INDEX ON users (store_id);

CREATE TABLE refresh_tokens (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash  TEXT NOT NULL UNIQUE,
    expires_at  TIMESTAMPTZ NOT NULL,
    revoked_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE activity_logs (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    store_id    UUID NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    user_id     UUID REFERENCES users(id) ON DELETE SET NULL,
    action      TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    ip_address  TEXT NOT NULL DEFAULT '',
    user_agent  TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX ON activity_logs (user_id, created_at DESC);

CREATE TABLE otp_codes (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    purpose     TEXT NOT NULL,
    target      TEXT NOT NULL DEFAULT '',
    code_hash   TEXT NOT NULL,
    attempts    INT NOT NULL DEFAULT 0,
    expires_at  TIMESTAMPTZ NOT NULL,
    used_at     TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE uploads (
    id          UUID PRIMARY KEY DEFAULT uuidv7(),
    store_id    UUID NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    path        TEXT NOT NULL,
    url         TEXT NOT NULL,
    mime_type   TEXT NOT NULL,
    size        BIGINT NOT NULL,
    purpose     TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE settings (
    branch_id   UUID NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    key         TEXT NOT NULL,
    value       JSONB NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (branch_id, key)
);

-- ---------- master data ----------
CREATE TABLE categories (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    branch_id   UUID NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    color       TEXT,
    sort_order  INT,
    description TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX ON categories (branch_id);

CREATE TABLE taxes (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    branch_id    UUID NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    rate         NUMERIC(6,3) NOT NULL,
    is_inclusive BOOLEAN NOT NULL DEFAULT false,
    is_active    BOOLEAN NOT NULL DEFAULT true,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE customer_types (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    branch_id        UUID NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    name             TEXT NOT NULL,
    discount_type    TEXT,
    discount_value   NUMERIC(18,2),
    point_multiplier NUMERIC(8,2) DEFAULT 1,
    description      TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE products (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    branch_id            UUID NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    source_product_id    UUID REFERENCES products(id) ON DELETE SET NULL,
    name                 TEXT NOT NULL,
    item_type            TEXT NOT NULL DEFAULT 'product',
    sku                  TEXT NOT NULL,
    barcode              TEXT,
    category_id          UUID REFERENCES categories(id) ON DELETE SET NULL,
    unit                 TEXT,
    selling_price        NUMERIC(18,2) NOT NULL DEFAULT 0,
    purchase_price       NUMERIC(18,2) NOT NULL DEFAULT 0,
    price_type           TEXT NOT NULL DEFAULT 'single',
    tax_id               UUID REFERENCES taxes(id) ON DELETE SET NULL,
    discount_type        TEXT,
    discount_value       NUMERIC(18,2),
    wholesale_prices     JSONB NOT NULL DEFAULT '[]',
    customer_type_prices JSONB NOT NULL DEFAULT '[]',
    track_stock          BOOLEAN NOT NULL DEFAULT true,
    stock                NUMERIC(18,3) NOT NULL DEFAULT 0,
    minimum_stock        NUMERIC(18,3),
    shelf_location       TEXT,
    weight_grams         NUMERIC(12,2),
    track_imei           BOOLEAN NOT NULL DEFAULT false,
    units                JSONB NOT NULL DEFAULT '[]',
    variants             JSONB NOT NULL DEFAULT '[]',
    ingredients          JSONB NOT NULL DEFAULT '[]',
    points_earned        NUMERIC(12,2),
    points_to_redeem     NUMERIC(12,2),
    show_in_online_store BOOLEAN NOT NULL DEFAULT true,
    is_available         BOOLEAN NOT NULL DEFAULT true,
    description          TEXT,
    online_description   TEXT,
    image_url            TEXT,
    deleted_at           TIMESTAMPTZ,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX products_sku_uq ON products (branch_id, lower(sku)) WHERE deleted_at IS NULL;
CREATE INDEX ON products (branch_id, category_id);

CREATE TABLE stock_movements (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    branch_id       UUID NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    product_id      UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    type            TEXT NOT NULL,
    reference       TEXT NOT NULL DEFAULT '',
    quantity_change NUMERIC(18,3) NOT NULL,
    stock_after     NUMERIC(18,3) NOT NULL,
    unit_cost       NUMERIC(18,2),
    actor_id        UUID REFERENCES users(id) ON DELETE SET NULL,
    actor_name      TEXT NOT NULL DEFAULT '',
    note            TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX ON stock_movements (product_id, created_at DESC);

CREATE TABLE customers (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    branch_id        UUID NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    name             TEXT NOT NULL,
    phone            TEXT,
    email            TEXT,
    code             TEXT,
    customer_type_id UUID REFERENCES customer_types(id) ON DELETE SET NULL,
    birth_date       DATE,
    points           INT NOT NULL DEFAULT 0,
    address          TEXT,
    note             TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX ON customers (branch_id);
CREATE UNIQUE INDEX customers_code_uq ON customers (branch_id, code) WHERE code IS NOT NULL AND code <> '';

CREATE TABLE point_ledger (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    customer_id   UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    type          TEXT NOT NULL,
    points        INT NOT NULL,
    balance_after INT NOT NULL,
    reference     TEXT,
    note          TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE suppliers (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    branch_id      UUID NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    name           TEXT NOT NULL,
    contact_person TEXT,
    phone          TEXT,
    email          TEXT,
    code           TEXT,
    address        TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE discounts (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    branch_id  UUID NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    type       TEXT NOT NULL,
    value      NUMERIC(18,2) NOT NULL,
    is_active  BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE fees (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    branch_id   UUID NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    amount_type TEXT NOT NULL DEFAULT 'amount',
    amount      NUMERIC(18,2) NOT NULL,
    applies_to  TEXT NOT NULL DEFAULT 'sale',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE cash_drawers (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    branch_id  UUID NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    is_active  BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE promotions (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    branch_id         UUID NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    name              TEXT NOT NULL,
    type              TEXT NOT NULL,
    start_date        DATE NOT NULL,
    end_date          DATE NOT NULL,
    start_time        TEXT,
    end_time          TEXT,
    days              TEXT[] NOT NULL DEFAULT '{}',
    customer_type_ids UUID[] NOT NULL DEFAULT '{}',
    discount_type     TEXT,
    discount_value    NUMERIC(18,2),
    minimum_purchase  NUMERIC(18,2),
    buy_quantity      NUMERIC(12,2),
    get_quantity      NUMERIC(12,2),
    products          JSONB NOT NULL DEFAULT '[]',
    is_active         BOOLEAN NOT NULL DEFAULT true,
    description       TEXT,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE commissions (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    branch_id   UUID NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    basis       TEXT NOT NULL,
    type        TEXT NOT NULL,
    value       NUMERIC(18,2) NOT NULL,
    staff_ids   UUID[] NOT NULL DEFAULT '{}',
    product_ids UUID[] NOT NULL DEFAULT '{}',
    is_active   BOOLEAN NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE payment_methods (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    branch_id              UUID NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    name                   TEXT NOT NULL,
    type                   TEXT NOT NULL,
    code                   TEXT,
    mdr_type               TEXT,
    mdr_value              NUMERIC(12,3),
    charge_fee_to_customer BOOLEAN NOT NULL DEFAULT false,
    edc_card_rates         JSONB NOT NULL DEFAULT '[]',
    is_active              BOOLEAN NOT NULL DEFAULT true,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ---------- stock opname ----------
CREATE TABLE stock_opnames (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    branch_id         UUID NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    code              TEXT NOT NULL,
    opname_date       DATE NOT NULL,
    performed_by_name TEXT NOT NULL,
    performed_by_role TEXT NOT NULL,
    status            TEXT NOT NULL DEFAULT 'pending',
    note              TEXT,
    reject_reason     TEXT,
    decided_at        TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE stock_opname_items (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    opname_id      UUID NOT NULL REFERENCES stock_opnames(id) ON DELETE CASCADE,
    product_id     UUID REFERENCES products(id) ON DELETE SET NULL,
    product_name   TEXT NOT NULL,
    system_stock   NUMERIC(18,3) NOT NULL,
    physical_stock NUMERIC(18,3) NOT NULL,
    unit_cost      NUMERIC(18,2) NOT NULL DEFAULT 0,
    note           TEXT
);

-- ---------- cash shifts ----------
CREATE TABLE shifts (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    branch_id      UUID NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    cash_drawer_id UUID REFERENCES cash_drawers(id) ON DELETE SET NULL,
    cashier_id     UUID REFERENCES users(id) ON DELETE SET NULL,
    opened_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    closed_at      TIMESTAMPTZ,
    opening_cash   NUMERIC(18,2) NOT NULL DEFAULT 0,
    cash_in        NUMERIC(18,2) NOT NULL DEFAULT 0,
    cash_out       NUMERIC(18,2) NOT NULL DEFAULT 0,
    actual_cash    NUMERIC(18,2)
);

-- ---------- sales ----------
CREATE TABLE sales (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    branch_id       UUID NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    receipt_number  TEXT NOT NULL,
    cashier_id      UUID REFERENCES users(id) ON DELETE SET NULL,
    cashier_name    TEXT NOT NULL DEFAULT '',
    customer_id     UUID REFERENCES customers(id) ON DELETE SET NULL,
    shift_id        UUID REFERENCES shifts(id) ON DELETE SET NULL,
    payment_method  TEXT NOT NULL DEFAULT 'cash',
    payment_type    TEXT NOT NULL DEFAULT 'cash',
    subtotal        NUMERIC(18,2) NOT NULL DEFAULT 0,
    discount_total  NUMERIC(18,2) NOT NULL DEFAULT 0,
    tax_id          UUID REFERENCES taxes(id) ON DELETE SET NULL,
    tax_name        TEXT,
    tax_rate        NUMERIC(6,3),
    tax_total       NUMERIC(18,2) NOT NULL DEFAULT 0,
    service_charge  NUMERIC(18,2) NOT NULL DEFAULT 0,
    fee_total       NUMERIC(18,2) NOT NULL DEFAULT 0,
    rounding        NUMERIC(18,2) NOT NULL DEFAULT 0,
    grand_total     NUMERIC(18,2) NOT NULL DEFAULT 0,
    paid_amount     NUMERIC(18,2) NOT NULL DEFAULT 0,
    change_amount   NUMERIC(18,2) NOT NULL DEFAULT 0,
    cost_total      NUMERIC(18,2) NOT NULL DEFAULT 0,
    profit          NUMERIC(18,2) NOT NULL DEFAULT 0,
    mdr_rate        NUMERIC(8,3) NOT NULL DEFAULT 0,
    mdr_amount      NUMERIC(18,2) NOT NULL DEFAULT 0,
    commission_total NUMERIC(18,2) NOT NULL DEFAULT 0,
    points_earned   INT NOT NULL DEFAULT 0,
    points_redeemed INT NOT NULL DEFAULT 0,
    promotion_id    UUID REFERENCES promotions(id) ON DELETE SET NULL,
    label           TEXT,
    customer_type   TEXT,
    note            TEXT,
    status          TEXT NOT NULL DEFAULT 'completed',
    due_date        DATE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX ON sales (branch_id, receipt_number);
CREATE INDEX ON sales (branch_id, created_at);

CREATE TABLE sale_items (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    sale_id         UUID NOT NULL REFERENCES sales(id) ON DELETE CASCADE,
    product_id      UUID REFERENCES products(id) ON DELETE SET NULL,
    product_name    TEXT NOT NULL,
    variant_name    TEXT,
    category_id     UUID,
    quantity        NUMERIC(18,3) NOT NULL,
    unit_price      NUMERIC(18,2) NOT NULL,
    unit_cost       NUMERIC(18,2) NOT NULL DEFAULT 0,
    discount_amount NUMERIC(18,2) NOT NULL DEFAULT 0,
    subtotal        NUMERIC(18,2) NOT NULL,
    points_earned   INT NOT NULL DEFAULT 0,
    points_redeemed INT NOT NULL DEFAULT 0,
    promotion_id    UUID REFERENCES promotions(id) ON DELETE SET NULL,
    promotion_discount NUMERIC(18,2) NOT NULL DEFAULT 0
);
CREATE INDEX ON sale_items (sale_id);
CREATE INDEX ON sale_items (product_id);

CREATE TABLE sale_fees (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    sale_id  UUID NOT NULL REFERENCES sales(id) ON DELETE CASCADE,
    fee_name TEXT NOT NULL,
    amount   NUMERIC(18,2) NOT NULL
);

-- ---------- purchases ----------
CREATE TABLE purchases (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    branch_id      UUID NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    receipt_number TEXT NOT NULL,
    supplier_id    UUID REFERENCES suppliers(id) ON DELETE SET NULL,
    staff_id       UUID REFERENCES users(id) ON DELETE SET NULL,
    staff_name     TEXT NOT NULL DEFAULT '',
    purchase_date  DATE NOT NULL,
    invoice_number TEXT,
    payment_type   TEXT NOT NULL DEFAULT 'cash',
    payment_method TEXT,
    due_date       DATE,
    subtotal       NUMERIC(18,2) NOT NULL DEFAULT 0,
    discount_total NUMERIC(18,2) NOT NULL DEFAULT 0,
    tax_total      NUMERIC(18,2) NOT NULL DEFAULT 0,
    fee_total      NUMERIC(18,2) NOT NULL DEFAULT 0,
    grand_total    NUMERIC(18,2) NOT NULL DEFAULT 0,
    paid_amount    NUMERIC(18,2) NOT NULL DEFAULT 0,
    status         TEXT NOT NULL DEFAULT 'completed',
    note           TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX ON purchases (branch_id, receipt_number);

CREATE TABLE purchase_items (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    purchase_id     UUID NOT NULL REFERENCES purchases(id) ON DELETE CASCADE,
    product_id      UUID REFERENCES products(id) ON DELETE SET NULL,
    product_name    TEXT NOT NULL,
    quantity        NUMERIC(18,3) NOT NULL,
    unit            TEXT,
    unit_cost       NUMERIC(18,2) NOT NULL,
    discount_amount NUMERIC(18,2) NOT NULL DEFAULT 0,
    subtotal        NUMERIC(18,2) NOT NULL
);
CREATE INDEX ON purchase_items (purchase_id);

-- ---------- cash in / out ----------
CREATE TABLE finance_entries (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    branch_id      UUID NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    entry_date     DATE NOT NULL,
    type           TEXT NOT NULL CHECK (type IN ('income','expense')),
    category       TEXT NOT NULL,
    amount         NUMERIC(18,2) NOT NULL,
    payment_method TEXT,
    cash_drawer_id UUID REFERENCES cash_drawers(id) ON DELETE SET NULL,
    description    TEXT,
    attachment_url TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ---------- debts (payables + receivables) ----------
CREATE TABLE debts (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    branch_id        UUID NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    kind             TEXT NOT NULL CHECK (kind IN ('payable','receivable')),
    supplier_id      UUID REFERENCES suppliers(id) ON DELETE SET NULL,
    customer_id      UUID REFERENCES customers(id) ON DELETE SET NULL,
    purchase_id      UUID REFERENCES purchases(id) ON DELETE CASCADE,
    sale_id          UUID REFERENCES sales(id) ON DELETE CASCADE,
    reference_number TEXT NOT NULL,
    transaction_date DATE NOT NULL,
    due_date         DATE,
    total_amount     NUMERIC(18,2) NOT NULL,
    paid_amount      NUMERIC(18,2) NOT NULL DEFAULT 0,
    status           TEXT NOT NULL DEFAULT 'unpaid',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE debt_payments (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    debt_id        UUID NOT NULL REFERENCES debts(id) ON DELETE CASCADE,
    payment_date   DATE NOT NULL,
    amount         NUMERIC(18,2) NOT NULL,
    payment_method TEXT NOT NULL,
    note           TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ---------- returns ----------
CREATE TABLE returns (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    branch_id     UUID NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    kind          TEXT NOT NULL CHECK (kind IN ('sale','purchase')),
    return_number TEXT NOT NULL,
    sale_id       UUID REFERENCES sales(id) ON DELETE SET NULL,
    purchase_id   UUID REFERENCES purchases(id) ON DELETE SET NULL,
    refund_method TEXT,
    refund_amount NUMERIC(18,2) NOT NULL DEFAULT 0,
    reason        TEXT,
    cashier_name  TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE return_items (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    return_id    UUID NOT NULL REFERENCES returns(id) ON DELETE CASCADE,
    product_id   UUID REFERENCES products(id) ON DELETE SET NULL,
    product_name TEXT NOT NULL,
    quantity     NUMERIC(18,3) NOT NULL,
    unit_price   NUMERIC(18,2) NOT NULL,
    subtotal     NUMERIC(18,2) NOT NULL
);

-- ---------- stock transfers ----------
CREATE TABLE stock_transfers (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    store_id              UUID NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    transfer_number       TEXT NOT NULL,
    from_branch_id        UUID NOT NULL REFERENCES branches(id),
    to_branch_id          UUID NOT NULL REFERENCES branches(id),
    transfer_date         DATE NOT NULL,
    requires_confirmation BOOLEAN NOT NULL DEFAULT false,
    status                TEXT NOT NULL,
    note                  TEXT,
    reject_reason         TEXT,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE stock_transfer_items (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    transfer_id       UUID NOT NULL REFERENCES stock_transfers(id) ON DELETE CASCADE,
    product_id        UUID REFERENCES products(id) ON DELETE SET NULL,
    product_name      TEXT NOT NULL,
    sku               TEXT,
    quantity          NUMERIC(18,3) NOT NULL,
    received_quantity NUMERIC(18,3),
    note              TEXT
);

-- ---------- PPOB ----------
CREATE TABLE ppob_prices (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    branch_id      UUID NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    kind           TEXT NOT NULL CHECK (kind IN ('prepaid','postpaid')),
    category       TEXT NOT NULL,
    provider       TEXT,
    product_code   TEXT NOT NULL,
    product_name   TEXT NOT NULL,
    base_price     NUMERIC(18,2),
    selling_price  NUMERIC(18,2),
    base_admin_fee NUMERIC(18,2),
    admin_fee      NUMERIC(18,2),
    is_active      BOOLEAN NOT NULL DEFAULT true,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (branch_id, kind, product_code)
);
CREATE TABLE ppob_transactions (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    branch_id       UUID NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    reference_id    TEXT NOT NULL,
    product_name    TEXT NOT NULL,
    customer_number TEXT NOT NULL,
    base_price      NUMERIC(18,2) NOT NULL,
    selling_price   NUMERIC(18,2) NOT NULL,
    cashier_id      UUID REFERENCES users(id) ON DELETE SET NULL,
    cashier_name    TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'success',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE ppob_deposits (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    branch_id            UUID NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    reference_id         TEXT NOT NULL,
    type                 TEXT NOT NULL DEFAULT 'top_up',
    payment_method       TEXT,
    amount               NUMERIC(18,2) NOT NULL,
    admin_fee            NUMERIC(18,2) NOT NULL DEFAULT 0,
    balance_after        NUMERIC(18,2),
    status               TEXT NOT NULL DEFAULT 'pending',
    payment_url          TEXT,
    payment_instructions TEXT,
    payment_proof_url    TEXT,
    expires_at           TIMESTAMPTZ,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ---------- billing ----------
CREATE TABLE invoices (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    store_id             UUID NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    invoice_number       TEXT NOT NULL UNIQUE,
    kind                 TEXT NOT NULL,
    description          TEXT NOT NULL,
    payment_method       TEXT NOT NULL,
    amount               NUMERIC(18,2) NOT NULL,
    admin_fee            NUMERIC(18,2) NOT NULL DEFAULT 0,
    status               TEXT NOT NULL DEFAULT 'pending',
    payment_url          TEXT,
    payment_instructions TEXT,
    payment_proof_url    TEXT,
    sender_account_name  TEXT,
    meta                 JSONB NOT NULL DEFAULT '{}',
    expires_at           TIMESTAMPTZ,
    paid_at              TIMESTAMPTZ,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE store_plugins (
    store_id   UUID NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    code       TEXT NOT NULL,
    status     TEXT NOT NULL,
    trial_used BOOLEAN NOT NULL DEFAULT false,
    expires_at TIMESTAMPTZ,
    PRIMARY KEY (store_id, code)
);
CREATE TABLE mission_claims (
    store_id   UUID NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    code       TEXT NOT NULL,
    claimed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (store_id, code)
);
CREATE TABLE vouchers (
    code          TEXT PRIMARY KEY,
    reward_type   TEXT NOT NULL,
    reward_amount NUMERIC(18,2) NOT NULL,
    max_uses      INT NOT NULL DEFAULT 1,
    used_count    INT NOT NULL DEFAULT 0,
    expires_at    TIMESTAMPTZ
);
CREATE TABLE voucher_redemptions (
    code       TEXT NOT NULL REFERENCES vouchers(code),
    store_id   UUID NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (code, store_id)
);
CREATE TABLE store_integrations (
    store_id     UUID NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    code         TEXT NOT NULL,
    status       TEXT NOT NULL,
    settings     JSONB NOT NULL DEFAULT '{}',
    connected_at TIMESTAMPTZ,
    PRIMARY KEY (store_id, code)
);
CREATE TABLE loan_applications (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    store_id      UUID NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    provider_code TEXT NOT NULL,
    amount        NUMERIC(18,2) NOT NULL,
    tenor_months  INT NOT NULL,
    purpose       TEXT NOT NULL,
    status        TEXT NOT NULL DEFAULT 'submitted',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE deactivation_requests (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    store_id   UUID NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    status     TEXT NOT NULL DEFAULT 'pending',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ---------- staff ----------
CREATE TABLE staff_attendances (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    branch_id           UUID NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    staff_id            UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    date                DATE NOT NULL,
    clock_in_at         TIMESTAMPTZ,
    clock_out_at        TIMESTAMPTZ,
    clock_in_photo_url  TEXT,
    clock_out_photo_url TEXT,
    location            TEXT,
    status              TEXT NOT NULL DEFAULT 'present',
    note                TEXT
);
CREATE TABLE help_accesses (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    store_id       UUID NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    platform       TEXT NOT NULL,
    account_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    duration_hours INT NOT NULL,
    description    TEXT NOT NULL,
    status         TEXT NOT NULL DEFAULT 'active',
    expires_at     TIMESTAMPTZ NOT NULL,
    revoked_at     TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ---------- online catalog ----------
CREATE TABLE catalog_orders (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    branch_id         UUID NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    order_number      TEXT NOT NULL,
    customer_name     TEXT NOT NULL,
    customer_phone    TEXT,
    shipping_address  TEXT,
    shipping_method   TEXT,
    courier           TEXT,
    tracking_number   TEXT,
    payment_method    TEXT NOT NULL DEFAULT 'bank_transfer',
    payment_proof_url TEXT,
    subtotal          NUMERIC(18,2) NOT NULL DEFAULT 0,
    shipping_fee      NUMERIC(18,2) NOT NULL DEFAULT 0,
    grand_total       NUMERIC(18,2) NOT NULL DEFAULT 0,
    customer_note     TEXT,
    status            TEXT NOT NULL DEFAULT 'unpaid',
    status_reason     TEXT,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE catalog_order_items (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    order_id     UUID NOT NULL REFERENCES catalog_orders(id) ON DELETE CASCADE,
    product_id   UUID REFERENCES products(id) ON DELETE SET NULL,
    product_name TEXT NOT NULL,
    quantity     NUMERIC(18,3) NOT NULL,
    unit_price   NUMERIC(18,2) NOT NULL,
    subtotal     NUMERIC(18,2) NOT NULL
);
CREATE TABLE catalog_payment_accounts (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    branch_id      UUID NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    type           TEXT NOT NULL,
    provider       TEXT NOT NULL,
    account_name   TEXT NOT NULL,
    account_number TEXT,
    qris_image_url TEXT,
    is_active      BOOLEAN NOT NULL DEFAULT true,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ---------- engagement ----------
CREATE TABLE store_ratings (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    branch_id     UUID NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    sale_id       UUID REFERENCES sales(id) ON DELETE SET NULL,
    customer_name TEXT,
    rating        INT NOT NULL CHECK (rating BETWEEN 1 AND 5),
    comment       TEXT,
    tags          TEXT[] NOT NULL DEFAULT '{}',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE link_page_visits (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    branch_id  UUID NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    ip_address TEXT NOT NULL,
    referrer   TEXT,
    visited_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE notifications (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    store_id       UUID NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    branch_id      UUID REFERENCES branches(id) ON DELETE CASCADE,
    source         TEXT NOT NULL DEFAULT 'back_office',
    title          TEXT NOT NULL,
    message        TEXT NOT NULL,
    link_route     TEXT,
    attachment_url TEXT,
    read_at        TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX ON notifications (store_id, created_at DESC);

-- ---------- regions ----------
CREATE TABLE provinces (
    id           UUID PRIMARY KEY DEFAULT uuidv7(),
    code         TEXT NOT NULL UNIQUE,   -- Kemendagri province code
    country_code TEXT NOT NULL DEFAULT 'ID',
    name         TEXT NOT NULL
);
CREATE TABLE cities (
    id          UUID PRIMARY KEY DEFAULT uuidv7(),
    province_id UUID NOT NULL REFERENCES provinces(id),
    name        TEXT NOT NULL
);

-- ---------- jobs (background work requested by the API) ----------
CREATE TABLE jobs (
    id          UUID PRIMARY KEY DEFAULT uuidv7(),
    store_id    UUID NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    kind        TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'queued',
    payload     JSONB NOT NULL DEFAULT '{}',
    result      JSONB,
    error       TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ
);
