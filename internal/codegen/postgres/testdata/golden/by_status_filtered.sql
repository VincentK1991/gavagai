SELECT
  status AS "status",
  region AS "region",
  SUM(orders.amount) AS "revenue",
  COUNT(DISTINCT orders.order_id) AS "order_count"
FROM analytics.orders AS "orders"
WHERE status IN ('complete', 'pending')
  AND region != 'APAC'
GROUP BY status, region
ORDER BY "revenue" DESC
LIMIT 50
