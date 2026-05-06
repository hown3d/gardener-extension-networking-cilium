// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package selfhostedshootexposure

import (
	"context"
	"fmt"
	"net/netip"
	"strings"
	"time"

	cilium_api_v2alpha1 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2alpha1"
	slimmetav1 "github.com/cilium/cilium/pkg/k8s/slim/k8s/apis/meta/v1"
	extensionscontroller "github.com/gardener/gardener/extensions/pkg/controller"
	extensionsselfhostedshootexposurecontroller "github.com/gardener/gardener/extensions/pkg/controller/selfhostedshootexposure"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	reconcilerutils "github.com/gardener/gardener/pkg/controllerutils/reconciler"
	"github.com/gardener/gardener/pkg/utils/kubernetes/health"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	ciliumv1alpha1 "github.com/gardener/gardener-extension-networking-cilium/pkg/apis/cilium/v1alpha1"
)

const (
	portName                 = "https"
	serviceLabelKey          = "cilium.networking.gardener.cloud/selfhostedshootexposure"
	l2announcementPolicyName = "selfhostedshootexposure"
)

type actuator struct {
	client client.Client
}

func newActuator(mgr manager.Manager) extensionsselfhostedshootexposurecontroller.Actuator {
	return &actuator{client: mgr.GetClient()}
}

func (a *actuator) Reconcile(ctx context.Context, log logr.Logger, exposure *extensionsv1alpha1.SelfHostedShootExposure, cluster *extensionscontroller.Cluster) ([]corev1.LoadBalancerIngress, error) {
	var (
		config *ciliumv1alpha1.SelfHostedShootExposureConfig
		err    error
	)

	if exposure.Spec.ProviderConfig != nil {
		config, err = configFromExposureResource(exposure)
		if err != nil {
			return nil, err
		}
	}

	svc := &corev1.Service{
		ObjectMeta: serviceMetadata(exposure),
	}
	_, err = controllerutil.CreateOrUpdate(ctx, a.client, svc, func() error {
		svc.Spec = serviceSpec(exposure)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("create or update service: %w", err)
	}

	for _, family := range endpointSliceFamiliesForCluster(cluster) {
		es := &discoveryv1.EndpointSlice{
			ObjectMeta: endpointSliceMetadata(exposure, family),
		}
		_, err := controllerutil.CreateOrUpdate(ctx, a.client, es, func() error {
			newEs, err := endpointSliceForExposure(exposure, family)
			if err != nil {
				return err
			}
			es.Endpoints = newEs.Endpoints
			es.AddressType = newEs.AddressType
			es.Ports = newEs.Ports
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("create or update %s EndpointSlice: %w", family, err)
		}
	}

	if config != nil && config.AnnouncementMode == ciliumv1alpha1.AnnouncementModeL2 {
		l2pol := &cilium_api_v2alpha1.CiliumL2AnnouncementPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name: serviceName(exposure),
			},
		}
		_, err = controllerutil.CreateOrUpdate(ctx, a.client, l2pol, func() error {
			l2pol.Spec = l2announcementPolicySpec()
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("create or update l2 policy: %w", err)
		}
	}

	ippool := &cilium_api_v2alpha1.CiliumLoadBalancerIPPool{
		ObjectMeta: metav1.ObjectMeta{
			Name: serviceName(exposure),
		},
	}
	_, err = controllerutil.CreateOrUpdate(ctx, a.client, ippool, func() error {
		ippool.Spec = ippoolSpec(config)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("create or update cilium loadbalancer ippool: %w", err)
	}

	// wait for LoadBalancer to be ready
	if err := health.CheckService(svc); err != nil {
		return nil, &reconcilerutils.RequeueAfterError{
			RequeueAfter: 5 * time.Second,
			Cause:        fmt.Errorf("LoadBalancer not yet ready"),
		}
	}

	return svc.Status.LoadBalancer.Ingress, nil
}

func (a *actuator) Delete(ctx context.Context, log logr.Logger, exposure *extensionsv1alpha1.SelfHostedShootExposure, cluster *extensionscontroller.Cluster) error {
	// Explicitly delete the Service and wait for it to be gone so the LoadBalancer is deprovisioned before
	// releasing the SelfHostedShootExposure.
	svc := &corev1.Service{
		ObjectMeta: serviceMetadata(exposure),
	}
	err := a.client.Delete(ctx, svc)
	if err == nil {
		return &reconcilerutils.RequeueAfterError{
			RequeueAfter: 5 * time.Second,
			Cause:        fmt.Errorf("waiting for Service to be deleted"),
		}
	}
	if !apierrors.IsNotFound(err) {
		return err
	}

	// Service is gone; delete the EndpointSlices. They have no owner references in the provider
	// cluster so they won't be garbage collected automatically.
	for _, family := range endpointSliceFamiliesForCluster(cluster) {
		if err := a.client.Delete(ctx, &discoveryv1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				Name:      endpointSliceName(exposure, family),
				Namespace: exposure.Namespace,
			},
		}); client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("could not delete %s EndpointSlice: %w", family, err)
		}
	}
	log.Info("Service and EndpointSlices gone, releasing SelfHostedShootExposure")
	return nil
}

func (a *actuator) ForceDelete(_ context.Context, _ logr.Logger, _ *extensionsv1alpha1.SelfHostedShootExposure, _ *extensionscontroller.Cluster) error {
	return nil
}

func endpointSliceFamiliesForCluster(cluster *extensionscontroller.Cluster) []discoveryv1.AddressType {
	families := make([]discoveryv1.AddressType, 0, len(cluster.Shoot.Spec.Networking.IPFamilies))
	for _, family := range cluster.Shoot.Spec.Networking.IPFamilies {
		switch family {
		case gardencorev1beta1.IPFamilyIPv4:
			families = append(families, discoveryv1.AddressTypeIPv4)
		case gardencorev1beta1.IPFamilyIPv6:
			families = append(families, discoveryv1.AddressTypeIPv6)
		}
	}
	return families
}

func serviceMetadata(exposure *extensionsv1alpha1.SelfHostedShootExposure) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:      serviceName(exposure),
		Namespace: exposure.Namespace,
		Labels:    serviceLabels(),
	}
}

func serviceLabels() map[string]string {
	return map[string]string{
		serviceLabelKey: "true",
	}
}

func serviceSpec(exposure *extensionsv1alpha1.SelfHostedShootExposure) corev1.ServiceSpec {
	return corev1.ServiceSpec{
		LoadBalancerClass: ptr.To("io.cilium/l2-announcer"),
		Type:              corev1.ServiceTypeLoadBalancer,
		IPFamilyPolicy:    ptr.To(corev1.IPFamilyPolicyPreferDualStack),
		Ports: []corev1.ServicePort{{
			Name:       portName,
			Port:       exposure.Spec.Port,
			TargetPort: intstr.FromInt32(exposure.Spec.Port),
			Protocol:   corev1.ProtocolTCP,
		}},
	}
}

func endpointSliceForExposure(exposure *extensionsv1alpha1.SelfHostedShootExposure, family discoveryv1.AddressType) (*discoveryv1.EndpointSlice, error) {
	var endpoints []discoveryv1.Endpoint
	for _, endpoint := range exposure.Spec.Endpoints {
		for _, address := range endpoint.Addresses {
			ep := discoveryv1.Endpoint{
				NodeName: &endpoint.NodeName,
				Conditions: discoveryv1.EndpointConditions{
					Ready: ptr.To(true),
				},
			}

			switch address.Type {
			case corev1.NodeInternalIP:
				ip, err := netip.ParseAddr(address.Address)
				if err != nil {
					return nil, fmt.Errorf("could not parse address %q for endpoint %q: %w", address.Address, endpoint.NodeName, err)
				}
				// Using ip.Is4() here excludes IPv4-mapped IPv6 addresses (like ::ffff:192.0.2.1)
				if ip.Is4() != (family == discoveryv1.AddressTypeIPv4) {
					continue
				}
				ep.Addresses = []string{address.Address}
			default:
				return nil, fmt.Errorf("unsupported address type %s", address.Type)
			}
			endpoints = append(endpoints, ep)
		}
	}
	if len(endpoints) == 0 {
		return nil, fmt.Errorf("no %s endpoints found for exposure %q", family, exposure.Name)
	}

	return &discoveryv1.EndpointSlice{
		AddressType: family,
		Endpoints:   endpoints,
		Ports: []discoveryv1.EndpointPort{{
			Name:     ptr.To(portName),
			Port:     ptr.To(exposure.Spec.Port),
			Protocol: ptr.To(corev1.ProtocolTCP),
		}},
	}, nil
}

func endpointSliceMetadata(exposure *extensionsv1alpha1.SelfHostedShootExposure, family discoveryv1.AddressType) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:      endpointSliceName(exposure, family),
		Namespace: exposure.Namespace,
		Labels:    map[string]string{discoveryv1.LabelServiceName: serviceName(exposure)},
	}
}

func serviceName(exposure *extensionsv1alpha1.SelfHostedShootExposure) string {
	return "exposure-" + exposure.Name
}

func endpointSliceName(exposure *extensionsv1alpha1.SelfHostedShootExposure, family discoveryv1.AddressType) string {
	return serviceName(exposure) + "-" + strings.ToLower(string(family))
}

func l2announcementPolicySpec() cilium_api_v2alpha1.CiliumL2AnnouncementPolicySpec {
	return cilium_api_v2alpha1.CiliumL2AnnouncementPolicySpec{
		ServiceSelector: serviceSelector(),
		LoadBalancerIPs: true,
		// TODO: rest of spec could in theory be configurable by proverconfig, but who gives a damn
	}
}

func ippoolSpec(config *ciliumv1alpha1.SelfHostedShootExposureConfig) cilium_api_v2alpha1.CiliumLoadBalancerIPPoolSpec {
	spec := cilium_api_v2alpha1.CiliumLoadBalancerIPPoolSpec{
		ServiceSelector: serviceSelector(),
	}
	if config != nil {
		for _, cidr := range config.IPPool.CIDRs {
			spec.Blocks = append(spec.Blocks, cilium_api_v2alpha1.CiliumLoadBalancerIPPoolIPBlock{
				Cidr: cilium_api_v2alpha1.IPv4orIPv6CIDR(cidr),
			})
		}
	}
	return spec
}

func serviceSelector() *slimmetav1.LabelSelector {
	return &slimmetav1.LabelSelector{
		MatchLabels: map[string]slimmetav1.MatchLabelsValue{
			serviceLabelKey: "true",
		},
	}
}
