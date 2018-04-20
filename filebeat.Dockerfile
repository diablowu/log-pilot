FROM tkp/base/centos-base


ADD assets/filebeat/filebeat.tar.gz /tmp/
ENV FILEBEAT_VERSION=5.6.9

RUN mkdir /var/log/filebeat /var/lib/filebeat && \
    mkdir -p /etc/filebeat /var/lib/filebeat /var/log/filebeat && \
    cp -rf /tmp/filebeat-${FILEBEAT_VERSION}-linux-x86_64/filebeat /usr/bin/ && \
    cp -rf /tmp/filebeat-${FILEBEAT_VERSION}-linux-x86_64/module /etc/filebeat/ && \
    cp -rf /tmp/filebeat-${FILEBEAT_VERSION}-linux-x86_64/filebeat.*.json /etc/filebeat/ && \
    cp -rf /tmp/filebeat-${FILEBEAT_VERSION}-linux-x86_64/scripts /etc/filebeat/ && \
    rm -rf /tmp/filebeat-${FILEBEAT_VERSION}-linux-x86_64.tar.gz /tmp/filebeat-${FILEBEAT_VERSION}-linux-x86_64

COPY ./log-pilot /pilot/pilot
COPY assets/entrypoint assets/filebeat/ /pilot/

VOLUME /var/log/filebeat
VOLUME /var/lib/filebeat

WORKDIR /pilot/
ENV PILOT_TYPE=filebeat FILEBEAT_OUTPUT=console CONSOLE_PRETTY=true
#ENTRYPOINT ["/pilot/entrypoint"]
ENTRYPOINT ["/pilot/pilot"]
