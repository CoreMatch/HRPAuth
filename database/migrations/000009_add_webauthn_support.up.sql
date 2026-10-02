ALTER TABLE `users`
ADD COLUMN IF NOT EXISTS `webauthn_2fa_enabled` tinyint(1) NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS `webauthn_credentials` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `user_id` varchar(32) COLLATE utf8mb4_unicode_ci NOT NULL,
  `name` varchar(255) NOT NULL,
  `credential_id` varchar(1024) NOT NULL,
  `credential_id_hash` char(64) NOT NULL,
  `credential_json` mediumtext NOT NULL,
  `last_used_at` datetime DEFAULT NULL,
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_webauthn_credentials_credential_id_hash` (`credential_id_hash`),
  KEY `idx_webauthn_credentials_user_id` (`user_id`),
  CONSTRAINT `fk_webauthn_credentials_user_id`
    FOREIGN KEY (`user_id`) REFERENCES `users` (`uuid`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
