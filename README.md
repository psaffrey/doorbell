Here's how to ring the doorbell from a terminal:

mosquitto_pub -h 192.168.0.101 -m '{"action":"single","battery":100,"last_seen":1664390305071,"linkquality":0}' -t "sensors/doorbell"

Topic configuration
===

The doorbell subscribes to `DOORBELL_MQTT_TOPIC`, defaulting to `sensors/Doorbell`, and now reports the configured topic on startup.
