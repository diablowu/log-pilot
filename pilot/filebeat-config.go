package pilot

import (
	"io/ioutil"
	"os"
	"text/template"
	"bytes"
	"strings"
	log "github.com/Sirupsen/logrus"
)

const FILEBEAT_CONFIG = "/etc/filebeat/filebeat.yml"

const TPL_BASE = `
path.config: /etc/filebeat
path.logs: /var/log/filebeat
path.data: /var/lib/filebeat/data
filebeat.registry_file: /var/lib/filebeat/registry
{{ putIfEnvNotEmpty "filebeat.shutdown_timeout" "FILEBEAT_SHUTDOWN_TIMEOUT" "0" }}
{{ putIfEnvNotEmpty "logging.level" "FILEBEAT_LOG_LEVEL" "info" }}
logging.metrics.enabled: true
filebeat.config:
    prospectors:
        enabled: true
        path: ${path.config}/prospectors.d/*.yml
        reload.enabled: true
        reload.period: 10s

# output
`

const TPL_CONSOLE = `
output.console:
    pretty: true
`

const TPL_KAFKA = `
output.kafka:
    hosts: {{ envArray "KAFKA_BROKERS" }} 
    topic: '%{[topic]}'
	{{ putIfEnvNotEmpty "version" "KAFKA_VERSION"}}
	{{ putIfEnvNotEmpty "username" "KAFKA_USERNAME"}}
	{{ putIfEnvNotEmpty "password" "KAFKA_PASSWORD"}}
	{{ putIfEnvNotEmpty "worker" "KAFKA_WORKER"}}
    {{ putIfEnvNotEmpty "key" "KAFKA_PARTITION_KEY"}}
    {{ putIfEnvNotEmpty "partition" "KAFKA_PARTITION"}}
    {{ putIfEnvNotEmpty "client_id" "KAFKA_CLIENT_ID"}}
    {{ putIfEnvNotEmpty "metadata" "KAFKA_METADATA"}}
    {{ putIfEnvNotEmpty "bulk_max_size" "KAFKA_BULK_MAX_SIZE"}}
    {{ putIfEnvNotEmpty "broker_timeout" "KAFKA_BROKER_TIMEOUT"}}
    {{ putIfEnvNotEmpty "channel_buffer_size" "KAFKA_CHANNEL_BUFFER_SIZE"}}
    {{ putIfEnvNotEmpty "keep_alive" "KAFKA_KEEP_ALIVE"}}
    {{ putIfEnvNotEmpty "max_message_bytes" "KAFKA_MAX_MESSAGE_BYTES" "1000000"}}
    {{ putIfEnvNotEmpty "required_acks" "KAFKA_REQUIRE_ACKS" "1"}}
    partition.round_robin.reachable_only: false
`

const TPL_REDIS = `
`
const TPL_ES = `
`

const TPL_LS = `

`

func CreateFileBeatCfg() error {
	os.Mkdir("/etc/filebeat/prospectors.d", 0666)

	allTpl := TPL_BASE + "\n" + TPL_CONSOLE

	tpl, err := template.New("filebeat").Funcs(fm).Parse(allTpl)
	if err != nil {
		return err
	}

	var buf bytes.Buffer

	tpl.Funcs(fm)
	tpl.Execute(&buf, nil)

	return ioutil.WriteFile(FILEBEAT_CONFIG, []byte(allTpl), 0666)
}

func putIfEnvNotEmpty(args ...interface{}) string {

	if len(args) < 2 {
		log.Fatal("putIfEnvNotEmpty must 2 args")
	}
	var key, envKey, envVal, dv string
	if v, ok := args[0].(string); ok {
		key = strings.TrimSpace(v)
	}

	if v, ok := args[1].(string); ok {
		envKey = strings.TrimSpace(v)
		for _, e := range os.Environ() {
			if strings.HasPrefix(e, envKey) {
				if ps := strings.Split(e, "="); len(ps) > 1 {
					envVal = ps[1]
				} else {
					envVal = ""
				}
			}
		}
	}

	if len(args) < 3 {
		dv = ""
	} else {
		if v, ok := args[2].(string); ok {
			dv = strings.TrimSpace(v)
		}
	}

	if len(envVal) > 0 {
		return key + ": " + envVal
	} else if len(dv) > 0 {
		return key + ": " + dv
	} else {
		return ""
	}
}

func envArray(args ...interface{}) string {
	arr := make([]string, 0)
	if v, ok := args[0].(string); ok {
		for _, e := range os.Environ() {
			if strings.HasPrefix(e, v) {
				if ps := strings.Split(e, "="); len(ps) > 1 {
					pp := strings.Split(ps[1], ",")
					arr = pp
				}
			}
		}
	}
	return "[ \"" + strings.Join(arr, "\",\"") + "\" ]"
}

var fm = template.FuncMap{
	"putIfEnvNotEmpty": putIfEnvNotEmpty,
	"envArray":         envArray,
}
