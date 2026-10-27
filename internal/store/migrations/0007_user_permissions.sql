-- M8.1 细粒度权限：用户可配置允许的操作
-- JSON 数组，如 ["monitor.create","channel.create","import","export"]
-- NULL = 未配置，按角色默认（user 全量 / viewer 只读）；[] = 全部禁用
ALTER TABLE users ADD COLUMN permissions TEXT;
