# LCD-51113 (Option A) — Wraps the cleanup-targets CloudFormation stack so
# the chronic-orphan resource classes (VPC interface endpoints, the new
# endpoint SG with standalone ingress, search log groups, the ENI/log-group
# sweeper Lambda) are owned by CFN. terraform apply creates the stack,
# terraform destroy deletes it via delete-stack — which handles the
# dependency-tricky bits in the right order.
resource "aws_cloudformation_stack" "cleanup_targets" {
	capabilities=["CAPABILITY_IAM"]
	depends_on=[
		module.eks,
		module.vpc
	]
	name="${var.deployment_name}-cleanup-targets"
	parameters={
		ClusterSecurityGroupId=module.eks.cluster_primary_security_group_id
		DeploymentName=var.deployment_name
		LogRetentionInDays=var.cleanup_targets_log_retention_in_days
		PrivateSubnetIds=join(",", module.vpc.private_subnets)
		VpcId=module.vpc.vpc_id
	}
	tags={
		DeploymentName=var.deployment_name
	}
	template_body=file("${path.module}/../../../cloudformation/cleanup-targets.yaml")
}
