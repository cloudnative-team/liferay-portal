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
run "should_allow_only_the_configured_cidrs" {
	assert {
		condition=join(",", kubernetes_manifest.argocd_gateway_security_policy[0].manifest.spec.authorization.rules[0].principal.clientCIDRs) == "10.0.0.0/16,203.0.113.0/24"
		error_message="The Argo CD SecurityPolicy must allow exactly the configured CIDR blocks"
	}
	assert {
		condition=join(",", kubernetes_manifest.argo_workflows_gateway_security_policy[0].manifest.spec.authorization.rules[0].principal.clientCIDRs) == "10.0.0.0/16,203.0.113.0/24"
		error_message="The Argo Workflows SecurityPolicy must allow exactly the configured CIDR blocks"
	}
	assert {
		condition=kubernetes_manifest.argocd_gateway_security_policy[0].manifest.spec.authorization.rules[0].action == "Allow"
		error_message="The allowlist rule must allow the listed CIDR blocks rather than deny them"
	}
	command=plan
	variables {
		access_control_config={
			allowed_cidr_blocks=["10.0.0.0/16", "203.0.113.0/24"]
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
run "should_deny_every_client_that_is_not_allowlisted" {
	assert {
		condition=kubernetes_manifest.argocd_gateway_security_policy[0].manifest.spec.authorization.defaultAction == "Deny"
		error_message="The Argo CD SecurityPolicy must deny by default, so that only the allowlisted CIDR blocks reach the gateway"
	}
	assert {
		condition=kubernetes_manifest.argo_workflows_gateway_security_policy[0].manifest.spec.authorization.defaultAction == "Deny"
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
		condition=length(kubernetes_manifest.argocd_gateway_security_policy) == 0
		error_message="The Argo CD SecurityPolicy must not be created if access control is disabled"
	}
	assert {
		condition=length(kubernetes_manifest.argo_workflows_gateway_security_policy) == 0
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
run "should_not_create_the_security_policies_without_a_hostname" {
	assert {
		condition=length(kubernetes_manifest.argocd_gateway_security_policy) == 0
		error_message="The Argo CD SecurityPolicy must not be created if no gateway exists to attach it to"
	}
	assert {
		condition=length(kubernetes_manifest.argo_workflows_gateway_security_policy) == 0
		error_message="The Argo Workflows SecurityPolicy must not be created if no gateway exists to attach it to"
	}
	command=plan
	variables {
		access_control_config={
			allowed_cidr_blocks=["203.0.113.0/24"]
			enabled=true
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
run "should_target_the_gateways_rather_than_the_routes" {
	assert {
		condition=kubernetes_manifest.argocd_gateway_security_policy[0].manifest.spec.targetRefs[0].kind == "Gateway"
		error_message="The SecurityPolicy must target the Gateway, so that any route later attached to it inherits the allowlist"
	}
	assert {
		condition=kubernetes_manifest.argocd_gateway_security_policy[0].manifest.spec.targetRefs[0].name == "argocd-gateway"
		error_message="The Argo CD SecurityPolicy must target the Argo CD gateway"
	}
	assert {
		condition=kubernetes_manifest.argocd_gateway_security_policy[0].manifest.metadata.namespace == "argocd-system"
		error_message="The SecurityPolicy must live in the same namespace as its Gateway, since targetRefs is a namespace local reference"
	}
	assert {
		condition=kubernetes_manifest.argo_workflows_gateway_security_policy[0].manifest.spec.targetRefs[0].name == "argo-workflows-gateway"
		error_message="The Argo Workflows SecurityPolicy must target the Argo Workflows gateway"
	}
	assert {
		condition=kubernetes_manifest.argo_workflows_gateway_security_policy[0].manifest.metadata.namespace == "argo-workflows-system"
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
	observability_helm_chart_version="0.1.0"
	project_id="liferay-test-project"
	region="us-central1"
}