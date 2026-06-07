SELECT
  order_date AS "order_date",
  SUM(orders.amount) AS "revenue"
FROM analytics.orders AS "orders"
WHERE status = 'complete'
GROUP BY order_date
ORDER BY "order_date" ASC
LIMIT 365
