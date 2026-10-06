-- Revert: Remove Minecraft-specific logic from Core service

-- 1. Re-add Minecraft-specific columns to users table
ALTER TABLE users ADD COLUMN cbh TINYINT(1) NOT NULL DEFAULT 1;
ALTER TABLE users ADD COLUMN mbe TINYINT(1) NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN mojang_uuid VARCHAR(32) DEFAULT NULL;
CREATE UNIQUE INDEX uk_users_mojang_uuid ON users(mojang_uuid);

-- 2. Re-create Minecraft-specific tables
CREATE TABLE profiles (
    id VARCHAR(32) PRIMARY KEY,
    user_id VARCHAR(32) NOT NULL,
    name VARCHAR(30) NOT NULL UNIQUE,
    model ENUM('default', 'slim') DEFAULT 'default',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_profiles_user_id (user_id)
);

CREATE TABLE profile_properties (
    id INT AUTO_INCREMENT PRIMARY KEY,
    profile_id VARCHAR(32) NOT NULL,
    name VARCHAR(255) NOT NULL,
    value TEXT NOT NULL,
    signature TEXT,
    delete_when BIGINT NOT NULL DEFAULT 0,
    INDEX idx_profile_properties_profile_id (profile_id)
);

CREATE TABLE tokens (
    id INT AUTO_INCREMENT PRIMARY KEY,
    access_token VARCHAR(255) NOT NULL UNIQUE,
    client_token VARCHAR(255),
    user_id VARCHAR(32) NOT NULL,
    selected_profile_id VARCHAR(32),
    issued_at BIGINT NOT NULL,
    expires_in_days INT DEFAULT 15,
    state ENUM('valid', 'temporarily_invalid', 'invalid') DEFAULT 'valid',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_tokens_user_id (user_id),
    INDEX idx_tokens_client_token (client_token)
);

CREATE TABLE sessions (
    id INT AUTO_INCREMENT PRIMARY KEY,
    profile_id VARCHAR(32) NOT NULL,
    server_id VARCHAR(255) NOT NULL,
    ip VARCHAR(45),
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    expires_at DATETIME NOT NULL,
    INDEX idx_sessions_profile_id (profile_id),
    INDEX idx_sessions_server_id (server_id)
);

CREATE TABLE profile_keys (
    id INT AUTO_INCREMENT PRIMARY KEY,
    user_id VARCHAR(32) NOT NULL UNIQUE,
    public_key TEXT NOT NULL,
    private_key TEXT NOT NULL,
    public_key_signature TEXT NOT NULL,
    expires_at DATETIME NOT NULL,
    refreshed_after DATETIME NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
);
