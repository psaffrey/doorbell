Here's how to ring the doorbell from a terminal:

mosquitto_pub -h 192.168.0.100 -m '{"action":"single","battery":100,"last_seen":1664390305071,"linkquality":0}' -t "sensors/doorbell"

TODO
===

Make the topic that the doorbell subscribes to configurable with an environment variable and report it on startup. I can't tell if it should be `sensors/Button` or `sensors/Doorbell`
