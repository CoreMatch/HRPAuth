ALTER TABLE `users`
  ADD COLUMN `mbe` TINYINT(1) NOT NULL DEFAULT 0
    COMMENT '1=允许同名 Mojang 玩家绑定, 0=HA 优先拒绝';
