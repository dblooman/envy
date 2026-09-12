{{- define "envy.name" -}}envy{{- end -}}
{{- define "envy.fullname" -}}{{ .Release.Name }}-envy{{- end -}}
{{- define "envy.labels" -}}
app.kubernetes.io/name: {{ include "envy.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}
