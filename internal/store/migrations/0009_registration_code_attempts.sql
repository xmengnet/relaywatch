-- 注册验证码防爆破：记录错误尝试次数，超限作废（6 位数字码在线可枚举）
ALTER TABLE registration_codes ADD COLUMN attempts INTEGER NOT NULL DEFAULT 0;
