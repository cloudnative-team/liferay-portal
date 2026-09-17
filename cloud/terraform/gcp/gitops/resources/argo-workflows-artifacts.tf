resource "google_storage_bucket_iam_member" "argo_workflows_server_artifacts" {
	bucket=local.argo_artifacts_bucket_name
	member=local.argo_artifacts_server_principal
	role="roles/storage.objectUser"
}

# The offline activation workflow runs in an environment namespace that the
# ArgoCD ApplicationSet derives from the Liferay GitOps repository, so Terraform
# cannot name it. Workload Identity Federation offers no wildcard over namespaces
# the way an IRSA trust policy does, so the read grant covers every identity in
# the cluster's pool. It stays read only, and the bucket holds nothing but Argo
# artifacts and the activation bundles uploaded through the Argo Workflows UI.

resource "google_storage_bucket_iam_member" "offline_activation_artifacts" {
	bucket=local.argo_artifacts_bucket_name
	member=local.argo_artifacts_pool_principal_set
	role="roles/storage.objectViewer"
}
