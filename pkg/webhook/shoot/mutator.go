// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package shoot

import (
	"context"
	"fmt"
	"regexp"

	extensionswebhook "github.com/gardener/gardener/extensions/pkg/webhook"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type mutator struct {
	logger logr.Logger
}

// NewMutator creates a new Mutator that mutates resources in the shoot cluster.
func NewMutator() extensionswebhook.Mutator {
	return &mutator{
		logger: log.Log.WithName("shoot-mutator"),
	}
}

var (
	regexNodeLocalDNS         = regexp.MustCompile(`^node-local-dns-.*`)
	regexAPIServerProxyConfig = regexp.MustCompile(`^apiserver-proxy-config-.*`)
)

// Mutate mutates resources.
func (m *mutator) Mutate(ctx context.Context, new, _ client.Object) error {
	acc, err := meta.Accessor(new)
	if err != nil {
		return fmt.Errorf("could not create accessor during webhook: %w", err)
	}
	// If the object does have a deletion timestamp then we don't want to mutate anything.
	if acc.GetDeletionTimestamp() != nil {
		return nil
	}

	switch x := new.(type) {
	case *corev1.ConfigMap:
		if regexNodeLocalDNS.MatchString(x.Name) {
			logMutation(logger, x.Kind, x.Namespace, x.Name)
			return m.mutateNodeLocalDNSConfigMap(ctx, x)
		}
		if regexAPIServerProxyConfig.MatchString(x.Name) {
			logMutation(logger, x.Kind, x.Namespace, x.Name)
			return mutateAPIServerProxyEnvoyConfig(x)
		}

	case *appsv1.DaemonSet:
		switch x.Name {
		case "node-local-dns":
			logMutation(logger, x.Kind, x.Namespace, x.Name)
			return m.mutateNodeLocalDNSDaemonSet(ctx, x)
		case "apiserver-proxy":
			logMutation(logger, x.Kind, x.Namespace, x.Name)
			return mutateAPIServerProxyDaemonset(x)
		}
	}
	return nil
}

// LogMutation provides a log message.
func logMutation(logger logr.Logger, kind, namespace, name string) {
	logger.Info("Mutating resource", "kind", kind, "namespace", namespace, "name", name)
}
