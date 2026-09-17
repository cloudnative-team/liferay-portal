resource "aws_s3_bucket" "argo_artifacts" {
	#checkov:skip=CKV_AWS_18:The bucket holds Argo workflow artifacts and activation bundles the Argo Server already logs. Access logging would need a second bucket whose own retention and access have to be managed, for an audit trail nothing reads.
	#checkov:skip=CKV_AWS_144:Cross region replication does not fit a bucket whose contents expire on the schedule the lifecycle configuration sets. A lost artifact is re-uploaded through the Argo Workflows UI.
	#checkov:skip=CKV2_AWS_62:Nothing subscribes to this bucket. The workflow reads the object by the key the Argo Server hands it at submit time rather than reacting to an event.
	bucket=local.argo_artifacts_bucket_name
	force_destroy=true
	tags={
		Name=local.argo_artifacts_bucket_name
	}
}
resource "aws_s3_bucket_lifecycle_configuration" "argo_artifacts" {
	bucket=aws_s3_bucket.argo_artifacts.id
	rule {
		abort_incomplete_multipart_upload {
			days_after_initiation=1
		}
		id="abort-incomplete-uploads"
		status="Enabled"
	}
	rule {
		expiration {
			days=var.argo_artifacts_retention_days
		}
		filter {
			prefix="uploads/"
		}
		id="expire-abandoned-uploads"
		noncurrent_version_expiration {
			noncurrent_days=var.argo_artifacts_retention_days
		}
		status="Enabled"
	}
}
resource "aws_s3_bucket_public_access_block" "argo_artifacts" {
	block_public_acls=true
	block_public_policy=true
	bucket=aws_s3_bucket.argo_artifacts.id
	ignore_public_acls=true
	restrict_public_buckets=true
}
resource "aws_s3_bucket_server_side_encryption_configuration" "argo_artifacts" {
	bucket=aws_s3_bucket.argo_artifacts.id
	rule {
		apply_server_side_encryption_by_default {
			sse_algorithm="aws:kms"
		}
		bucket_key_enabled=true
	}
}
resource "aws_s3_bucket_versioning" "argo_artifacts" {
	bucket=aws_s3_bucket.argo_artifacts.id
	versioning_configuration {
		status="Enabled"
	}
}
