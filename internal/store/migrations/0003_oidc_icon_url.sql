-- Provider 图标：登录页按钮左侧显示。空 = 按内置 name 映射，映射不到用默认图标。
ALTER TABLE oidc_providers ADD COLUMN icon_url TEXT NOT NULL DEFAULT '';
