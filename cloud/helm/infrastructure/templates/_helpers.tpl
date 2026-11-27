{{- define "clientExtensions.namespace" -}}
{{- .Values.clientExtensions.separateNamespace.name -}}
{{- end -}}

{{- define "elasticsearch.containerSecurityContext" -}}
allowPrivilegeEscalation: false
capabilities:
    drop:
        -   ALL
privileged: false
readOnlyRootFilesystem: false
{{- end -}}

{{- define "elasticsearch.elasticUserSecretName" -}}
{{- printf "%s-es-elastic-user" (include "elasticsearch.name" .) -}}
{{- end -}}

{{- define "elasticsearch.labels" -}}
component: search
environmentId: {{ .Values.environmentId | quote }}
projectId: {{ .Values.projectId | quote }}
{{- end -}}

{{- define "elasticsearch.name" -}}
{{- printf "%s-es" .Release.Name -}}
{{- end -}}

{{- define "elasticsearch.namespace" -}}
{{- printf "%s-elasticsearch" .Release.Namespace -}}
{{- end -}}

{{- define "elasticsearch.podSecurityContext" -}}
fsGroup: 1000
runAsGroup: 1000
runAsNonRoot: true
runAsUser: 1000
seccompProfile:
    type: RuntimeDefault
{{- end -}}

{{- define "search.secretReaderServiceAccountName" -}}
{{- printf "%s-search-secret-reader" .Release.Name -}}
{{- end -}}

{{- define "search.engine" -}}
{{- $supportedEngines := .Values.search.supportedEngines -}}
{{- $engine := .Values.search.engine | default (has "opensearch" $supportedEngines | ternary "opensearch" (first $supportedEngines)) -}}
{{- if not (has $engine $supportedEngines) -}}
{{- fail (printf "Search engine %s is not available on this cloud, which supports: %s" $engine (join ", " $supportedEngines)) -}}
{{- end -}}
{{- $engine -}}
{{- end -}}

{{- define "search.snapshotsChecksum" -}}
{{- .Values.search.snapshots | toJson | sha256sum -}}
{{- end -}}