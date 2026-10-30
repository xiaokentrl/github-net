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

# 特权辅助服务：装完即启用并（重）启动。deb 的 postinst 本来就以 root 跑，
# 这是规划里「安装时一次性装好辅助服务、之后不再向用户要权限」的落点。
# 服务缺位时应用会明确报「特权辅助服务没在运行」，所以这里的失败要打出来说明。
# 不能用 enable --now：升级安装时旧进程还占着 socket，那条命令对已在运行的服务是空操作，
# 新二进制不会被加载（2026-10-31 升级后仍是旧进程，就是这个原因）。enable 管开机自启，
# restart 每次都换上新二进制；首次安装时 restart 等同于启动。
if command -v systemctl >/dev/null 2>&1; then
  systemctl daemon-reload || true
  systemctl enable github-net-helper.service || \
    echo "Warning: github-net-helper 服务没能设成开机自启；应用内的 Hosts 写入会报服务不可用。" >&2
  systemctl restart github-net-helper.service || \
    echo "Warning: github-net-helper 服务没能启动；应用内的 Hosts 写入会报服务不可用。" >&2
else
  echo "Warning: 未找到 systemctl（非 systemd 系统），特权辅助服务没有启动。" >&2
fi

exit 0
