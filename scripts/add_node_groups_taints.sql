-- Add taint support to node_groups table
-- This migration adds the node_group_taints column to support Kubernetes taints

ALTER TABLE `node_groups` 
ADD COLUMN `node_group_taints` json DEFAULT NULL 
AFTER `node_group_labels`;

-- Add comment to column
ALTER TABLE `node_groups` 
MODIFY COLUMN `node_group_taints` json DEFAULT NULL COMMENT 'Kubernetes taints for node group scheduling'; 