locals {
	az_count=min(length(data.aws_availability_zones.available.names), var.max_availability_zones)
	cluster_name="${var.deployment_name}-eks"
	default_private_subnets=[for i in range(local.az_count) : cidrsubnet(var.vpc_cidr, 8, i + 1)]
	default_public_subnets=[for i in range(local.az_count) : cidrsubnet(var.vpc_cidr, 8, i + 101)]
	eks_api_public_access_cidrs=length(var.eks_api_additional_allowed_cidr_blocks) > 0 ? var.eks_api_additional_allowed_cidr_blocks : ["0.0.0.0/0"]
	liferay_namespace_pattern="liferay-*"

	// The identity every caller of the marketplace access point is remapped to.
	// It matches the user the workloads already run as, so that what the
	// operator writes is readable where it is mounted.

	marketplace_posix_id=1000
	oidc_provider_arn="arn:${var.arn_partition}:iam::${data.aws_caller_identity.current.account_id}:oidc-provider/${module.eks.oidc_provider}"
	selected_azs=slice(data.aws_availability_zones.available.names, 0, local.az_count)
}