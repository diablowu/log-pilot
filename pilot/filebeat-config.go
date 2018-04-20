package pilot

const TPL_BASE = `
path.config: /etc/filebeat
path.logs: /var/log/filebeat
path.data: /var/lib/filebeat/data
filebeat.registry_file: /var/lib/filebeat/registry
filebeat.shutdown_timeout: ${FILEBEAT_SHUTDOWN_TIMEOUT:-0}
logging.level: ${FILEBEAT_LOG_LEVEL:-info}
logging.metrics.enabled: true
${FILEBEAT_MAX_PROCS:+max_procs: ${FILEBEAT_MAX_PROCS}}
filebeat.config:
    prospectors:
        enabled: true
        path: \${path.config}/prospectors.d/*.yml
        reload.enabled: true
        reload.period: 10s

# output
`

const TPL_CONSOLE = `
output.console:
    pretty: ${CONSOLE_PRETTY:-false}
`

const TPL_KAFKA = `
`

const TPL_REDIS = `
`
const TPL_ES = `
`

const TPL_LS = `

`

func CreateFileBeatCfg() error {
	return nil
}
