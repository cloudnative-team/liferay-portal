resource "google_project_iam_custom_role" "crossplane_provider" {
	for_each=local.crossplane_provider_iam
	permissions=each.value.manage_permissions
	project=var.project_id
	provisioner "local-exec" {
		command="gcloud iam roles delete ${self.role_id} --project ${self.project} --quiet"
		on_failure=continue
		when=destroy
	}
	role_id=replace("${var.deployment_name}_crossplane_${each.key}", "-", "_")
	title="Liferay Crossplane ${each.key} Role"
}
resource "google_project_iam_custom_role" "crossplane_provider_provision" {
	for_each={
		for provider_name, provider_iam in local.crossplane_provider_iam :
		provider_name => provider_iam
		if length(provider_iam.provision_permissions) > 0
	}
	permissions=each.value.provision_permissions
	project=var.project_id
	provisioner "local-exec" {
		command="gcloud iam roles delete ${self.role_id} --project ${self.project} --quiet"
		on_failure=continue
		when=destroy
	}
	role_id=replace("${var.deployment_name}_crossplane_${each.key}_provision", "-", "_")
	title="Liferay Crossplane ${each.key} Provisioning Role"
}
resource "google_project_iam_member" "crossplane_provider" {
	condition {
		expression=each.value.condition_expression
		title=each.value.condition_title
	}
	for_each=local.crossplane_provider_iam
	member=each.value.member
	project=var.project_id
	role=google_project_iam_custom_role.crossplane_provider[each.key].name
}
resource "google_project_iam_member" "crossplane_provider_provision" {
	for_each=google_project_iam_custom_role.crossplane_provider_provision
	member=local.crossplane_provider_iam[each.key].member
	project=var.project_id
	role=each.value.name
}
resource "google_service_account" "cloudplatform_gsa" {
	account_id="${var.deployment_name}-cp-iam"
	project=var.project_id
}
resource "google_service_account_iam_member" "cloudplatform_wi_binding" {
	member="serviceAccount:${var.project_id}.svc.id.goog[${var.crossplane_namespace}/provider-gcp-cloudplatform]"
	role="roles/iam.workloadIdentityUser"
	service_account_id=google_service_account.cloudplatform_gsa.name
}
resource "kubernetes_manifest" "function_auto_ready" {
	manifest={
		apiVersion="pkg.crossplane.io/v1beta1"
		kind="Function"
		metadata={
			name="function-auto-ready"
		}
		spec={
			package="xpkg.upbound.io/upbound/function-auto-ready:v0.6.0"
			runtimeConfigRef={
				name="function-auto-ready-runtime-config"
			}
		}
	}
	provider=kubernetes
}
resource "kubernetes_manifest" "function_auto_ready_runtime_config" {
	manifest={
		apiVersion="pkg.crossplane.io/v1beta1"
		kind="DeploymentRuntimeConfig"
		metadata={
			name="function-auto-ready-runtime-config"
		}
		spec={
			deploymentTemplate={
				metadata={
					annotations=local.deploymentruntimeconfig_opentelemetry_annotations
				}
				spec={
					selector={
						matchLabels={
							"pkg.crossplane.io/function"="function-auto-ready"
						}
					}
					template={
						metadata={
							annotations=local.deploymentruntimeconfig_opentelemetry_annotations
						}
						spec={
							containers=[
								{
									name="package-runtime"
									resources={
										limits={
											memory="256Mi"
										}
										requests={
											cpu="15m"
											memory="128Mi"
										}
									}
									securityContext=local.default_crossplane_container_security_context
								},
							],
							securityContext=local.default_crossplane_pod_security_context
						}
					}
				}
			}
		}
	}
	provider=kubernetes
}
resource "kubernetes_manifest" "function_environment_configs" {
	manifest={
		apiVersion="pkg.crossplane.io/v1beta1"
		kind="Function"
		metadata={
			name="function-environment-configs"
		}
		spec={
			package="xpkg.upbound.io/crossplane-contrib/function-environment-configs:v0.6.0"
			runtimeConfigRef={
				name="function-environment-configs-runtime-config"
			}
		}
	}
	provider=kubernetes
}
resource "kubernetes_manifest" "function_environment_configs_runtime_config" {
	manifest={
		apiVersion="pkg.crossplane.io/v1beta1"
		kind="DeploymentRuntimeConfig"
		metadata={
			name="function-environment-configs-runtime-config"
		}
		spec={
			deploymentTemplate={
				metadata={
					annotations=local.deploymentruntimeconfig_opentelemetry_annotations
				}
				spec={
					selector={
						matchLabels={
							"pkg.crossplane.io/function"="function-environment-configs"
						}
					}
					template={
						metadata={
							annotations=local.deploymentruntimeconfig_opentelemetry_annotations
						}
						spec={
							containers=[
								{
									name="package-runtime"
									resources={
										limits={
											memory="256Mi"
										}
										requests={
											cpu="15m"
											memory="128Mi"
										}
									}
									securityContext=local.default_crossplane_container_security_context
								},
							]
							securityContext=local.default_crossplane_pod_security_context
						}
					}
				}
			}
		}
	}
	provider=kubernetes
}
resource "kubernetes_manifest" "function_go_templating" {
	manifest={
		apiVersion="pkg.crossplane.io/v1beta1"
		kind="Function"
		metadata={
			name="function-go-templating"
		}
		spec={
			package="xpkg.upbound.io/crossplane-contrib/function-go-templating:v0.11.3"
			runtimeConfigRef={
				name="function-go-templating-runtime-config"
			}
		}
	}
	provider=kubernetes
}
resource "kubernetes_manifest" "function_go_templating_runtime_config" {
	manifest={
		apiVersion="pkg.crossplane.io/v1beta1"
		kind="DeploymentRuntimeConfig"
		metadata={
			name="function-go-templating-runtime-config"
		}
		spec={
			deploymentTemplate={
				metadata={
					annotations=local.deploymentruntimeconfig_opentelemetry_annotations
				}
				spec={
					selector={
						matchLabels={
							"pkg.crossplane.io/function"="function-go-templating"
						}
					}
					template={
						metadata={
							annotations=local.deploymentruntimeconfig_opentelemetry_annotations
						}
						spec={
							containers=[
								{
									name="package-runtime"
									resources={
										limits={
											memory="512Mi"
										}
										requests={
											cpu="15m"
											memory="128Mi"
										}
									}
									securityContext=local.default_crossplane_container_security_context
								},
							],
							securityContext=local.default_crossplane_pod_security_context
						}
					}
				}
			}
		}
	}
	provider=kubernetes
}
