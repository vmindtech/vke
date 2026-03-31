ALTER TABLE `clusters`
  ADD COLUMN IF NOT EXISTS `cluster_subdomain_hash` varchar(36) DEFAULT NULL;

