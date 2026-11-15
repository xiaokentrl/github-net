#!/bin/sh

# 注：update-desktop-database / update-mime-database 对本应用是空转——桌面项本体
# 装在 /opt/github-net/share（用户级入口走首启软链，GNOME 直接扫目录不用缓存刷新），
# 也没有自定义 URL scheme（2026-11-15 目录重构复核）。

# 特权辅助服务：装完即启用并（重）启动。deb 的 postinst 本来就以 root 跑，
# 这是规划里「安装时一次性装好辅助服务、之后不再向用户要权限」的落点。
# 服务缺位时应用会明确报「特权辅助服务没在运行」，所以这里的失败要打出来说明。
# 不能用 enable --now：升级安装时旧进程还占着 socket，那条命令对已在运行的服务是空操作，
# 新二进制不会被加载（2026-10-31 升级后仍是旧进程，就是这个原因）。enable 管开机自启，
# restart 每次都换上新二进制；首次安装时 restart 等同于启动。
# socket 0660 访问组（2026-11-15 定稿）：内核在 connect 时把关组成员资格。
# 建系统组；把安装时的真实用户加进组——组成员资格需重新登录生效，装完提示一句。
if ! getent group github-net >/dev/null; then
  groupadd -r github-net || echo "Warning: github-net 组创建失败，socket 访问模型回退属主位。" >&2
fi
if [ -n "${SUDO_USER:-}" ] && [ "$SUDO_USER" != root ] && getent passwd "$SUDO_USER" >/dev/null; then
  usermod -aG github-net "$SUDO_USER" || echo "Warning: $SUDO_USER 加入 github-net 组失败。" >&2
fi
# 状态根（FHS：/opt 包的可变数据归 /var/opt）0700 root；老布局数据迁入（复制不删，降级可回退）。
mkdir -p /var/opt/github-net/backups
chmod 700 /var/opt/github-net
chown root:root /var/opt/github-net
if [ -d /var/lib/github-net ] && [ ! -f /var/opt/github-net/recovery.json ]; then
  cp -a /var/lib/github-net/recovery.json /var/opt/github-net/ 2>/dev/null || true
  cp -a /var/lib/github-net/backups/. /var/opt/github-net/backups/ 2>/dev/null || true
fi

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
