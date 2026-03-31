ALTER TABLE `clusters`
  ADD COLUMN `application_credential_secret_enc` text DEFAULT NULL,
  ADD COLUMN `create_request` json DEFAULT NULL,
  ADD COLUMN `cluster_subdomain_hash` varchar(36) DEFAULT NULL,
  ADD COLUMN `create_state` enum('INITIAL','LOADBALANCER','FLOATING_IP','SECURITY_GROUPS','SERVER_GROUPS','PORTS','COMPUTES','DNS','KUBECONFIG','COMPLETED') DEFAULT 'INITIAL';

