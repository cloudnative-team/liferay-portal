provider "azurerm" {
	features {}
}

# The API server is private (no public IP). The helm and kubernetes providers reach it
# with the cluster CA and host from kube_config plus a kubelogin exec token, mirroring the
# AWS EKS module's `aws eks get-token` exec pattern. The runner must have line-of-sight to
# the private endpoint (in-VNet agent or `az aks command invoke`) and have `az` + `kubelogin`
# installed. 6dae42f8-4368-4678-94ff-3960e28e3630 is the well-known AKS AAD server app ID.

provider "helm" {
	kubernetes={
		cluster_ca_certificate=base64decode(azurerm_kubernetes_cluster.main.kube_config[0].cluster_ca_certificate)
		exec={
			api_version="client.authentication.k8s.io/v1beta1"
			args=["get-token", "--login", "azurecli", "--server-id", "6dae42f8-4368-4678-94ff-3960e28e3630"]
			command="kubelogin"
		}
		host=azurerm_kubernetes_cluster.main.kube_config[0].host
	}
}
provider "kubernetes" {
	cluster_ca_certificate=base64decode(azurerm_kubernetes_cluster.main.kube_config[0].cluster_ca_certificate)
	exec {
		api_version="client.authentication.k8s.io/v1beta1"
		args=["get-token", "--login", "azurecli", "--server-id", "6dae42f8-4368-4678-94ff-3960e28e3630"]
		command="kubelogin"
	}
	host=azurerm_kubernetes_cluster.main.kube_config[0].host
}
