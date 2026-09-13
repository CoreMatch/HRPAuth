-- Restore the remember_token column (legacy site session token).
ALTER TABLE `users` ADD COLUMN `remember_token` varchar(100) COLLATE utf8mb4_unicode_ci DEFAULT NULL AFTER `verified`;
