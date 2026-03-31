ALTER TABLE `clusters`
  ADD COLUMN `application_credential_secret_enc` text DEFAULT NULL,
  ADD COLUMN `create_request` json DEFAULT NULL,
  ADD COLUMN `create_state` enum('initial','loadbalancer','floating_ip','security_groups','server_groups','ports','computes','dns','kubeconfig','completed') DEFAULT 'initial';

