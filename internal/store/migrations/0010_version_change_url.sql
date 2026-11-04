-- 版本变更历史补充上游发布页面链接（用于跳转与通知）
ALTER TABLE version_changes ADD COLUMN url TEXT NOT NULL DEFAULT '';
