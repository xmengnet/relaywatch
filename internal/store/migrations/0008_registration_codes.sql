-- 注册邮箱验证码：按邮箱存储，避免全局单条记录被并发注册互相覆盖
CREATE TABLE registration_codes (
    email      TEXT PRIMARY KEY,
    code       TEXT NOT NULL,
    expires_at DATETIME NOT NULL,
    sent_at    DATETIME NOT NULL
);

CREATE INDEX idx_registration_codes_expires ON registration_codes(expires_at);
