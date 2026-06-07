SELECT
  region AS "region",
  SUM(orders.amount) AS "revenue"
FROM analytics.orders AS "orders"
GROUP BY region
LIMIT 100
