#!/bin/sh

set -e

FILEBEAT_CONFIG=/etc/filebeat/filebeat.yml


if [ -f "$FILEBEAT_CONFIG" ]; then
    echo "$FILEBEAT_CONFIG has been existed"
    if [ $DEBUG ]; then
        echo "debug"
        rm -f $FILEBEAT_CONFIG
    else
        exit
    fi

fi

mkdir -p /etc/filebeat/prospectors.d

assert_not_empty() {
    arg=$1
    shift
    if [ -z "$arg" ]; then
        echo "$@"
        exit 1
    fi
}

cd $(dirname $0)

base() {
cat >> $FILEBEAT_CONFIG << EOF
path.config: /etc/filebeat
path.logs: /var/log/filebeat
path.data: /var/lib/filebeat/data
filebeat.registry_file: /var/lib/filebeat/registry
filebeat.shutdown_timeout: ${FILEBEAT_SHUTDOWN_TIMEOUT:-0}
logging.level: ${FILEBEAT_LOG_LEVEL:-info}
logging.metrics.enabled: true
${FILEBEAT_MAX_PROCS:+max_procs: ${FILEBEAT_MAX_PROCS}}
setup.template.name: "${FILEBEAT_INDEX:-filebeat}"
setup.template.pattern: "${FILEBEAT_INDEX:-filebeat}-*"
filebeat.config:
    prospectors:
        enabled: true
        path: \${path.config}/prospectors.d/*.yml
        reload.enabled: true
        reload.period: 10s

# output
EOF
}

es() {
if [ -f "/run/secrets/es_credential" ];then
    ELASTICSEARCH_USER=$(cat /run/secrets/es_credential | awk -F":" '{ print $1 }')
    ELASTICSEARCH_PASSWORD=$(cat /run/secrets/es_credential | awk -F":" '{ print $2 }')
fi

assert_not_empty "$ELASTICSEARCH_HOST" "ELASTICSEARCH_HOST required"
assert_not_empty "$ELASTICSEARCH_PORT" "ELASTICSEARCH_PORT required"

cat >> $FILEBEAT_CONFIG << EOF
$(base)
output.elasticsearch:
    hosts: ["$ELASTICSEARCH_HOST:$ELASTICSEARCH_PORT"]
    index: ${FILEBEAT_INDEX:-filebeat}-%{+yyyy.MM.dd}
    ${ELASTICSEARCH_SCHEME:+protocol: ${ELASTICSEARCH_SCHEME}}
    ${ELASTICSEARCH_USER:+username: ${ELASTICSEARCH_USER}}
    ${ELASTICSEARCH_PASSWORD:+password: ${ELASTICSEARCH_PASSWORD}}
    ${ELASTICSEARCH_WORKER:+worker: ${ELASTICSEARCH_WORKER}}
    ${ELASTICSEARCH_PATH:+path: ${ELASTICSEARCH_PATH}}
    ${ELASTICSEARCH_BULK_MAX_SIZE:+bulk_max_size: ${ELASTICSEARCH_BULK_MAX_SIZE}}
EOF
}

default() {
echo "use default console output "
cat >> $FILEBEAT_CONFIG << EOF
$(base)
output.console:
    pretty: ${CONSOLE_PRETTY:-false}
EOF
}

file() {
assert_not_empty "$FILE_PATH" "FILE_PATH required"

cat >> $FILEBEAT_CONFIG << EOF
$(base)
output.file:
    path: $FILE_PATH
    ${FILE_NAME:+filename: ${FILE_NAME}}
    ${FILE_ROTATE_SIZE:+rotate_every_kb: ${FILE_ROTATE_SIZE}}
    ${FILE_NUMBER_OF_FILES:+number_of_files: ${FILE_NUMBER_OF_FILES}}
    ${FILE_PERMISSIONS:+permissions: ${FILE_PERMISSIONS}}
EOF
}

logstash() {
assert_not_empty "$LOGSTASH_HOST" "LOGSTASH_HOST required"
assert_not_empty "$LOGSTASH_PORT" "LOGSTASH_PORT required"

cat >> $FILEBEAT_CONFIG << EOF
$(base)
output.logstash:
    hosts: ["$LOGSTASH_HOST:$LOGSTASH_PORT"]
    index: ${FILEBEAT_INDEX:-filebeat}-%{+yyyy.MM.dd}
    ${LOGSTASH_WORKER:+worker: ${LOGSTASH_WORKER}}
    ${LOGSTASH_LOADBALANCE:+loadbalance: ${LOGSTASH_LOADBALANCE}}
    ${LOGSTASH_BULK_MAX_SIZE:+bulk_max_size: ${LOGSTASH_BULK_MAX_SIZE}}
    ${LOGSTASH_SLOW_START:+slow_start: ${LOGSTASH_SLOW_START}}
EOF
}

redis() {
assert_not_empty "$REDIS_HOST" "REDIS_HOST required"
assert_not_empty "$REDIS_PORT" "REDIS_PORT required"

cat >> $FILEBEAT_CONFIG << EOF
$(base)
output.redis:
    hosts: ["$REDIS_HOST:$REDIS_PORT"]
    key: "%{[fields.topic]:filebeat}"
    ${REDIS_WORKER:+worker: ${REDIS_WORKER}}
    ${REDIS_PASSWORD:+password: ${REDIS_PASSWORD}}
    ${REDIS_DATATYPE:+datatype: ${REDIS_DATATYPE}}
    ${REDIS_LOADBALANCE:+loadbalance: ${REDIS_LOADBALANCE}}
    ${REDIS_TIMEOUT:+timeout: ${REDIS_TIMEOUT}}
    ${REDIS_BULK_MAX_SIZE:+bulk_max_size: ${REDIS_BULK_MAX_SIZE}}
EOF
}

kafka() {
assert_not_empty "$KAFKA_BROKERS" "KAFKA_BROKERS required"
KAFKA_BROKERS=$(echo $KAFKA_BROKERS|awk -F, '{for(i=1;i<=NF;i++){printf "\"%s\",", $i}}')
KAFKA_BROKERS=${KAFKA_BROKERS%,}

KAFKA_MAX_MESSAGE_BYTES=1000000
KAFKA_REQUIRE_ACKS=1
cat >> $FILEBEAT_CONFIG << EOF
$(base)
output.kafka:
    hosts: [$KAFKA_BROKERS]
    topic: '%{[topic]}'
    ${KAFKA_VERSION:+version: ${KAFKA_VERSION}}
    ${KAFKA_USERNAME:+username: ${KAFKA_USERNAME}}
    ${KAFKA_PASSWORD:+password: ${KAFKA_PASSWORD}}
    ${KAFKA_WORKER:+worker: ${KAFKA_WORKER}}
    ${KAFKA_PARTITION_KEY:+key: ${KAFKA_PARTITION_KEY}}
    ${KAFKA_PARTITION:+partition: ${KAFKA_PARTITION}}
    ${KAFKA_CLIENT_ID:+client_id: ${KAFKA_CLIENT_ID}}
    ${KAFKA_METADATA:+metadata: ${KAFKA_METADATA}}
    ${KAFKA_BULK_MAX_SIZE:+bulk_max_size: ${KAFKA_BULK_MAX_SIZE}}
    ${KAFKA_BROKER_TIMEOUT:+broker_timeout: ${KAFKA_BROKER_TIMEOUT}}
    ${KAFKA_CHANNEL_BUFFER_SIZE:+channel_buffer_size: ${KAFKA_CHANNEL_BUFFER_SIZE}}
    ${KAFKA_KEEP_ALIVE:+keep_alive ${KAFKA_KEEP_ALIVE}}
    ${KAFKA_MAX_MESSAGE_BYTES:+max_message_bytes: ${KAFKA_MAX_MESSAGE_BYTES}}
    ${KAFKA_REQUIRE_ACKS:+required_acks: ${KAFKA_REQUIRE_ACKS}}
    partition.round_robin.reachable_only: false
EOF
}

count(){
cat >> $FILEBEAT_CONFIG << EOF
$(base)
output.count:
EOF
}

case "$FILEBEAT_OUTPUT" in
    elasticsearch)
        es;;
    logstash)
        logstash;;
    file)
        file;;
    redis)
        redis;;
    kafka)
        kafka;;
    count)
        count;;
    *)
        default
esac

