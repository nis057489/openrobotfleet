#!/bin/sh
set -eu
: "${MQTT_CONTROLLER_PASSWORD:?Set MQTT_CONTROLLER_PASSWORD}"
: "${MQTT_AGENT_PASSWORD:?Set MQTT_AGENT_PASSWORD}"
if [ "$MQTT_CONTROLLER_PASSWORD" = "$MQTT_AGENT_PASSWORD" ]; then
  echo 'Controller and agent MQTT passwords must differ' >&2
  exit 1
fi
umask 077
mkdir -p /mosquitto/security
mosquitto_passwd -b -c /mosquitto/security/passwords controller "$MQTT_CONTROLLER_PASSWORD"
mosquitto_passwd -b /mosquitto/security/passwords agents "$MQTT_AGENT_PASSWORD"
chown -R mosquitto:mosquitto /mosquitto/security
exec /docker-entrypoint.sh /usr/sbin/mosquitto -c /mosquitto/config/mosquitto.conf
