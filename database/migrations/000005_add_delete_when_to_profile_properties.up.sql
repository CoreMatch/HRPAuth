ALTER TABLE `profile_properties` ADD COLUMN `delete_when` bigint NOT NULL DEFAULT 0 COMMENT '0=active, >0=orphan Unix timestamp (seconds) when file should be deleted';
