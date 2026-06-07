SELECT
  region AS "region",
  status AS "status",
  SUM(orders.amount) AS "revenue",
  COUNT(DISTINCT orders.order_id) AS "order_count",
  AVG(orders.amount) AS "avg_order_value"
FROM analytics.orders AS "orders"
GROUP BY region, status
ORDER BY "revenue" DESC, "order_count" ASC
