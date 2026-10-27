-- M8 多用户：资源归属 + 用户资料扩展
-- monitors.owner_id  0 = 未归属（迁移时回填首 admin）
-- channels.owner_id  NULL = 全局共享渠道（admin 可建，所有用户可引用）
-- 注：SQLite 的 ALTER TABLE ADD COLUMN 不允许「REFERENCES + 非 NULL 默认值」，
-- 且旧行回填后仍可能为 0，因此 owner_id 不建数据库级外键，完整性由应用层保证
ALTER TABLE monitors ADD COLUMN owner_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE channels ADD COLUMN owner_id INTEGER REFERENCES users(id);
CREATE INDEX idx_monitors_owner ON monitors(owner_id);
CREATE INDEX idx_channels_owner ON channels(owner_id);

-- 用户资料扩展
ALTER TABLE users ADD COLUMN display_name TEXT NOT NULL DEFAULT '';

-- 存量资源归首个 admin（单用户部署升级无感）
UPDATE monitors SET owner_id = (SELECT id FROM users WHERE role = 'admin' AND disabled = 0 ORDER BY id LIMIT 1)
  WHERE owner_id = 0;
UPDATE channels SET owner_id = (SELECT id FROM users WHERE role = 'admin' AND disabled = 0 ORDER BY id LIMIT 1)
  WHERE owner_id IS NULL;
