{{- define "common_labels" }}
app.kubernetes.io/name: {{ .Values.appName }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: "{{ .Values.tag }}"
app.kubernetes.io/component: {{ .Values.rel.component }}
app.kubernetes.io/part-of: {{ .Values.rel.part_of }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}