CREATE TABLE `users` (
  `uid` int unsigned NOT NULL,
  `uuid` varchar(32) COLLATE utf8mb4_unicode_ci DEFAULT NULL,
  `email` varchar(255) COLLATE utf8mb4_unicode_ci DEFAULT NULL,
  `locale` varchar(255) COLLATE utf8mb4_unicode_ci DEFAULT NULL,
  `score` int DEFAULT NULL,
  `avatar` varchar(255) COLLATE utf8mb4_unicode_ci DEFAULT NULL,
  `password` varchar(255) COLLATE utf8mb4_unicode_ci NOT NULL,
  `ip` varchar(255) COLLATE utf8mb4_unicode_ci DEFAULT NULL,
  `is_dark_mode` tinyint(1) NOT NULL DEFAULT '0',
  `permission` int NOT NULL DEFAULT '0',
  `last_sign_at` datetime DEFAULT NULL,
  `register_at` datetime DEFAULT NULL,
  `verified` tinyint(1) NOT NULL DEFAULT '0',
  `remember_token` varchar(100) COLLATE utf8mb4_unicode_ci DEFAULT NULL,
  `username` varchar(255) COLLATE utf8mb4_unicode_ci DEFAULT NULL,
  `lastlogin` bigint DEFAULT NULL,
  `x` double NOT NULL DEFAULT '0',
  `y` double NOT NULL DEFAULT '0',
  `z` double NOT NULL DEFAULT '0',
  `world` varchar(255) COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT 'world',
  `regdate` bigint NOT NULL DEFAULT '0',
  `regip` varchar(40) COLLATE utf8mb4_unicode_ci DEFAULT NULL,
  `yaw` double(8,2) DEFAULT NULL,
  `pitch` double(8,2) DEFAULT NULL,
  `isLogged` smallint NOT NULL DEFAULT '0',
  `hasSession` smallint NOT NULL DEFAULT '0',
  `totp` varchar(32) COLLATE utf8mb4_unicode_ci DEFAULT NULL,
  PRIMARY KEY (`uid`),
  KEY `idx_uuid` (`uuid`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE `profiles` (
  `id` varchar(32) COLLATE utf8mb4_unicode_ci NOT NULL,
  `user_id` varchar(32) COLLATE utf8mb4_unicode_ci NOT NULL,
  `name` varchar(16) COLLATE utf8mb4_unicode_ci NOT NULL,
  `model` enum('default','slim') COLLATE utf8mb4_unicode_ci DEFAULT 'default',
  `created_at` timestamp NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` timestamp NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `name` (`name`),
  KEY `user_id` (`user_id`),
  KEY `idx_profiles_name` (`name`),
  CONSTRAINT `profiles_ibfk_1` FOREIGN KEY (`user_id`) REFERENCES `users` (`uuid`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE `profile_properties` (
  `id` int NOT NULL AUTO_INCREMENT,
  `profile_id` varchar(32) COLLATE utf8mb4_unicode_ci NOT NULL,
  `name` varchar(255) COLLATE utf8mb4_unicode_ci NOT NULL,
  `value` text COLLATE utf8mb4_unicode_ci NOT NULL,
  `signature` text COLLATE utf8mb4_unicode_ci,
  PRIMARY KEY (`id`),
  KEY `profile_id` (`profile_id`),
  CONSTRAINT `profile_properties_ibfk_1` FOREIGN KEY (`profile_id`) REFERENCES `profiles` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE `sessions` (
  `id` int NOT NULL AUTO_INCREMENT,
  `profile_id` varchar(32) COLLATE utf8mb4_unicode_ci NOT NULL,
  `server_id` varchar(255) COLLATE utf8mb4_unicode_ci NOT NULL,
  `ip` varchar(45) COLLATE utf8mb4_unicode_ci DEFAULT NULL,
  `created_at` timestamp NULL DEFAULT CURRENT_TIMESTAMP,
  `expires_at` timestamp NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  KEY `profile_id` (`profile_id`),
  KEY `idx_sessions_server_id` (`server_id`),
  KEY `idx_sessions_expires_at` (`expires_at`),
  CONSTRAINT `sessions_ibfk_1` FOREIGN KEY (`profile_id`) REFERENCES `profiles` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE `tokens` (
  `id` int NOT NULL AUTO_INCREMENT,
  `access_token` varchar(255) COLLATE utf8mb4_unicode_ci NOT NULL,
  `client_token` varchar(255) COLLATE utf8mb4_unicode_ci NOT NULL,
  `user_id` varchar(32) COLLATE utf8mb4_unicode_ci NOT NULL,
  `selected_profile_id` varchar(32) COLLATE utf8mb4_unicode_ci DEFAULT NULL,
  `issued_at` bigint NOT NULL,
  `expires_in_days` int DEFAULT '15',
  `state` enum('valid','temporarily_invalid','invalid') COLLATE utf8mb4_unicode_ci DEFAULT 'valid',
  `created_at` timestamp NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `access_token` (`access_token`),
  KEY `user_id` (`user_id`),
  KEY `selected_profile_id` (`selected_profile_id`),
  KEY `idx_tokens_access_token` (`access_token`),
  KEY `idx_tokens_client_token` (`client_token`),
  CONSTRAINT `tokens_ibfk_1` FOREIGN KEY (`user_id`) REFERENCES `users` (`uuid`) ON DELETE CASCADE,
  CONSTRAINT `tokens_ibfk_2` FOREIGN KEY (`selected_profile_id`) REFERENCES `profiles` (`id`) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
