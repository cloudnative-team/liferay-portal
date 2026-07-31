package persistentvolumeclaim

import (
	corev1 "k8s.io/api/core/v1"
)

func GetVolumeByClaimName(claimName string, podSpec *corev1.PodSpec) *corev1.Volume {
	for index, volume := range podSpec.Volumes {
		if volume.PersistentVolumeClaim == nil {
			continue
		}

		if volume.PersistentVolumeClaim.ClaimName == claimName {
			return &podSpec.Volumes[index]
		}
	}

	return nil
}

func IsVolumeMountedReadOnly(podSpec *corev1.PodSpec, volume *corev1.Volume) bool {
	mounted := false

	for _, container := range getContainers(podSpec) {
		for _, volumeMount := range container.VolumeMounts {
			if volumeMount.Name != volume.Name {
				continue
			}

			if !volumeMount.ReadOnly && !volume.PersistentVolumeClaim.ReadOnly {
				return false
			}

			mounted = true
		}
	}

	return mounted
}

func getContainers(podSpec *corev1.PodSpec) []corev1.Container {
	containers := []corev1.Container{}

	containers = append(containers, podSpec.Containers...)
	containers = append(containers, podSpec.InitContainers...)

	return containers
}
