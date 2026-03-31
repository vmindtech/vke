ALTER TABLE `clusters`
  MODIFY COLUMN `create_state` enum('initial','loadbalancer','floating_ip','security_groups','server_groups','ports','computes','dns','kubeconfig','completed') DEFAULT 'initial';

UPDATE `clusters`
SET `create_state` = LOWER(`create_state`)
WHERE `create_state` IS NOT NULL;

