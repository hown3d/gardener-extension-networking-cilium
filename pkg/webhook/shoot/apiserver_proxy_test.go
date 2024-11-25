package shoot

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
)

// envoyProxyConfig is a subpart of the apiserver-proxy envoy config used in gardener/gardener
var envoyProxyConfig = `static_resources:
  listeners:
  - name: kube_apiserver
    address:
      socket_address:
        address: "192.168.192.1"
        port_value: 443
  - name: metrics
    address:
      socket_address:
        address: "0.0.0.0"
        port_value: {{ .adminPort }}
    additional_addresses:
    - address:
        socket_address:
          address: "::"
          port_value: {{ .adminPort }}
`

var _ = Describe("mutateAPIServerProxyEnvoyConfig", func() {
	It("should mutate address to 0.0.0.0", func() {
		cm := corev1.ConfigMap{
			Data: map[string]string{
				envoyConfigKey: envoyProxyConfig,
			},
		}
		mutateAPIServerProxyEnvoyConfig(&cm)
		configData := cm.Data[envoyConfigKey]
		Expect(strings.Count(configData, "0.0.0.0")).To(Equal(2))
		Expect(strings.Contains(configData, "192.168.192.1")).To(BeFalse())
	})
})
