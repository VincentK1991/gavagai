-- E-commerce schema for gavagai integration tests (PostgreSQL).
-- Used by semantic model: internal/model/testdata/ecommerce_postgres.yaml

CREATE SCHEMA IF NOT EXISTS analytics;

-- ---------------------------------------------------------------------------
-- customers
-- ---------------------------------------------------------------------------
CREATE TABLE analytics.customers (
    customer_id   BIGSERIAL PRIMARY KEY,
    name          VARCHAR(255)             NOT NULL,
    email         VARCHAR(255)             NOT NULL UNIQUE,
    region        VARCHAR(100),                            -- e.g. EMEA, APAC, AMER
    tier          VARCHAR(50)              NOT NULL DEFAULT 'standard', -- gold | silver | standard
    created_at    TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

-- ---------------------------------------------------------------------------
-- orders
-- ---------------------------------------------------------------------------
CREATE TABLE analytics.orders (
    order_id      BIGSERIAL PRIMARY KEY,
    customer_id   BIGINT                   NOT NULL REFERENCES analytics.customers (customer_id),
    order_date    DATE                     NOT NULL,
    status        VARCHAR(50)              NOT NULL,       -- pending | complete | cancelled | refunded
    region        VARCHAR(100),                            -- region where the order was placed
    amount        NUMERIC(12, 2)           NOT NULL DEFAULT 0,
    created_at    TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX orders_customer_id_idx ON analytics.orders (customer_id);
CREATE INDEX orders_order_date_idx  ON analytics.orders (order_date);

-- ---------------------------------------------------------------------------
-- products
-- ---------------------------------------------------------------------------
CREATE TABLE analytics.products (
    product_id    BIGSERIAL PRIMARY KEY,
    name          VARCHAR(255)  NOT NULL,
    category      VARCHAR(100),                            -- electronics | apparel | home | food
    list_price    NUMERIC(10, 2) NOT NULL
);

-- ---------------------------------------------------------------------------
-- order_items
-- One row per product line within an order.
-- Joining order_items → orders is a many-to-one; aggregating an order-level
-- metric across this join causes fan-out (double-counting).
-- ---------------------------------------------------------------------------
CREATE TABLE analytics.order_items (
    order_item_id BIGSERIAL PRIMARY KEY,
    order_id      BIGINT         NOT NULL REFERENCES analytics.orders   (order_id),
    product_id    BIGINT         NOT NULL REFERENCES analytics.products (product_id),
    quantity      INTEGER        NOT NULL,
    unit_price    NUMERIC(10, 2) NOT NULL,
    line_total    NUMERIC(12, 2) GENERATED ALWAYS AS (quantity * unit_price) STORED
);

CREATE INDEX order_items_order_id_idx   ON analytics.order_items (order_id);
CREATE INDEX order_items_product_id_idx ON analytics.order_items (product_id);
