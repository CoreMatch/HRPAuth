ALTER TABLE `users`
  ADD COLUMN `locale` varchar(255) COLLATE utf8mb4_unicode_ci DEFAULT NULL,
  ADD COLUMN `score` int DEFAULT NULL,
  ADD COLUMN `is_dark_mode` tinyint(1) NOT NULL DEFAULT '0',
  ADD COLUMN `lastlogin` bigint DEFAULT NULL,
  ADD COLUMN `x` double NOT NULL DEFAULT '0',
  ADD COLUMN `y` double NOT NULL DEFAULT '0',
  ADD COLUMN `z` double NOT NULL DEFAULT '0',
  ADD COLUMN `world` varchar(255) COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT 'world',
  ADD COLUMN `regdate` bigint NOT NULL DEFAULT '0',
  ADD COLUMN `yaw` double(8,2) DEFAULT NULL,
  ADD COLUMN `pitch` double(8,2) DEFAULT NULL,
  ADD COLUMN `isLogged` smallint NOT NULL DEFAULT '0',
  ADD COLUMN `hasSession` smallint NOT NULL DEFAULT '0';
