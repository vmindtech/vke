-- Add errors table for cluster error logging
-- This migration adds the errors table to track cluster operation errors

CREATE TABLE IF NOT EXISTS `errors` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `cluster_uuid` varchar(36) DEFAULT NULL,
  `error_message` text NOT NULL,
  `created_at` datetime NOT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_cluster_uuid` (`cluster_uuid`),
  KEY `idx_created_at` (`created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- Add comment to table
ALTER TABLE `errors` COMMENT = 'Stores cluster operation errors for monitoring and debugging';