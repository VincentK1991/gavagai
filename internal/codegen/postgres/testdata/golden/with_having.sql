SELECT
  region AS "region",
  SUM(orders.amount) AS "revenue"
FROM analytics.orders AS "orders"
GROUP BY region
HAVING SUM(orders.amount) > 1000
ORDER BY "revenue" DESC
