{{- define "search.labels" -}}
component: search
{{ include "liferay.commonLabels" . }}
workload_class: standard
{{- end -}}

{{- define "search.name" -}}
{{- .Values.search.elasticsearch.name | default (printf "%s-es" .Release.Name) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "search.namespace" -}}
{{- printf "%s-elasticsearch" .Release.Namespace -}}
{{- end -}}