mock_provider "aws" {}

variables {
	deployment_name="test-deployment"
	region="us-east-1"
}

run "creates_one_repository_per_name" {
	command=plan

	variables {
		ecr_repository_names=["liferay/dxp", "liferay/search"]
	}

	assert {
		condition=length(aws_ecr_repository.this)==2
		error_message="Expected one ECR repository per name in ecr_repository_names."
	}
}

run "repositories_are_immutable_and_scan_on_push" {
	command=plan

	assert {
		condition=aws_ecr_repository.this["liferay/dxp"].image_tag_mutability=="IMMUTABLE"
		error_message="ECR repositories must use immutable image tags."
	}

	assert {
		condition=aws_ecr_repository.this["liferay/dxp"].image_scanning_configuration[0].scan_on_push
		error_message="ECR repositories must scan images on push."
	}
}

run "rejects_invalid_deployment_name" {
	command=plan

	variables {
		deployment_name="X"
	}

	expect_failures=[
		var.deployment_name,
	]
}
