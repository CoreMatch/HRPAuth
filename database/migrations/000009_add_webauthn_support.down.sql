DROP TABLE IF EXISTS `webauthn_credentials`;

ALTER TABLE `users`
DROP COLUMN `webauthn_2fa_enabled`;
