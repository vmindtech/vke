ALTER TABLE `clusters`
  ADD COLUMN IF NOT EXISTS `application_credential_secret_enc` text DEFAULT NULL,
  ADD COLUMN IF NOT EXISTS `create_request` json DEFAULT NULL,
  ADD COLUMN IF NOT EXISTS `create_state` enum('initial','loadbalancer','floating_ip','security_groups','server_groups','ports','computes','dns','kubeconfig','completed') DEFAULT 'initial';

