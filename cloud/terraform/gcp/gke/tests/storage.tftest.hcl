mock_provider "google" {}
mock_provider "google-beta" {}
mock_provider "helm" {}
mock_provider "kubernetes" {}
mock_provider "null" {}
mock_provider "time" {}
override_data {
	target=data.google_compute_zones.available
	values={
		names=["us-central1-a", "us-central1-b", "us-central1-c"]
	}
}
override_data {
	target=data.google_netblock_ip_ranges.health_checkers
	values={
		cidr_blocks_ipv4=["35.191.0.0/16"]
	}
}
override_data {
	target=data.google_netblock_ip_ranges.legacy_health_checkers
	values={
		cidr_blocks_ipv4=["130.211.0.0/22"]
	}
}
override_data {
	target=data.google_project.project
	values={
		number="1234567890"
	}
}
run "should_create_the_argo_artifacts_bucket" {
	assert {
		condition=google_storage_bucket.argo_artifacts.name == "liferay-test-argo-artifacts"
		error_message="The Argo artifacts bucket name must be derived from deployment_name"
	}
	assert {
		condition=google_storage_bucket.argo_artifacts.public_access_prevention == "enforced"
		error_message="The Argo artifacts bucket must refuse to be made public"
	}
	assert {
		condition=google_storage_bucket.argo_artifacts.uniform_bucket_level_access
		error_message="The Argo artifacts bucket must use uniform bucket level access, so its IAM bindings are the only grant"
	}
	assert {
		condition=output.argo_artifacts_bucket_name == "liferay-test-argo-artifacts"
		error_message="The Argo artifacts bucket name must be exposed for the platform module to configure the artifact repository"
	}
	command=plan
}
run "should_expire_abandoned_argo_artifact_uploads" {
	assert {
		condition=length([for rule in google_storage_bucket.argo_artifacts.lifecycle_rule : rule if one(rule.action).type == "Delete" && one(rule.condition).age == 3 && try(contains(one(rule.condition).matches_prefix, "uploads/"), false)]) == 1
		error_message="A custom argo_artifacts_retention_days must expire the prefix the Argo Server uploads into"
	}
	assert {
		condition=length([for rule in google_storage_bucket.argo_artifacts.lifecycle_rule : rule if one(rule.action).type == "AbortIncompleteMultipartUpload"]) == 1
		error_message="An interrupted upload must not linger as a billable multipart upload"
	}
	assert {
		condition=length(google_storage_bucket.argo_artifacts.lifecycle_rule) == 3
		error_message="Versioning keeps a superseded upload, so a third rule must expire it rather than let it accumulate"
	}
	assert {
		condition=one(google_storage_bucket.argo_artifacts.versioning).enabled
		error_message="The bucket must be versioned, so a replaced object is recoverable"
	}
	command=plan
	variables {
		argo_artifacts_retention_days=3
	}
}
variables {
	deployment_name="liferay-test"
	envoy_gateway_helm_chart_version="1.6.3"
	project_id="liferay-test-project"
	region="us-central1"
}
