# app

## Configuration

Settings come from four layers; a later layer overrides an earlier one:

1. defaults,
2. the config file (`key = value` lines, `#` comments),
3. environment variables,
4. command-line flags.

| Setting | File key | Environment | Flag | Default |
| --- | --- | --- | --- | --- |
| Listen address | `addr` | `APP_ADDR` | `-addr` | `:8080` |
| Request timeout | `timeout` | `APP_TIMEOUT` | `-timeout` | `30s` |
| Debug logging | `debug` | `APP_DEBUG` | `-debug` | `false` |

A value that does not parse is an error naming its layer and key.
