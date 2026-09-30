DROP INDEX `idx_users_deleted_at` ON `users`;
ALTER TABLE `users` DROP COLUMN `deleted_at`;
