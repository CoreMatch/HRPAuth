CREATE TABLE IF NOT EXISTS `oauth2_clients` (
  `id` int unsigned NOT NULL AUTO_INCREMENT,
  `client_id` varchar(100) COLLATE utf8mb4_unicode_ci NOT NULL,
  `client_secret` varchar(255) COLLATE utf8mb4_unicode_ci DEFAULT NULL,
  `name` varchar(255) COLLATE utf8mb4_unicode_ci NOT NULL,
  `type` enum('public','confidential') COLLATE utf8mb4_unicode_ci NOT NULL,
  `grant_types` text COLLATE utf8mb4_unicode_ci NOT NULL,
  `redirect_uris` text COLLATE utf8mb4_unicode_ci NOT NULL,
  `scopes` text COLLATE utf8mb4_unicode_ci NOT NULL,
  `is_internal` tinyint(1) NOT NULL DEFAULT 0,
  `is_super` tinyint(1) NOT NULL DEFAULT 0,
  `is_active` tinyint(1) NOT NULL DEFAULT 1,
  `created_at` timestamp NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` timestamp NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_oauth2_clients_client_id` (`client_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `oauth2_authorization_codes` (
  `id` int unsigned NOT NULL AUTO_INCREMENT,
  `code` varchar(255) COLLATE utf8mb4_unicode_ci NOT NULL,
  `client_id` varchar(100) COLLATE utf8mb4_unicode_ci NOT NULL,
  `user_id` varchar(32) COLLATE utf8mb4_unicode_ci NOT NULL,
  `redirect_uri` text COLLATE utf8mb4_unicode_ci NOT NULL,
  `scopes` text COLLATE utf8mb4_unicode_ci NOT NULL,
  `code_challenge` varchar(255) COLLATE utf8mb4_unicode_ci NOT NULL,
  `code_challenge_method` varchar(20) COLLATE utf8mb4_unicode_ci NOT NULL,
  `expires_at` timestamp NOT NULL,
  `consumed_at` timestamp NULL DEFAULT NULL,
  `created_at` timestamp NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_oauth2_authorization_codes_code` (`code`),
  KEY `idx_oauth2_authorization_codes_client_id` (`client_id`),
  KEY `idx_oauth2_authorization_codes_user_id` (`user_id`),
  KEY `idx_oauth2_authorization_codes_expires_at` (`expires_at`),
  CONSTRAINT `oauth2_authorization_codes_ibfk_1` FOREIGN KEY (`user_id`) REFERENCES `users` (`uuid`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `oauth2_access_tokens` (
  `id` int unsigned NOT NULL AUTO_INCREMENT,
  `access_token` varchar(255) COLLATE utf8mb4_unicode_ci NOT NULL,
  `client_id` varchar(100) COLLATE utf8mb4_unicode_ci NOT NULL,
  `user_id` varchar(32) COLLATE utf8mb4_unicode_ci DEFAULT NULL,
  `scopes` text COLLATE utf8mb4_unicode_ci NOT NULL,
  `subject_type` enum('user','service') COLLATE utf8mb4_unicode_ci NOT NULL,
  `target_uid` int unsigned DEFAULT NULL,
  `target_email` varchar(255) COLLATE utf8mb4_unicode_ci DEFAULT NULL,
  `expires_at` timestamp NOT NULL,
  `revoked_at` timestamp NULL DEFAULT NULL,
  `created_at` timestamp NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_oauth2_access_tokens_access_token` (`access_token`),
  KEY `idx_oauth2_access_tokens_client_id` (`client_id`),
  KEY `idx_oauth2_access_tokens_user_id` (`user_id`),
  KEY `idx_oauth2_access_tokens_target_uid` (`target_uid`),
  KEY `idx_oauth2_access_tokens_target_email` (`target_email`),
  KEY `idx_oauth2_access_tokens_expires_at` (`expires_at`),
  CONSTRAINT `oauth2_access_tokens_ibfk_1` FOREIGN KEY (`user_id`) REFERENCES `users` (`uuid`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `oauth2_refresh_tokens` (
  `id` int unsigned NOT NULL AUTO_INCREMENT,
  `refresh_token` varchar(255) COLLATE utf8mb4_unicode_ci NOT NULL,
  `access_token_id` int unsigned NOT NULL,
  `client_id` varchar(100) COLLATE utf8mb4_unicode_ci NOT NULL,
  `user_id` varchar(32) COLLATE utf8mb4_unicode_ci NOT NULL,
  `scopes` text COLLATE utf8mb4_unicode_ci NOT NULL,
  `expires_at` timestamp NOT NULL,
  `revoked_at` timestamp NULL DEFAULT NULL,
  `created_at` timestamp NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_oauth2_refresh_tokens_refresh_token` (`refresh_token`),
  KEY `idx_oauth2_refresh_tokens_access_token_id` (`access_token_id`),
  KEY `idx_oauth2_refresh_tokens_client_id` (`client_id`),
  KEY `idx_oauth2_refresh_tokens_user_id` (`user_id`),
  KEY `idx_oauth2_refresh_tokens_expires_at` (`expires_at`),
  CONSTRAINT `oauth2_refresh_tokens_ibfk_1` FOREIGN KEY (`access_token_id`) REFERENCES `oauth2_access_tokens` (`id`) ON DELETE CASCADE,
  CONSTRAINT `oauth2_refresh_tokens_ibfk_2` FOREIGN KEY (`user_id`) REFERENCES `users` (`uuid`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
