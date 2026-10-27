-- 版本正则重写：支持将非标准版本（如日期、混合 tag）重写为标准版本
ALTER TABLE monitors ADD COLUMN from_pattern TEXT;
ALTER TABLE monitors ADD COLUMN to_pattern TEXT;
