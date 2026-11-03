{{- define "dxp-operator.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "dxp-operator.customLabels" -}}
{{- with .Values.customLabels }}
{{ toYaml . }}
{{- end }}
{{- end }}

{{- define "dxp-operator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "dxp-operator.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{- define "dxp-operator.labels" -}}
{{ include "dxp-operator.selectorLabels" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
component: liferay-dxp-operator
helm.sh/chart: {{ include "dxp-operator.chart" . }}
{{- include "dxp-operator.customLabels" . }}
{{- end }}

{{- define "dxp-operator.namespace" -}}
{{- default .Release.Namespace .Values.namespaceOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "dxp-operator.rules" -}}
{{- $clientExtensions := .context.Values.clientExtensions.enabled }}
{{- $watchNamespaces := .context.Values.watchNamespaces }}
{{- if and $clientExtensions .namespaced }}
-   apiGroups:
        -   ""
    resources:
        -   configmaps
    verbs:
        -   get
        -   list
        -   watch
{{- end }}
{{- if .namespaced }}
-   apiGroups:
        -   ""
    resources:
        -   events
    verbs:
        -   create
        -   patch
{{- end }}
{{- if .clusterScoped }}
-   apiGroups:
        -   ""
    {{- with $watchNamespaces }}
    resourceNames:
        {{- range . }}
        -   {{ . }}
        {{- end }}
    {{- end }}
    resources:
        -   namespaces
    verbs:
        -   get
        {{- if and $clientExtensions (empty $watchNamespaces) }}
        -   list
        {{- end }}
        -   patch
        {{- if and $clientExtensions (empty $watchNamespaces) }}
        -   watch
        {{- end }}
{{- end }}
{{- if .namespaced }}
-   apiGroups:
        -   ""
    resources:
        -   secrets
    verbs:
        -   create
        -   get
        -   list
        -   update
        -   watch
{{- end }}
{{- if .clusterScoped }}
-   apiGroups:
        -   apiextensions.k8s.io
    resources:
        -   customresourcedefinitions
    verbs:
        -   get
        -   list
        -   watch
{{- end }}
{{- if .namespaced }}
-   apiGroups:
        -   apps
    resources:
        -   deployments
    verbs:
        -   get
        -   list
        {{- if $clientExtensions }}
        -   patch
        {{- end }}
        -   update
        -   watch
-   apiGroups:
        -   apps
    resources:
        -   statefulsets
    verbs:
        -   get
        -   list
        -   update
        -   watch
-   apiGroups:
        -   autoscaling
    resources:
        -   horizontalpodautoscalers
    verbs:
        -   get
        -   list
        -   patch
        -   watch
{{- if $clientExtensions }}
-   apiGroups:
        -   batch
    resources:
        -   cronjobs
    verbs:
        -   get
        -   list
        -   watch
-   apiGroups:
        -   batch
    resources:
        -   jobs
    verbs:
        -   get
        -   list
        -   watch
-   apiGroups:
        -   cx.liferay.com
    resources:
        -   clientextensions
    verbs:
        -   get
        -   list
        -   patch
        -   watch
-   apiGroups:
        -   cx.liferay.com
    resources:
        -   clientextensions/finalizers
    verbs:
        -   update
-   apiGroups:
        -   cx.liferay.com
    resources:
        -   clientextensions/status
    verbs:
        -   get
        -   patch
        -   update
{{- end }}
-   apiGroups:
        -   keda.sh
    resources:
        -   scaledobjects
    verbs:
        -   get
        -   list
        -   patch
        -   watch
-   apiGroups:
        -   licensing.liferay.com
    resources:
        -   liferayenvironments
    verbs:
        -   get
        -   list
        -   watch
-   apiGroups:
        -   licensing.liferay.com
    resources:
        -   liferayenvironments/finalizers
    verbs:
        -   update
-   apiGroups:
        -   licensing.liferay.com
    resources:
        -   liferayenvironments/status
    verbs:
        -   get
        -   update
{{- end }}
{{- end }}

{{- define "dxp-operator.selectorLabels" -}}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/name: {{ include "dxp-operator.name" . }}
{{- end }}

{{- define "dxp-operator.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "dxp-operator.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}