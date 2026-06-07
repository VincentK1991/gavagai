SELECT
  region AS `region`,
  COUNT(DISTINCT orders.order_id) AS `order_count`
FROM `my_project.analytics.orders` AS `orders`
WHERE region IS NOT NULL
GROUP BY region
