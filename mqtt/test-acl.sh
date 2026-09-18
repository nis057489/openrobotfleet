#!/bin/sh
# Run inside an isolated eclipse-mosquitto:2 container; see CONTRIBUTING.md.
set -eu
/bin/sh /mosquitto/config/fleet-entrypoint.sh >/tmp/broker.log 2>&1 &
broker=$!
trap 'kill "$broker" 2>/dev/null || true' EXIT
ready=false
for attempt in 1 2 3 4 5; do
  if mosquitto_pub -h 127.0.0.1 -u controller -P "$MQTT_CONTROLLER_PASSWORD" -t lab/commands/probe -m probe 2>/dev/null; then ready=true; break; fi
  sleep 1
done
if [ "$ready" != true ]; then cat /tmp/broker.log; exit 1; fi
[ "$(stat -c '%a:%U:%G' /mosquitto/security/acl)" = '600:mosquitto:mosquitto' ]
cmp /mosquitto/config/acl /mosquitto/security/acl
if grep -q 'Warning: File .*acl' /tmp/broker.log; then
  cat /tmp/broker.log; exit 1
fi

if mosquitto_pub -h 127.0.0.1 -t lab/commands/robot-a -m anonymous 2>/dev/null; then
  echo 'FAIL: anonymous publisher connected'; exit 1
fi
if mosquitto_pub -h 127.0.0.1 -u controller -P "$MQTT_AGENT_PASSWORD" -t lab/commands/robot-a -m spoof 2>/dev/null; then
  echo 'FAIL: agent password authorized controller'; exit 1
fi

mosquitto_sub -h 127.0.0.1 -u agents -P "$MQTT_AGENT_PASSWORD" -i robot-a -t lab/commands/robot-a -C 1 -W 3 >/tmp/command &
reader=$!
sleep 1
mosquitto_pub -h 127.0.0.1 -u controller -P "$MQTT_CONTROLLER_PASSWORD" -t lab/commands/robot-a -m legitimate
wait "$reader"
[ "$(cat /tmp/command)" = legitimate ]

mosquitto_sub -h 127.0.0.1 -u controller -P "$MQTT_CONTROLLER_PASSWORD" -i telemetry-reader -t lab/status/+ -C 1 -W 3 >/tmp/status &
reader=$!
sleep 1
mosquitto_pub -h 127.0.0.1 -u agents -P "$MQTT_AGENT_PASSWORD" -i robot-a -t lab/status/robot-a -m heartbeat
wait "$reader"
[ "$(cat /tmp/status)" = heartbeat ]

mosquitto_sub -h 127.0.0.1 -u agents -P "$MQTT_AGENT_PASSWORD" -i robot-a -t lab/commands/robot-a -C 1 -W 2 >/tmp/forbidden 2>/dev/null &
reader=$!
sleep 1
mosquitto_pub -h 127.0.0.1 -u agents -P "$MQTT_AGENT_PASSWORD" -i attacker -t lab/commands/robot-a -m unauthorized
if wait "$reader"; then echo 'FAIL: agent published a command'; exit 1; fi
[ ! -s /tmp/forbidden ]

mosquitto_sub -h 127.0.0.1 -u agents -P "$MQTT_AGENT_PASSWORD" -i robot-b -t lab/commands/robot-a -C 1 -W 2 >/tmp/other-robot 2>/dev/null &
reader=$!
sleep 1
mosquitto_pub -h 127.0.0.1 -u controller -P "$MQTT_CONTROLLER_PASSWORD" -t lab/commands/robot-a -m private
if wait "$reader"; then echo 'FAIL: client subscribed to another robot topic'; exit 1; fi
[ ! -s /tmp/other-robot ]
echo 'PASS: broker authentication, controller commands, agent telemetry, and topic ACLs'
