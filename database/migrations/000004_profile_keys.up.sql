CREATE TABLE IF NOT EXISTS `profile_keys` (
  `id` int unsigned NOT NULL AUTO_INCREMENT,
  `user_id` varchar(32) COLLATE utf8mb4_unicode_ci NOT NULL,
  `public_key` text COLLATE utf8mb4_unicode_ci NOT NULL,
  `private_key` text COLLATE utf8mb4_unicode_ci NOT NULL,
  `public_key_signature` text COLLATE utf8mb4_unicode_ci NOT NULL,
  `expires_at` datetime NOT NULL,
  `refreshed_after` datetime NOT NULL,
  `created_at` timestamp NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` timestamp NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_profile_keys_user_id` (`user_id`),
  KEY `idx_profile_keys_expires_at` (`expires_at`),
  CONSTRAINT `profile_keys_ibfk_1` FOREIGN KEY (`user_id`) REFERENCES `users` (`uuid`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;