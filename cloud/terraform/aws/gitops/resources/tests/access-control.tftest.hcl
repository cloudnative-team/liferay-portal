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
	target=data.aws_iam_role.envoy_proxy_role
	values={
		arn="arn:aws:iam::123456789012:role/liferay-test-envoy-proxy"
	}
}
override_data {
	target=data.aws_iam_role.liferay_irsa
	values={
		arn="arn:aws:iam::123456789012:role/liferay-test-irsa"
		id="liferay-test-irsa"
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
run "should_allow_the_vpc_the_legacy_cidrs_and_the_shared_cidrs" {
	assert {
		condition=join(",", kubernetes_manifest.argocd_gateway_authorization[0].manifest.spec.authorization.rules[0].principal.clientCIDRs) == "10.0.0.0/16,192.168.0.0/24,203.0.113.0/24"
		error_message="The Argo CD SecurityPolicy must allow the VPC CIDR, the legacy per service CIDR blocks and the shared CIDR blocks"
	}
	assert {
		condition=join(",", kubernetes_manifest.argo_workflows_gateway_authorization[0].manifest.spec.authorization.rules[0].principal.clientCIDRs) == "10.0.0.0/16,172.16.0.0/24,203.0.113.0/24"
		error_message="The Argo Workflows SecurityPolicy must allow the VPC CIDR, the legacy per service CIDR blocks and the shared CIDR blocks"
	}
	command=plan
	variables {
		access_control_config={
			allowed_cidr_blocks=["203.0.113.0/24"]
			enabled=true
		}
		argo_workflows_additional_allowed_cidr_blocks=["172.16.0.0/24"]
		argo_workflows_domain_config={
			hostname="argo.example.com"
		}
		argocd_additional_allowed_cidr_blocks=["192.168.0.0/24"]
		argocd_domain_config={
			hostname="argocd.example.com"
		}
	}
}
run "should_deny_every_client_that_is_not_allowlisted" {
	assert {
		condition=kubernetes_manifest.argocd_gateway_authorization[0].manifest.spec.authorization.defaultAction == "Deny"
		error_message="The Argo CD SecurityPolicy must deny by default, so that only the allowlisted CIDR blocks reach the gateway"
	}
	assert {
		condition=kubernetes_manifest.argo_workflows_gateway_authorization[0].manifest.spec.authorization.defaultAction == "Deny"
		error_message="The Argo Workflows SecurityPolicy must deny by default, so that only the allowlisted CIDR blocks reach the gateway"
	}
	command=plan
	variables {
		access_control_config={
			allowed_cidr_blocks=["203.0.113.0/24"]
			enabled=true
		}
		argo_workflows_domain_config={
			hostname="argo.example.com"
		}
		argocd_domain_config={
			hostname="argocd.example.com"
		}
	}
}
run "should_keep_the_load_balancer_source_ranges_and_the_security_policies_in_sync" {
	assert {
		condition=join(",", kubernetes_manifest.argocd_gateway_authorization[0].manifest.spec.authorization.rules[0].principal.clientCIDRs) == kubernetes_manifest.argocd_gateway_proxy_config.manifest.spec.provider.kubernetes.envoyService.annotations["service.beta.kubernetes.io/load-balancer-source-ranges"]
		error_message="The Argo CD load balancer source ranges and SecurityPolicy must allow the same CIDR blocks, otherwise a client allowed by one layer is silently dropped by the other"
	}
	assert {
		condition=join(",", kubernetes_manifest.argo_workflows_gateway_authorization[0].manifest.spec.authorization.rules[0].principal.clientCIDRs) == kubernetes_manifest.argo_workflows_gateway_proxy_config[0].manifest.spec.provider.kubernetes.envoyService.annotations["service.beta.kubernetes.io/load-balancer-source-ranges"]
		error_message="The Argo Workflows load balancer source ranges and SecurityPolicy must allow the same CIDR blocks, otherwise a client allowed by one layer is silently dropped by the other"
	}
	command=plan
	variables {
		access_control_config={
			allowed_cidr_blocks=["203.0.113.0/24"]
			enabled=true
		}
		argo_workflows_additional_allowed_cidr_blocks=["172.16.0.0/24"]
		argo_workflows_domain_config={
			hostname="argo.example.com"
		}
		argocd_additional_allowed_cidr_blocks=["192.168.0.0/24"]
		argocd_domain_config={
			hostname="argocd.example.com"
		}
	}
}
run "should_not_accept_an_invalid_cidr" {
	command=plan
	expect_failures=[var.access_control_config]
	variables {
		access_control_config={
			allowed_cidr_blocks=["not-a-cidr"]
			enabled=true
		}
	}
}
run "should_not_create_the_security_policies_when_disabled" {
	assert {
		condition=length(kubernetes_manifest.argocd_gateway_authorization) == 0
		error_message="The Argo CD SecurityPolicy must not be created if access control is disabled"
	}
	assert {
		condition=length(kubernetes_manifest.argo_workflows_gateway_authorization) == 0
		error_message="The Argo Workflows SecurityPolicy must not be created if access control is disabled"
	}
	command=plan
	variables {
		argo_workflows_domain_config={
			hostname="argo.example.com"
		}
		argocd_domain_config={
			hostname="argocd.example.com"
		}
	}
}
run "should_not_enable_the_allowlist_without_cidrs" {
	command=plan
	expect_failures=[var.access_control_config]
	variables {
		access_control_config={
			enabled=true
		}
	}
}
run "should_preserve_the_default_closed_load_balancer_source_ranges" {
	assert {
		condition=kubernetes_manifest.argocd_gateway_proxy_config.manifest.spec.provider.kubernetes.envoyService.annotations["service.beta.kubernetes.io/load-balancer-source-ranges"] == "10.0.0.0/16"
		error_message="The Argo CD load balancer must stay closed to the VPC CIDR when access control is disabled"
	}
	assert {
		condition=kubernetes_manifest.argo_workflows_gateway_proxy_config[0].manifest.spec.provider.kubernetes.envoyService.annotations["service.beta.kubernetes.io/load-balancer-source-ranges"] == "10.0.0.0/16"
		error_message="The Argo Workflows load balancer must stay closed to the VPC CIDR when access control is disabled"
	}
	command=plan
	variables {
		argo_workflows_domain_config={
			hostname="argo.example.com"
		}
	}
}
run "should_target_the_gateways_rather_than_the_routes" {
	assert {
		condition=kubernetes_manifest.argocd_gateway_authorization[0].manifest.spec.targetRefs[0].kind == "Gateway"
		error_message="The SecurityPolicy must target the Gateway, so that any route later attached to it inherits the allowlist"
	}
	assert {
		condition=kubernetes_manifest.argocd_gateway_authorization[0].manifest.metadata.namespace == "argocd-system"
		error_message="The SecurityPolicy must live in the same namespace as its Gateway, since targetRefs is a namespace local reference"
	}
	assert {
		condition=kubernetes_manifest.argo_workflows_gateway_authorization[0].manifest.spec.targetRefs[0].name == "argo-workflows-gateway"
		error_message="The Argo Workflows SecurityPolicy must target the Argo Workflows gateway"
	}
	assert {
		condition=kubernetes_manifest.argo_workflows_gateway_authorization[0].manifest.metadata.namespace == "argo-workflows-system"
		error_message="The SecurityPolicy must live in the same namespace as its Gateway, since targetRefs is a namespace local reference"
	}
	command=plan
	variables {
		access_control_config={
			allowed_cidr_blocks=["203.0.113.0/24"]
			enabled=true
		}
		argo_workflows_domain_config={
			hostname="argo.example.com"
		}
		argocd_domain_config={
			hostname="argocd.example.com"
		}
	}
}
variables {
	deployment_name="liferay-test"
	infrastructure_helm_chart_version="0.4.9"
	infrastructure_provider_helm_chart_version="0.3.12"
	liferay_git_repo_url="https://github.com/example/liferay-gitops.git"
	liferay_helm_chart_version="0.4.20"
	region="us-east-1"
}