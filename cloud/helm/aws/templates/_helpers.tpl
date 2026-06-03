{{- define "liferay-aws.overlayBucketName" -}}
{{- $liferayDefault := index .Values "liferay-default" | default dict -}}
{{- $overlay := $liferayDefault.overlay | default dict -}}
{{- if $overlay.bucketName -}}
{{- $overlay.bucketName -}}
{{- else -}}
{{- $global := .Values.global | default dict -}}
{{- $aws := $global.aws | default dict -}}
{{- $accountId := $aws.accountId | toString -}}
{{- $deploymentName := $global.deploymentName | toString -}}
{{- $projectId := $global.projectId | toString -}}
{{- $hash := printf "%s-%s-%s" $accountId $deploymentName $projectId | sha256sum | trunc 6 -}}
{{- printf "%s-overlay-%.18s-%s" $deploymentName $projectId $hash -}}
{{- end -}}
{{- end -}}