-- Remove legacy remember_token column from users table.
-- The remember_token authentication mechanism has been fully replaced by OAuth2.
ALTER TABLE `users` DROP COLUMN `remember_token`;
