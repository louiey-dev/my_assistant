#!/bin/sh
set -eu

package=${1:?usage: install.sh RELEASE_DIRECTORY}
test -x "$package/my_assistant"

install -Dm755 "$package/my_assistant" /usr/local/bin/my_assistant
install -Dm644 "$package/my_assistant.service" /etc/systemd/system/my_assistant.service
install -Dm644 "$package/my_assistant.logrotate" /etc/logrotate.d/my_assistant
install -d -o my_assistant -g my_assistant /var/lib/my_assistant /var/log/my_assistant /etc/my_assistant
install -Dm640 -o my_assistant -g my_assistant "$package/my_assistant.env" /etc/my_assistant/my_assistant.env
systemctl daemon-reload
systemctl enable --now my_assistant.service
