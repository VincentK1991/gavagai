SELECT
  "customers"."region" AS "region",
  tier AS "tier",
  SUM(orders.amount) AS "revenue",
  COUNT(DISTINCT orders.order_id) AS "order_count"
FROM analytics.orders AS "orders"
LEFT JOIN analytics.customers AS "customers"
  ON "orders"."customer_id" = "customers"."customer_id"
WHERE status = 'complete'
  AND tier IN ('gold', 'silver')
GROUP BY "customers"."region", tier
HAVING SUM(orders.amount) >= 500
ORDER BY "revenue" DESC
LIMIT 20
