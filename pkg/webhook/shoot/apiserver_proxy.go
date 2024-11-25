package shoot

import (
	"errors"
	"regexp"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

const envoyConfigKey = "envoy.yaml"

func mutateAPIServerProxyDaemonset(daemonset *appsv1.DaemonSet) error {
	podSpec := &daemonset.Spec.Template.Spec
	if podSpec.HostNetwork {
		podSpec.HostNetwork = false
	}

	// remove init and sidecar containers that modify network interfaces on the host
	for i, container := range podSpec.InitContainers {
		if container.Name == "setup" {
			podSpec.InitContainers = append(podSpec.InitContainers[:i], podSpec.InitContainers[i+1:]...)
		}
	}

	newContainers := []corev1.Container{}
	for i, container := range podSpec.Containers {
		if container.Name == "proxy" {
			container.Ports = append(podSpec.Containers[i].Ports, corev1.ContainerPort{
				Name:          "https",
				ContainerPort: 443,
			})
			newContainers = append(newContainers, container)
			continue
		}
		if container.Name != "sidecar" {
			newContainers = append(newContainers, container)
		}
	}
	podSpec.Containers = newContainers

	return nil
}

var envoyAddressRegex *regexp.Regexp = regexp.MustCompile(`address: \"?(?:[0-9]{1,3}\.){3}[0-9]{1,3}\"?`)

func mutateAPIServerProxyEnvoyConfig(configmap *corev1.ConfigMap) error {
	if configmap.Data == nil {
		configmap.Data = make(map[string]string, 1)
	}

	envoyConf, ok := configmap.Data[envoyConfigKey]
	if !ok {
		return errors.New("apiserver-proxy envoy configmap does not have 'envoy.yaml' key")
	}
	configmap.Data[envoyConfigKey] = envoyAddressRegex.ReplaceAllString(envoyConf, "address: \"0.0.0.0\"")
	return nil
}
