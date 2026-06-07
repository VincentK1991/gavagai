SELECT
  region AS `region`,
  tier AS `tier`,
  SUM(orders.amount) AS `revenue`,
  COUNT(DISTINCT orders.order_id) AS `order_count`
FROM `my_project.analytics.orders` AS `orders`
LEFT JOIN `my_project.analytics.customers` AS `customers`
  ON `orders`.`customer_id` = `customers`.`customer_id`
WHERE status = 'complete'
  AND tier IN ('gold', 'silver')
GROUP BY region, tier
HAVING SUM(orders.amount) >= 500
ORDER BY `revenue` DESC
LIMIT 20
