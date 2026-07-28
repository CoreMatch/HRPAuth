ALTER TABLE `tokens`
  MODIFY COLUMN `client_token` varchar(255) COLLATE utf8mb4_unicode_ci DEFAULT NULL;
