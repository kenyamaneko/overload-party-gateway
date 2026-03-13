-- Game configuration seed data
INSERT INTO game_config (key, value) VALUES
  ('free_daily_battle_limit', '10'),
  ('default_language', 'ja'),
  ('default_bgm_volume', '50'),
  ('default_se_volume', '50'),
  ('default_push_enabled', 'true')
ON CONFLICT (key) DO NOTHING;
