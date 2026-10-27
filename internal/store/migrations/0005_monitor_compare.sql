-- 监控表增加对比字段
ALTER TABLE monitors ADD COLUMN reference_monitor_id INTEGER REFERENCES monitors(id) ON DELETE SET NULL;
ALTER TABLE monitors ADD COLUMN compare_state TEXT NOT NULL DEFAULT '';        -- '' | in_sync | lagging | ahead
ALTER TABLE monitors ADD COLUMN compare_ref_version TEXT NOT NULL DEFAULT '';  -- 参照方 latest 快照
ALTER TABLE monitors ADD COLUMN compare_since DATETIME;                        -- 进入当前对比状态的时间
ALTER TABLE monitors ADD COLUMN compare_notified_ref TEXT NOT NULL DEFAULT ''; -- 上次落后通知时的参照版本
CREATE INDEX idx_monitors_reference ON monitors(reference_monitor_id);

-- 投递记录增加 kind 与 context
ALTER TABLE deliveries ADD COLUMN kind TEXT NOT NULL DEFAULT 'update';         -- update | compare
ALTER TABLE deliveries ADD COLUMN context TEXT;                                -- JSON 事件快照
