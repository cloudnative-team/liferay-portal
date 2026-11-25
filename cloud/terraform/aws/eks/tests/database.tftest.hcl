mock_provider "aws" {}
mock_provider "kubernetes" {}
mock_provider "time" {}
override_data {
	target=data.aws_availability_zones.available
	values={
		names=["us-east-1a", "us-east-1b", "us-east-1c"]
	}
}
override_data {
	target=data.aws_caller_identity.current
	values={
		account_id="123456789012"
	}
}
override_module {
	outputs={
		cluster_addons={}
		cluster_certificate_authority_data="dGVzdA=="
		cluster_endpoint="https://example.eks.amazonaws.com"
		cluster_name="liferay-test-eks"
		cluster_primary_security_group_id="sg-0123456789abcdef0"
		cluster_version="1.31"
		oidc_provider="oidc.eks.us-east-1.amazonaws.com/id/EXAMPLE"
		oidc_provider_arn="arn:aws:iam::123456789012:oidc-provider/oidc.eks.us-east-1.amazonaws.com/id/EXAMPLE"
	}
	target=module.eks
}
override_module {
	outputs={
		private_subnets=["subnet-aaa", "subnet-bbb"]
		vpc_id="vpc-0123456789abcdef0"
	}
	target=module.vpc
}
run "setup_database_engine" {
	command=apply
	plan_options {
		target=[
			terraform_data.database_engine,
		]
	}
}
run "should_fail_database_changed" {
	command=plan
	expect_failures=[
		terraform_data.database_engine,
	]
	variables {
		database_engine="mysql"
	}
}
run "should_not_accept_empty_database_engine" {
	command=plan
	expect_failures=[var.database_engine]
	variables {
		database_engine=""
	}
}
run "should_not_accept_unsupported_database_engine" {
	command=plan
	expect_failures=[var.database_engine]
	variables {
		database_engine="hypersonic"
	}
}
run "should_pass_database_unchanged" {
	assert {
		condition=terraform_data.database_engine.output == "postgresql"
		error_message="The database engine must stay \"postgresql\" when it is unchanged"
	}
	command=plan
}
variables {
	database_engine="postgresql"
	deployment_name="liferay-test"
	region="us-east-1"
}