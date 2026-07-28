ALTER TABLE `users`
  DROP INDEX `uk_users_mojang_uuid`,
  DROP COLUMN `mojang_uuid`,
  DROP COLUMN `cbh`;
