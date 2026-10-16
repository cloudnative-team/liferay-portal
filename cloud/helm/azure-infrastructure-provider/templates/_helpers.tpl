{{- define "database.engine" -}}
{{- $engine := required "The value \"database.engine\" must be set to the database engine of the deployment" .Values.database.engine -}}
{{- if not (has $engine (list "mysql" "postgresql")) -}}
{{- fail (printf "The value %q contains an unsupported database engine: %s" "database.engine" $engine) -}}
{{- end -}}
{{- $engine -}}
{{- end -}}

{{- define "network.databasePrivateDnsZoneName" -}}
{{- print "privatelink.postgres.database.azure.com" -}}
{{- end -}}

{{- define "network.databaseSubnetName" -}}
{{- printf "%s-database-subnet" .Values.deploymentContext.deploymentName -}}
{{- end -}}

{{- define "network.virtualNetworkId" -}}
{{- printf "/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Network/virtualNetworks/%s" .Values.deploymentContext.subscriptionId .Values.deploymentContext.resourceGroupName (include "network.virtualNetworkName" .) -}}
{{- end -}}

{{- define "network.virtualNetworkName" -}}
{{- printf "%s-vnet" .Values.deploymentContext.deploymentName -}}
{{- end -}}