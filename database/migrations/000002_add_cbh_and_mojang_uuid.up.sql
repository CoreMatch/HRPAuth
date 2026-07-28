ALTER TABLE `users`
  ADD COLUMN `cbh` TINYINT(1) NOT NULL DEFAULT 1
    COMMENT '1=由人在 WebUI 创建, 0=由 WinnerProxy 代注册创建',
  ADD COLUMN `mojang_uuid` VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NULL
    COMMENT 'Mojang 玩家 UUID (无连字符), 绑后唯一',
  ADD UNIQUE KEY `uk_users_mojang_uuid` (`mojang_uuid`);
