-- Migration: Remove Minecraft-specific logic from Core service
-- Part of HRPAuth -> General Auth refactoring

-- 1. Remove Minecraft-specific tables
DROP TABLE IF EXISTS profile_keys;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS tokens;
DROP TABLE IF EXISTS profile_properties;
DROP TABLE IF EXISTS profiles;

-- 2. Remove Minecraft-specific columns from users table
ALTER TABLE users DROP COLUMN IF EXISTS mbe;
ALTER TABLE users DROP COLUMN IF EXISTS mojang_uuid;
-- Note: cbh (Created By Human) is kept in Core for robot user cleanup logic.

