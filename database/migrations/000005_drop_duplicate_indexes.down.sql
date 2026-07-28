ALTER TABLE `profiles`
  ADD KEY `idx_profiles_name` (`name`);

ALTER TABLE `tokens`
  ADD KEY `idx_tokens_access_token` (`access_token`);
