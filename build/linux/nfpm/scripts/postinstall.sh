#!/bin/sh

# 刷新桌面项数据库，使应用立即出现在开始菜单
if command -v update-desktop-database >/dev/null 2>&1; then
  update-desktop-database -q /usr/share/applications
fi

# 刷新 hicolor 图标缓存，使启动器图标立即生效
if command -v gtk-update-icon-cache >/dev/null 2>&1; then
  gtk-update-icon-cache -q -t -f /usr/share/icons/hicolor >/dev/null 2>&1 || true
fi

cat <<'MSG'

------------------------------------------------------------
  SuperSender 已安装完成
  - 命令行启动：SuperSender
  - 或在应用菜单中搜索 “SuperSender” 点击图标启动
------------------------------------------------------------
MSG

exit 0
