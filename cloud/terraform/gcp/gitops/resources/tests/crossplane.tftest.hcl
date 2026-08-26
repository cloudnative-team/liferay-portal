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
run "should_bind_provider_least_privilege_roles" {
	assert {
		condition=length(google_project_iam_custom_role.crossplane_provider) == 4
		error_message="A custom role must be created for every Crossplane GCP provider"
	}
	assert {
		condition=!contains(keys(google_project_iam_custom_role.crossplane_provider), "compute")
		error_message="The compute provider manages no resources and must not be granted a role"
	}
	assert {
		condition=google_project_iam_custom_role.crossplane_provider["storage"].role_id == "liferay_test_crossplane_storage"
		error_message="The provider custom role ids must be derived from deployment_name"
	}
	assert {
		condition=contains(google_project_iam_custom_role.crossplane_provider["storage"].permissions, "storage.buckets.setIamPolicy")
		error_message="The storage role must be able to manage bucket IAM policies"
	}
	assert {
		condition=endswith(google_project_iam_member.crossplane_provider["storage"].member, "/provider-gcp-storage")
		error_message="The storage provider binding must target the provider-gcp-storage service account"
	}
	assert {
		condition=length(google_project_iam_member.crossplane_provider) == 4
		error_message="Every Crossplane provider custom role must be bound to its identity"
	}
	assert {
		condition=startswith(google_project_iam_member.crossplane_provider["sql"].member, "principal://iam.googleapis.com/projects/1234567890/")
		error_message="The sql provider binding must use the Workload Identity principal"
	}
	command=plan
}
run "should_condition_every_provider_binding" {
	assert {
		condition=alltrue([for binding in google_project_iam_member.crossplane_provider : length(binding.condition) == 1])
		error_message="Every Crossplane provider binding must carry an IAM condition"
	}
	assert {
		condition=strcontains(google_project_iam_member.crossplane_provider["cloudplatform"].condition[0].expression, "modifiedGrantsByRole")
		error_message="The cloudplatform binding must limit which roles Crossplane can grant"
	}
	assert {
		condition=strcontains(google_project_iam_member.crossplane_provider["kms"].condition[0].expression, "cloudkms.googleapis.com/KeyRing")
		error_message="The kms binding condition must be scoped by resource type"
	}
	assert {
		condition=strcontains(google_project_iam_member.crossplane_provider["sql"].condition[0].expression, "sqladmin.googleapis.com/Instance")
		error_message="The sql binding condition must be scoped by resource type"
	}
	assert {
		condition=strcontains(google_project_iam_member.crossplane_provider["storage"].condition[0].expression, "projects/_/buckets/liferay-test-overlay-")
		error_message="The storage binding condition must scope the overlay bucket by deployment_name"
	}
	command=plan
}
run "should_leave_provisioning_roles_unconditioned" {
	assert {
		condition=length(google_project_iam_custom_role.crossplane_provider_provision) == 3
		error_message="A provisioning role must be created for the kms, sql and storage providers"
	}
	assert {
		condition=google_project_iam_custom_role.crossplane_provider_provision["sql"].permissions == toset(["cloudsql.instances.create", "cloudsql.instances.list"])
		error_message="Provisioning roles must hold only create and list permissions"
	}
	assert {
		condition=alltrue([for binding in google_project_iam_member.crossplane_provider_provision : length(binding.condition) == 0])
		error_message="Provisioning roles cannot be conditioned because the resource does not exist yet"
	}
	command=plan
}
run "should_configure_crossplane_functions_and_runtime_configs" {
	assert {
		condition=kubernetes_manifest.function_auto_ready.manifest.spec.package == "xpkg.upbound.io/upbound/function-auto-ready:v0.6.0"
		error_message="The auto-ready function must pin its expected package version"
	}
	assert {
		condition=kubernetes_manifest.function_auto_ready_runtime_config.manifest.spec.deploymentTemplate.metadata.annotations["sidecar.opentelemetry.io/inject"] == "false"
		error_message="The Function runtime configs must disable OpenTelemetry sidecar injection"
	}
	assert {
		condition=kubernetes_manifest.function_auto_ready_runtime_config.manifest.spec.deploymentTemplate.spec.template.spec.securityContext.runAsNonRoot == true
		error_message="The Function runtime configs must run as nonroot"
	}
	assert {
		condition=kubernetes_manifest.function_go_templating.manifest.spec.package == "xpkg.upbound.io/crossplane-contrib/function-go-templating:v0.11.3"
		error_message="The go-templating function must pin its expected package version"
	}
	command=plan
}
run "should_name_the_cloudplatform_service_account" {
	assert {
		condition=google_service_account.cloudplatform_gsa.account_id == "liferay-test-cp-iam"
		error_message="The cloudplatform service account id must be derived from deployment_name"
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