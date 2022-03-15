/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	"errors"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// log is for logging in this package.
var synapselog = logf.Log.WithName("synapse-resource")

func (r *Synapse) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
//+kubebuilder:webhook:path=/validate-synapse-opdev-io-v1alpha1-synapse,mutating=false,failurePolicy=fail,sideEffects=None,groups=synapse.opdev.io,resources=synapses,verbs=create;update,versions=v1alpha1,name=vsynapse.kb.io,admissionReviewVersions=v1

var _ webhook.Validator = &Synapse{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *Synapse) ValidateCreate() error {
	synapselog.Info("validate create", "name", r.Name)

	ConfigMapName := r.Spec.Homeserver.ConfigMap.Name
	ServerName := r.Spec.Homeserver.Values.ServerName

	err := errors.New("error validating Synapse config")

	if ConfigMapName == "" && ServerName != "" {
		return err
	}
	if ConfigMapName != "" && ServerName == "" {
		return err
	}
	if ConfigMapName == "" && ServerName == "" {
		return err
	}

	return nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *Synapse) ValidateUpdate(old runtime.Object) error {
	synapselog.Info("validate update", "name", r.Name)

	ConfigMapName := r.Spec.Homeserver.ConfigMap.Name
	ServerName := r.Spec.Homeserver.Values.ServerName

	err := errors.New("error validating Synapse config")

	if ConfigMapName == "" && ServerName != "" {
		return err
	}
	if ConfigMapName != "" && ServerName == "" {
		return err
	}
	if ConfigMapName == "" && ServerName == "" {
		return err
	}

	return nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *Synapse) ValidateDelete() error {
	synapselog.Info("validate delete", "name", r.Name)

	return nil
}
