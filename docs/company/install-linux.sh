#!/bin/sh
# SIN PROBAR. Corre como root.
set -eu
VERSION=0.11.2
install -d -m 0755 -o root -g root /etc/rosmarops /etc/systemd/user
install -m 0755 -o root -g root "$INSTALLER_PATH/rosmarops-linux-amd64" /usr/local/bin/rosmarops
umask 022
printf '%s  %s\n' "$(sha256sum /usr/local/bin/rosmarops | cut -d' ' -f1)" "$VERSION" > /etc/rosmarops/binaries.sha256
printf '%s\n' '{"schema_version":1,"summary":{"enabled":true},"first_run":{"repo_roots":["~/source","~/projects"]}}' > /etc/rosmarops/company.json
chown root:root /etc/rosmarops/binaries.sha256 /etc/rosmarops/company.json
chmod 0644 /etc/rosmarops/binaries.sha256 /etc/rosmarops/company.json
# Mismos nombres que la tarea que crea cada persona (WeeklyCommandLinux): si existe la suya, manda la suya.
cat > /etc/systemd/user/rosmarops-weekly.service <<'EOF'
[Unit]
Description=RosmarOps weekly review
[Service]
Type=oneshot
ExecStart=/usr/local/bin/rosmarops run --unattended
EOF
cat > /etc/systemd/user/rosmarops-weekly.timer <<'EOF'
[Unit]
Description=RosmarOps weekly review
[Timer]
OnCalendar=Fri *-*-* 09:00:00
Persistent=true
[Install]
WantedBy=timers.target
EOF
systemctl --global enable rosmarops-weekly.timer
