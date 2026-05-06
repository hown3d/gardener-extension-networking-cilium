package selfhostedshootexposure

import (
	"context"

	extensionsselfhostedshootexposurecontroller "github.com/gardener/gardener/extensions/pkg/controller/selfhostedshootexposure"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/gardener/gardener-extension-networking-cilium/pkg/cilium"
)

// DefaultAddOptions are the default AddOptions for AddToManager.
var DefaultAddOptions = AddOptions{}

// AddOptions are options to apply when adding the SelfHostedShootExposure controller to the manager.
type AddOptions struct {
	// Controller are the controller.Options.
	Controller controller.Options
	// IgnoreOperationAnnotation specifies whether to ignore the operation annotation or not.
	IgnoreOperationAnnotation bool
}

// AddToManagerWithOptions adds a controller with the given Options to the given manager.
func AddToManagerWithOptions(mgr manager.Manager, opts AddOptions) error {
	return extensionsselfhostedshootexposurecontroller.Add(mgr, extensionsselfhostedshootexposurecontroller.AddArgs{
		Actuator:          newActuator(mgr),
		ControllerOptions: opts.Controller,
		Predicates:        extensionsselfhostedshootexposurecontroller.DefaultPredicates(opts.IgnoreOperationAnnotation),
		Type:              cilium.Type,
		ExtensionClasses:  []extensionsv1alpha1.ExtensionClass{extensionsv1alpha1.ExtensionClassShoot},
	})
}

// AddToManager adds a controller with the default Options.
func AddToManager(_ context.Context, mgr manager.Manager) error {
	return AddToManagerWithOptions(mgr, DefaultAddOptions)
}
