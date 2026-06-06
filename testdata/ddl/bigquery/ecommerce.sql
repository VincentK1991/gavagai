-- E-commerce schema for gavagai integration tests (BigQuery).
-- Used by semantic model: internal/model/testdata/ecommerce_bigquery.yaml
-- Project: my_project  Dataset: analytics

CREATE SCHEMA IF NOT EXISTS `my_project.analytics`
  OPTIONS (location = 'US');

-- ---------------------------------------------------------------------------
-- customers
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `my_project.analytics.customers` (
    customer_id   INT64   NOT NULL,
    name          STRING  NOT NULL,
    email         STRING  NOT NULL,
    region        STRING,                  -- e.g. EMEA, APAC, AMER
    tier          STRING  NOT NULL,        -- gold | silver | standard
    created_at    TIMESTAMP
)
OPTIONS (description = 'Customer master table');

-- ---------------------------------------------------------------------------
-- orders
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `my_project.analytics.orders` (
    order_id      INT64   NOT NULL,
    customer_id   INT64   NOT NULL,
    order_date    DATE    NOT NULL,
    status        STRING  NOT NULL,        -- pending | complete | cancelled | refunded
    region        STRING,                  -- region where the order was placed
    amount        NUMERIC NOT NULL,
    created_at    TIMESTAMP
)
OPTIONS (
    description   = 'One row per customer order',
    partition_expiration_days = 365
);

-- ---------------------------------------------------------------------------
-- products
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `my_project.analytics.products` (
    product_id    INT64   NOT NULL,
    name          STRING  NOT NULL,
    category      STRING,                  -- electronics | apparel | home | food
    list_price    NUMERIC NOT NULL
)
OPTIONS (description = 'Product catalogue');

-- ---------------------------------------------------------------------------
-- order_items
-- One row per product line within an order.
-- Joining order_items → orders is a many-to-one; aggregating an order-level
-- metric across this join causes fan-out (double-counting).
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `my_project.analytics.order_items` (
    order_item_id INT64   NOT NULL,
    order_id      INT64   NOT NULL,
    product_id    INT64   NOT NULL,
    quantity      INT64   NOT NULL,
    unit_price    NUMERIC NOT NULL,
    line_total    NUMERIC NOT NULL
)
OPTIONS (description = 'Line items within each order');
