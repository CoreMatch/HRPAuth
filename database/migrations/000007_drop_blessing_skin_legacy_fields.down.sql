ALTER TABLE `users`
  ADD COLUMN `locale` varchar(255) COLLATE utf8mb4_unicode_ci DEFAULT NULL,
  ADD COLUMN `score` int DEFAULT NULL,
  ADD COLUMN `is_dark_mode` tinyint(1) NOT NULL DEFAULT '0',
  ADD COLUMN `lastlogin` bigint DEFAULT NULL,
  ADD