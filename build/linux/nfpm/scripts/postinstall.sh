#!/bin/sh

# Update desktop database for .desktop file changes
# This makes the application appear in application menus and registers its capabilities.
if command -v update-desktop-database >/dev/null 2>&1; then
  echo "Updating desktop database..."
  update-desktop-database -q /usr/share/applications
else
  echo "Warning: update-desktop-database command not found. Desktop file may not be immediately recognized." >&2
fi

# Update MIME database for custom URL schemes (x-scheme-handler)
# This ensures the system knows how to handle your custom protocols.
if command -v update-mime-database >/dev/null 2>&1; then
  echo "Updating MIME database..."
  update-mime-database -n /usr/share/mime
else
  echo "Warning: update-mime-database command not found. Custom URL schemes may not be immediately recognized." >&2
fi

# 特权辅助服务：装完即启用并启动。deb 的 postinst 本来就以 root 跑，
# 这是规划里「安装时一次性装好辅助服务、之后不再向用户要权限」的落点。
# 服务缺位时应用会明确报「特权辅助服务没在运行」，所以这里的失败要打出来说明。
if command -v systemctl >/dev/null 2>&1; then
  systemctl daemon-reload || true
  systemctl enable --now github-net-helper.service || \
    echo "Warning: github-net-helper 服务没能启用；应用内的 Hosts 写入会报服务不可用。" >&2
else
  echo "Warning: 未找到 systemctl（非 systemd 系统），特权辅助服务没有启动。" >&2
fi

exit 0
