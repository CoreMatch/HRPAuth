ALTER TABLE `users` ADD COLUMN `email_2fa_enabled` tinyint(1) NOT NULL DEFAULT 0 AFTER `webauthn_2fa_enabled`;
