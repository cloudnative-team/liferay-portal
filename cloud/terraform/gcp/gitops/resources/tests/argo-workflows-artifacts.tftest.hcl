mock_provider "google" {
	mock_resource "google_service_account" {
		defaults={
			email="crossplane-gsa@liferay-test-project.iam.gserviceaccount.com"
			name="projects/liferay-test-project/serviceAccounts/crossplane-gsa@liferay-test-project.iam.gserviceaccount.com"
		}
	}
}
mock_provider "helm" {}
mock_provider "kubernetes" {}
override_data {
	target=data.google_project.project
	values={
		number="1234567890"
	}
}
run "should_grant_the_argo_server_write_access_to_the_artifacts_bucket" {
	assert {
		condition=google_storage_bucket_iam_member.argo_workflows_server_artifacts.bucket == "liferay-test-argo-artifacts"
		error_message="The Argo Server binding must target the artifacts bucket the cluster module creates"
	}
	assert {
		condition=endswith(google_storage_bucket_iam_member.argo_workflows_server_artifacts.member, "/ns/argo-workflows-system/sa/argo-workflows-server")
		error_message="The Argo Server binding must target the workload identity principal that performs the upload"
	}
	assert {
		condition=google_storage_bucket_iam_member.argo_workflows_server_artifacts.role == "roles/storage.objectUser"
		error_message="The Argo Server must be able to write the upload it receives from the browser"
	}
	command=plan
}
run "should_grant_the_offline_activation_workflow_read_access_to_the_artifacts_bucket" {
	assert {
		condition=google_storage_bucket_iam_member.offline_activation_artifacts.bucket == "liferay-test-argo-artifacts"
		error_message="The workflow binding must target the artifacts bucket the cluster module creates"
	}
	assert {
		condition=endswith(google_storage_bucket_iam_member.offline_activation_artifacts.member, "/workloadIdentityPools/liferay-test-project.svc.id.goog/*")
		error_message="The workflow binding must cover the pool, because the environment namespace is not known until ArgoCD renders it"
	}
	assert {
		condition=google_storage_bucket_iam_member.offline_activation_artifacts.role == "roles/storage.objectViewer"
		error_message="The workflow only downloads the bundle, so it must not be able to write to the bucket"
	}
	command=plan
}
variables {
	deployment_name="liferay-test"
	infrastructure_helm_chart_version="0.4.9"
	infrastructure_provider_helm_chart_version="0.3.12"
	liferay_git_repo_url="https://github.com/example/liferay-gitops.git"
	liferay_helm_chart_version="0.4.20"
	observability_helm_chart_version="0.1.0"
	project_id="liferay-test-project"
	region="us-central1"
}
