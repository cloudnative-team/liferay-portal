{{/*
Per-project overlay bucket name.
This MUST stay identical to the formula in the LiferayOverlay composition
(cloud/helm/aws-infrastructure-provider/templates/compositions.yaml, step "overlay"):
    hash   = sha256sum("<accountId>-<deploymentName>-<projectId>") | trunc 6
    base   = printf "%.18s-%s" <projectId> <hash>
    bucket = printf "%s-overlay-%s" <deploymentName> <base>
An explicit liferay-default.overlay.bucketName overrides the derived value.
*/}}

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