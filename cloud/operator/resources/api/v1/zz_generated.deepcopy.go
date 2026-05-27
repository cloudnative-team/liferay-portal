// Hand-written deep-copy implementations for the lxc.liferay.com/v1 types.
// Kept under the controller-gen conventional filename so a future move to
// controller-gen replaces this file in place.

package v1

import (
	"k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto copies the receiver into out.
func (in *LiferayExtension) DeepCopyInto(out *LiferayExtension) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	out.Status = in.Status
}

// DeepCopy returns a new LiferayExtension matching the receiver.
func (in *LiferayExtension) DeepCopy() *LiferayExtension {
	if in == nil {
		return nil
	}

	out := new(LiferayExtension)

	in.DeepCopyInto(out)

	return out
}

// DeepCopyObject satisfies runtime.Object.
func (in *LiferayExtension) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}

// DeepCopyInto copies the receiver into out.
func (in *LiferayExtensionList) DeepCopyInto(out *LiferayExtensionList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)

	if in.Items != nil {
		out.Items = make([]LiferayExtension, len(in.Items))

		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}

// DeepCopy returns a new LiferayExtensionList matching the receiver.
func (in *LiferayExtensionList) DeepCopy() *LiferayExtensionList {
	if in == nil {
		return nil
	}

	out := new(LiferayExtensionList)

	in.DeepCopyInto(out)

	return out
}

// DeepCopyObject satisfies runtime.Object.
func (in *LiferayExtensionList) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}

// DeepCopyInto copies the receiver into out.
func (in *LiferayExtensionSpec) DeepCopyInto(out *LiferayExtensionSpec) {
	*out = *in

	if in.LCP != nil {
		out.LCP = new(LCPConfig)

		in.LCP.DeepCopyInto(out.LCP)
	}
}

// DeepCopy returns a new LiferayExtensionSpec matching the receiver.
func (in *LiferayExtensionSpec) DeepCopy() *LiferayExtensionSpec {
	if in == nil {
		return nil
	}

	out := new(LiferayExtensionSpec)

	in.DeepCopyInto(out)

	return out
}

// DeepCopyInto copies the receiver into out.
func (in *LCPConfig) DeepCopyInto(out *LCPConfig) {
	*out = *in

	if in.Env != nil {
		out.Env = make(map[string]string, len(in.Env))

		for k, v := range in.Env {
			out.Env[k] = v
		}
	}
}

// DeepCopy returns a new LCPConfig matching the receiver.
func (in *LCPConfig) DeepCopy() *LCPConfig {
	if in == nil {
		return nil
	}

	out := new(LCPConfig)

	in.DeepCopyInto(out)

	return out
}

// DeepCopyInto copies the receiver into out.
func (in *LiferayExtensionStatus) DeepCopyInto(out *LiferayExtensionStatus) {
	*out = *in
}

// DeepCopy returns a new LiferayExtensionStatus matching the receiver.
func (in *LiferayExtensionStatus) DeepCopy() *LiferayExtensionStatus {
	if in == nil {
		return nil
	}

	out := new(LiferayExtensionStatus)

	in.DeepCopyInto(out)

	return out
}
