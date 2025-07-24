-- Add resources table for cluster resource tracking
-- This migration adds the resources table to track cluster-related resources

CREATE TABLE IF NOT EXISTS `resources` (
  `id` int NOT NULL AUTO_INCREMENT,
  `cluster_uuid` varchar(36) DEFAULT NULL,
  `resource_type` varchar(30) DEFAULT NULL,
  `resource_uuid` varchar(36) DEFAULT NULL,
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- Add comment to table
ALTER TABLE `resources` COMMENT = 'Stores cluster-related resources for tracking and management'; 