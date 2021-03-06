{{range .configList}}
- type: log
  enabled: true
  paths:
      - {{ .HostDir }}/{{ .File }}
  scan_frequency: 10s
  fields_under_root: true
  {{if .Stdout}}
  docker-json: true
  {{end}}
  {{if eq .Format "json"}}
  json.keys_under_root: true
  {{end}}
  fields:
      {{range $key, $value := .Tags}}
      {{ $key }}: {{ $value }}
      {{end}}
      {{range $key, $value := $.container}}
      {{ $key }}: {{ $value }}
      {{end}}
  tail_files: false
  close_inactive: 2h
  close_eof: false
  close_removed: true
  clean_removed: true
  close_renamed: false
  multiline:
    pattern: {{ if eq .MultlinePattern "" }} {{ . MultlinePattern }} {{else}}^\d{4}\-\d{2}\-\d{2}T\d{2}:\d{2}:\d{2}{{end}}
    negate: true
    match: after
  encoding: utf-8
  document_type: springboot
  exclude_files: [".gz$",".zip$"]

{{end}}
