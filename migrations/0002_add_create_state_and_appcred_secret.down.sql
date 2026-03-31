ALTER TABLE `clusters`
  DROP COLUMN IF EXISTS `create_state`,
  DROP COLUMN IF EXISTS `create_request`,
  DROP COLUMN IF EXISTS `application_credential_secret_enc`;

