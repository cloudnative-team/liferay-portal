package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// LiferayExtensionSpec defines the desired state of a LiferayExtension.
//
// The CX image is assumed to be pre-built by upstream CI (customer or PaaS)
// and pushed to a registry the cluster can pull from. The operator does not
// build images; it only deploys references to existing images.
type LiferayExtensionSpec struct {
	// Image is the OCI image reference for the pre-built CX (e.g.
	// "us-central1-docker.pkg.dev/<project>/cx/<name>:<tag>").
	Image string `json:"image"`

	// VirtualInstanceID identifies the Liferay virtual instance this
	// extension is registered against. Joined with ServiceID to find the
	// matching ConfigMap metadata emitted by Liferay.
	VirtualInstanceID string `json:"virtualInstanceId"`

	// ServiceID identifies the CX service within the virtual instance.
	ServiceID string `json:"serviceId"`

	// LCP holds the Liferay Cloud Platform configuration for the workload
	// (kind, env, port, resource limits). Mirrors the lcp.json contract
	// from the existing CX packaging.
	LCP *LCPConfig `json:"lcp,omitempty"`
}

// LCPConfig captures the workload-level configuration that customers express
// today through LCP.json inside the CX zip.
type LCPConfig struct {
	// Kind is "Service" (default, deploys as a Deployment) or "Job"
	// (deploys as a Kubernetes Job). Defaults to "Service" when empty.
	Kind string `json:"kind,omitempty"`

	// Env is the set of environment variables to expose to the workload.
	Env map[string]string `json:"env,omitempty"`

	// TargetPort is the container port the workload listens on. When set
	// the operator creates a Service and HTTPRoute pointing at this port.
	TargetPort int32 `json:"targetPort,omitempty"`

	// Memory is the memory request/limit hint (Kubernetes resource string).
	Memory string `json:"memory,omitempty"`

	// CPU is the CPU request/limit hint (Kubernetes resource string).
	CPU string `json:"cpu,omitempty"`
}

// LiferayExtensionStatus reflects the observed state of a LiferayExtension.
type LiferayExtensionStatus struct {
	// Phase is a short summary of where reconciliation currently stands.
	// Expected values: "Pending", "Deploying", "Running", "Completed", "Failed".
	Phase string `json:"phase,omitempty"`

	// URL is the externally-reachable URL of the deployed CX when one
	// applies (Service kind with a TargetPort).
	URL string `json:"url,omitempty"`

	// Message carries human-readable detail about the current phase, most
	// commonly an error description when Phase is "Failed".
	Message string `json:"message,omitempty"`

	// ObservedGeneration is the .metadata.generation last reconciled.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=cx
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Image",type=string,JSONPath=".spec.image"
// +kubebuilder:printcolumn:name="URL",type=string,JSONPath=".status.url"

// LiferayExtension is the Schema for the liferayextensions API.
type LiferayExtension struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   LiferayExtensionSpec   `json:"spec,omitempty"`
	Status LiferayExtensionStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// LiferayExtensionList is a list of LiferayExtension resources.
type LiferayExtensionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []LiferayExtension `json:"items"`
}
