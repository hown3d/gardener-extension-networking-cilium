package cilium

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type AnnouncementMode string

const AnnouncementModeL2 = "l2"

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type SelfHostedShootExposureConfig struct {
	metav1.TypeMeta
	AnnouncementMode AnnouncementMode
	IPPool           IPPoolConfig
}

type IPPoolConfig struct {
	CIDRs []string
}
