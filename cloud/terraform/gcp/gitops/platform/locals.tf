locals {
	argo_artifacts_bucket_name="${var.deployment_name}-argo-artifacts"
	argo_workflows_server_service_account_name="argo-workflows-server"
	common_labels={
		"app.kubernetes.io/managed-by"=local.terraform_manager_name
		"environment"="internal"
	}
	terraform_manager_name="liferay-cloud-native-terraform"
}