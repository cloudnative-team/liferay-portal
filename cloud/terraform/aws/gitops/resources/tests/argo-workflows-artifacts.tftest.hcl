mock_provider "aws" {
	mock_data "aws_iam_policy_document" {
		defaults={
			json="{\"Statement\": [], \"Version\": \"2012-10-17\"}"
		}
	}
	mock_resource "aws_iam_policy" {
		defaults={
			arn="arn:aws:iam::123456789012:policy/mock"
		}
	}
}
mock_provider "helm" {}
mock_provider "kubernetes" {}
override_data {
	target=data.aws_caller_identity.current
	values={
		account_id="123456789012"
	}
}
override_data {
	target=data.aws_eks_cluster.cluster
	values={
		identity=[{
			oidc=[{
				issuer="https://oidc.eks.us-east-1.amazonaws.com/id/EXAMPLE"
			}]
		}]
		vpc_config=[{
			cluster_security_group_id="sg-0123456789abcdef0"
			endpoint_private_access=true
			endpoint_public_access=true
			public_access_cidrs=["0.0.0.0/0"]
			security_group_ids=[]
			subnet_ids=["subnet-aaa", "subnet-bbb"]
			vpc_id="vpc-0123456789abcdef0"
		}]
	}
}
override_data {
	target=data.aws_subnets.private
	values={
		ids=["subnet-aaa", "subnet-bbb"]
	}
}
override_data {
	target=data.aws_vpc.current
	values={
		cidr_block="10.0.0.0/16"
	}
}
run "should_annotate_the_argo_server_service_account_with_the_artifacts_role" {
	assert {
		condition=kubernetes_annotations.argo_workflows_server_service_account.metadata[0].name == "argo-workflows-server"
		error_message="The annotation must land on the service account the Argo Server runs as"
	}
	assert {
		condition=kubernetes_annotations.argo_workflows_server_service_account.metadata[0].namespace == "argo-workflows-system"
		error_message="The annotation must land in the Argo Workflows namespace"
	}
	assert {
		condition=aws_iam_role.argo_workflows_artifacts.name == "liferay-test-eks-argo-workflows-artifacts"
		error_message="The Argo Server artifacts role name must be derived from the cluster name"
	}
	command=plan
}
run "should_let_the_argo_server_write_uploads_and_the_workflow_only_read_them" {
	assert {
		condition=strcontains(aws_iam_role_policy.argo_workflows_artifacts.policy, "s3:PutObject")
		error_message="The Argo Server must be able to write the upload it receives from the browser"
	}
	assert {
		condition=!strcontains(aws_iam_role_policy.offline_activation_artifacts.policy, "s3:PutObject")
		error_message="The workflow only downloads the bundle, so it must not be able to write to the bucket"
	}
	assert {
		condition=strcontains(aws_iam_role_policy.offline_activation_artifacts.policy, "arn:aws:s3:::liferay-test-argo-artifacts/uploads/*")
		error_message="The workflow read grant must be confined to the prefix the Argo Server uploads into"
	}
	command=plan
}
run "should_trust_the_offline_activation_service_account_in_every_environment_namespace" {
	assert {
		condition=strcontains(aws_iam_role.offline_activation_artifacts.assume_role_policy, "system:serviceaccount:liferay-*:offline-activation-service-account")
		error_message="The workflow role must trust the offline activation service account in any liferay namespace, because ArgoCD names them"
	}
	assert {
		condition=strcontains(aws_iam_role.argo_workflows_artifacts.assume_role_policy, "system:serviceaccount:argo-workflows-system:argo-workflows-server")
		error_message="The Argo Server role must trust only the Argo Server service account"
	}
	command=plan
}
variables {
	deployment_name="liferay-test"
	infrastructure_helm_chart_version="0.4.9"
	infrastructure_provider_helm_chart_version="0.3.12"
	liferay_git_repo_url="https://github.com/example/liferay-gitops.git"
	liferay_helm_chart_version="0.4.20"
	region="us-east-1"
}