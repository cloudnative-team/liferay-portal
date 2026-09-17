resource "google_storage_bucket" "argo_artifacts" {
	#checkov:skip=CKV_GCP_62:The bucket holds Argo workflow artifacts and activation bundles the Argo Server already logs. Access logging would need a second bucket whose own retention and access have to be managed, for an audit trail nothing reads.
	force_destroy=true
	lifecycle_rule {
		action {
			type="AbortIncompleteMultipartUpload"
		}
		condition {
			age=1
		}
	}
	lifecycle_rule {
		action {
			type="Delete"
		}
		condition {
			age=var.argo_artifacts_retention_days
			matches_prefix=["uploads/"]
		}
	}
	lifecycle_rule {
		action {
			type="Delete"
		}
		condition {
			age=var.argo_artifacts_retention_days
			with_state="ARCHIVED"
		}
	}
	location=var.region
	name="${var.deployment_name}-argo-artifacts"
	project=var.project_id
	public_access_prevention="enforced"
	uniform_bucket_level_access=true
	versioning {
		enabled=true
	}
}
