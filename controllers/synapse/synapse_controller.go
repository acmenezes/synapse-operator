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

package synapse

import (
	"context"
	"errors"
	"reflect"
	"time"

	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	pgov1beta1 "github.com/crunchydata/postgres-operator/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
	synapsev1alpha1 "github.com/opdev/synapse-operator/apis/synapse/v1alpha1"
)

// SynapseReconciler reconciles a Synapse object
type SynapseReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

type HomeserverPgsqlDatabase struct {
	Name     string `yaml:"name"`
	TxnLimit int64  `yaml:"txn_limit"`
	Args     struct {
		User     string `yaml:"user"`
		Password string `yaml:"password"`
		Database string `yaml:"database"`
		Host     string `yaml:"host"`
		Port     int64  `yaml:"port"`
		CpMin    int64  `yaml:"cp_min"`
		CpMax    int64  `yaml:"cp_max"`
	}
}

//+kubebuilder:rbac:groups=synapse.opdev.io,resources=synapses,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=synapse.opdev.io,resources=synapses/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=synapse.opdev.io,resources=synapses/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.9.2/pkg/reconcile
func (r *SynapseReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx)

	// Load the Synapse by name
	var synapse synapsev1alpha1.Synapse
	if err := r.Get(ctx, req.NamespacedName, &synapse); err != nil {
		if k8serrors.IsNotFound(err) {
			// we'll ignore not-found errors, since they can't be fixed by an immediate
			// requeue (we'll need to wait for a new notification), and we can get them
			// on deleted requests.
			log.Error(
				err,
				"Cannot find Synapse - has it been deleted ?",
				"Synapse Name", synapse.Name,
				"Synapse Namespace", synapse.Namespace,
			)
			return ctrl.Result{}, nil
		}
		log.Error(
			err,
			"Error fetching Synapse",
			"Synapse Name", synapse.Name,
			"Synapse Namespace", synapse.Namespace,
		)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	var outputConfigMap corev1.ConfigMap

	if synapse.Spec.Homeserver.ConfigMap != nil {
		var inputConfigMap corev1.ConfigMap
		// Get and validate homeserver ConfigMap
		ConfigMapName := synapse.Spec.Homeserver.ConfigMap.Name
		if err := r.Get(ctx, types.NamespacedName{Name: ConfigMapName, Namespace: synapse.Namespace}, &inputConfigMap); err != nil {
			reason := "ConfigMap " + ConfigMapName + " does not exist in namespace " + synapse.Namespace
			if err := r.setFailedState(ctx, &synapse, reason); err != nil {
				log.Error(err, "Error updating Synapse State")
			}

			log.Error(err, "Failed to get ConfigMap", "ConfigMap.Namespace", synapse.Namespace, "ConfigMap.Name", ConfigMapName)
			return ctrl.Result{RequeueAfter: time.Duration(30)}, err
		}

		if err := r.ParseHomeserverConfigMap(ctx, &synapse, inputConfigMap); err != nil {
			return ctrl.Result{RequeueAfter: time.Duration(30)}, err
		}

		synapse.Status.HomeserverConfigMapName = ConfigMapName
		outputConfigMap = inputConfigMap
	} else {
		synapse.Status.HomeserverConfigMapName = synapse.Name
		synapse.Status.HomeserverConfiguration.ServerName = synapse.Spec.Homeserver.Values.ServerName
		synapse.Status.HomeserverConfiguration.ReportStats = synapse.Spec.Homeserver.Values.ReportStats

		objectMeta := setObjectMeta(synapse.Name, synapse.Namespace, map[string]string{})
		r.reconcileResource(ctx, r.configMapForSynapse, &synapse, &outputConfigMap, objectMeta)
	}

	if err := r.updateSynapseStatus(ctx, &synapse); err != nil {
		log.Error(err, "Error updating Synapse Status")
		return ctrl.Result{}, err
	}

	if synapse.Spec.CreateNewPostgreSQL {
		if !r.isPostgresOperatorInstalled(ctx) {
			reason := "Cannot create PostgreSQL instance for synapse. Postgres-operator is not installed."
			if err := r.setFailedState(ctx, &synapse, reason); err != nil {
				log.Error(err, "Error updating Synapse State")
			}

			err := errors.New("cannot create PostgreSQL instance for synapse. Potsres-operator is not installed")
			log.Error(err, "Cannot create PostgreSQL instance for synapse. Potsres-operator is not installed.")
			return ctrl.Result{}, nil
		}
		if result, err := r.createPostgresClusterForSynapse(ctx, synapse, outputConfigMap); err != nil {
			return result, err
		}
	}

	// We first need to create the Synapse Service as its IP address is potentially
	// needed by the Bridges
	objectMeta := setObjectMeta(synapse.Name, synapse.Namespace, map[string]string{})
	synapseKey := types.NamespacedName{Name: synapse.Name, Namespace: synapse.Namespace}
	createdService := &corev1.Service{}
	if err := r.reconcileResource(ctx, r.serviceForSynapse, &synapse, createdService, objectMeta); err != nil {
		return ctrl.Result{}, err
	}

	// Fetch Synapse IP and update the resource status
	synapseIP, err := r.getServiceIP(ctx, synapseKey, createdService)
	if err != nil {
		return ctrl.Result{}, err
	}
	synapse.Status.IP = synapseIP
	if err := r.updateSynapseStatus(ctx, &synapse); err != nil {
		return ctrl.Result{}, err
	}

	if synapse.Spec.Bridges.Heisenbridge.Enabled {
		log.Info("Heisenbridge is enabled - deploying Heisenbridge")
		// Heisenbridge is composed of a ConfigMap, a Service and a Deployment.
		// Resources associated to the Heisenbridge are append with "-heisenbridge"
		createdHeisenbridgeService := &corev1.Service{}
		objectMetaHeisenbridge := setObjectMeta(synapse.Name+"-heisenbridge", synapse.Namespace, map[string]string{})
		heisenbergKey := types.NamespacedName{Name: synapse.Name + "-heisenbridge", Namespace: synapse.Namespace}

		// First create the service as we need its IP address for the
		// heisenbridge.yaml configuration file
		if err := r.reconcileResource(
			ctx,
			r.serviceForHeisenbridge,
			&synapse,
			createdHeisenbridgeService,
			objectMetaHeisenbridge,
		); err != nil {
			return ctrl.Result{}, err
		}

		// Get Service IP and update the Synapse status with Service IP and
		// ConfigMap name
		heisenbridgeIP, err := r.getServiceIP(ctx, heisenbergKey, createdHeisenbridgeService)
		if err != nil {
			return ctrl.Result{}, err
		}
		synapse.Status.BridgesConfiguration.Heisenbridge.IP = heisenbridgeIP

		inputConfigMapName := synapse.Spec.Bridges.Heisenbridge.ConfigMap.Name
		if inputConfigMapName != "" {
			synapse.Status.BridgesConfiguration.Heisenbridge.ConfigMapName = inputConfigMapName
		} else {
			synapse.Status.BridgesConfiguration.Heisenbridge.ConfigMapName = objectMetaHeisenbridge.Name
		}

		if err := r.updateSynapseStatus(ctx, &synapse); err != nil {
			return ctrl.Result{}, err
		}

		if inputConfigMapName != "" {
			var inputHeisenbridgeConfigMap = &corev1.ConfigMap{}
			keyForConfigMap := types.NamespacedName{Name: inputConfigMapName, Namespace: synapse.Namespace}

			if err := r.Get(ctx, keyForConfigMap, inputHeisenbridgeConfigMap); err != nil {
				reason := "ConfigMap " + inputConfigMapName + " does not exist in namespace " + synapse.Namespace
				if err := r.setFailedState(ctx, &synapse, reason); err != nil {
					log.Error(err, "Error updating Synapse State")
				}

				log.Error(err, "Failed to get ConfigMap", "ConfigMap.Namespace", synapse.Namespace, "ConfigMap.Name", inputConfigMapName)
				return ctrl.Result{RequeueAfter: time.Duration(30)}, err
			}

			// Configure correct URL in Heisenbridge ConfigMap
			if err := r.updateConfigMap(
				ctx,
				inputHeisenbridgeConfigMap,
				synapse,
				r.updateHeisenbridgeWithURL,
				"heisenbridge.yaml",
			); err != nil {
				return ctrl.Result{}, err
			}
		} else {
			// Create ConfigMap for Heisenbridge
			if err := r.reconcileResource(
				ctx,
				r.configMapForHeisenbridge,
				&synapse,
				&corev1.ConfigMap{},
				objectMetaHeisenbridge,
			); err != nil {
				return ctrl.Result{}, err
			}
		}

		// Create Deployment for Heisenbridge
		if err := r.reconcileResource(
			ctx,
			r.deploymentForHeisenbridge,
			&synapse,
			&appsv1.Deployment{},
			objectMetaHeisenbridge,
		); err != nil {
			return ctrl.Result{}, err
		}

		// Update the Synapse ConfigMap to enable Heisenbridge
		if err := r.updateConfigMap(
			ctx,
			&outputConfigMap,
			synapse,
			r.updateHomeserverWithHeisenbridgeInfos,
			"homeserver.yaml",
		); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Reconcile Synapse resources: PVC, Deployment and Service
	if err := r.reconcileResource(ctx, r.serviceAccountForSynapse, &synapse, &corev1.ServiceAccount{}, objectMeta); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileResource(ctx, r.roleBindingForSynapse, &synapse, &rbacv1.RoleBinding{}, objectMeta); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileResource(ctx, r.persistentVolumeClaimForSynapse, &synapse, &corev1.PersistentVolumeClaim{}, objectMeta); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileResource(ctx, r.deploymentForSynapse, &synapse, &appsv1.Deployment{}, objectMeta); err != nil {
		return ctrl.Result{}, err
	}
	// TODO: If a deployment is found, check that its Spec are correct.

	// Update the Synapse status if needed
	if synapse.Status.State != "RUNNING" {
		synapse.Status.State = "RUNNING"
		synapse.Status.Reason = ""
		if err := r.Status().Update(ctx, &synapse); err != nil {
			log.Error(err, "Failed to update Synapse status")
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

// labelsForSynapse returns the labels for selecting the resources
// belonging to the given synapse CR name.
func labelsForSynapse(name string) map[string]string {
	return map[string]string{"app": "synapse", "synapse_cr": name}
}

func (r *SynapseReconciler) setFailedState(ctx context.Context, synapse *synapsev1alpha1.Synapse, reason string) error {

	synapse.Status.State = "FAILED"
	synapse.Status.Reason = reason

	return r.updateSynapseStatus(ctx, synapse)
}

func (r *SynapseReconciler) updateSynapseStatus(ctx context.Context, synapse *synapsev1alpha1.Synapse) error {
	current := &synapsev1alpha1.Synapse{}
	if err := r.Get(
		ctx,
		types.NamespacedName{Name: synapse.Name, Namespace: synapse.Namespace},
		current,
	); err != nil {
		return err
	}

	if !reflect.DeepEqual(synapse.Status, current.Status) {
		if err := r.Status().Patch(ctx, synapse, client.MergeFrom(current)); err != nil {
			return err
		}
	}

	return nil
}

func (r *SynapseReconciler) isPostgresOperatorInstalled(ctx context.Context) bool {
	err := r.Client.List(ctx, &pgov1beta1.PostgresClusterList{})
	return err == nil
}

func (r *SynapseReconciler) createPostgresClusterForSynapse(
	ctx context.Context,
	synapse synapsev1alpha1.Synapse,
	cm corev1.ConfigMap,
) (ctrl.Result, error) {
	var objectMeta metav1.ObjectMeta
	createdPostgresCluster := pgov1beta1.PostgresCluster{}

	// Create ConfigMap for PostgresCluster
	objectMeta = setObjectMeta(synapse.Name, synapse.Namespace, map[string]string{})
	if err := r.reconcileResource(ctx, r.configMapForPostgresCluster, &synapse, &corev1.ConfigMap{}, objectMeta); err != nil {
		return ctrl.Result{}, err
	}

	// Create PostgresCluster for Synapse
	objectMeta = setObjectMeta(synapse.Name, synapse.Namespace, map[string]string{})
	if err := r.reconcileResource(ctx, r.postgresClusterForSynapse, &synapse, &createdPostgresCluster, objectMeta); err != nil {
		return ctrl.Result{}, err
	}

	// Wait for PostgresCluster to be up
	if err := r.Get(ctx, types.NamespacedName{Name: createdPostgresCluster.Name, Namespace: createdPostgresCluster.Namespace}, &createdPostgresCluster); err != nil {
		return ctrl.Result{}, err
	}
	if !r.isPostgresClusterReady(createdPostgresCluster) {
		r.updateSynapseStatusDatabaseState(ctx, &synapse, "NOT READY")
		err := errors.New("postgreSQL Database not ready yet")
		return ctrl.Result{RequeueAfter: time.Duration(5)}, err
	}

	// Update Synapse Status with PostgreSQL DB information
	if err := r.updateSynapseStatusWithPostgreSQLInfos(ctx, &synapse, createdPostgresCluster); err != nil {
		return ctrl.Result{}, err
	}

	// Update configMap data with PostgreSQL DB information
	if err := r.updateConfigMap(
		ctx,
		&cm,
		synapse,
		r.updateHomeserverWithPostgreSQLInfos,
		"homeserver.yaml",
	); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *SynapseReconciler) isPostgresClusterReady(p pgov1beta1.PostgresCluster) bool {
	var status_found bool

	// Going through instance Specs
	for _, instance_spec := range p.Spec.InstanceSets {
		status_found = false
		for _, instance_status := range p.Status.InstanceSets {
			if instance_status.Name == instance_spec.Name {
				desired_replicas := *instance_spec.Replicas
				if instance_status.Replicas != desired_replicas ||
					instance_status.ReadyReplicas != desired_replicas ||
					instance_status.UpdatedReplicas != desired_replicas {
					return false
				}
				// Found instance in Status, breaking out of for loop
				status_found = true
				break
			}
		}

		// Instance found in spec, but not in status
		if !status_found {
			return false
		}
	}

	// All instances have the correct number of replicas
	return true
}

func (r *SynapseReconciler) updateSynapseStatusDatabaseState(ctx context.Context, synapse *synapsev1alpha1.Synapse, state string) error {
	synapse.Status.DatabaseConnectionInfo.State = state
	return r.updateSynapseStatus(ctx, synapse)
}

func (r *SynapseReconciler) updateSynapseStatusWithPostgreSQLInfos(
	ctx context.Context,
	s *synapsev1alpha1.Synapse,
	createdPostgresCluster pgov1beta1.PostgresCluster,
) error {
	var postgresSecret corev1.Secret

	// Get PostgreSQL secret related, containing information for the synapse user
	if err := r.Get(
		ctx,
		types.NamespacedName{
			Name:      s.Name + "-pguser-synapse",
			Namespace: s.Namespace,
		},
		&postgresSecret,
	); err != nil {
		return err
	}

	if err := r.updateSynapseStatusDatabase(s, postgresSecret); err != nil {
		return err
	}

	return r.updateSynapseStatus(ctx, s)
}

func (r *SynapseReconciler) updateSynapseStatusDatabase(
	s *synapsev1alpha1.Synapse,
	postgresSecret corev1.Secret,
) error {
	var postgresSecretData map[string][]byte = postgresSecret.Data

	host, ok := postgresSecretData["host"]
	if !ok {
		err := errors.New("missing host in PostgreSQL Secret")
		// log.Error(err, "Missing host in PostgreSQL Secret")
		return err
	}

	port, ok := postgresSecretData["port"]
	if !ok {
		err := errors.New("missing port in PostgreSQL Secret")
		// log.Error(err, "Missing port in PostgreSQL Secret")
		return err
	}

	// See https://github.com/opdev/synapse-operator/issues/12
	// databaseName, ok := postgresSecretData["dbname"]
	_, ok = postgresSecretData["dbname"]
	if !ok {
		err := errors.New("missing dbname in PostgreSQL Secret")
		// log.Error(err, "Missing dbname in PostgreSQL Secret")
		return err
	}

	user, ok := postgresSecretData["user"]
	if !ok {
		err := errors.New("missing user in PostgreSQL Secret")
		// log.Error(err, "Missing user in PostgreSQL Secret")
		return err
	}

	password, ok := postgresSecretData["password"]
	if !ok {
		err := errors.New("missing password in PostgreSQL Secret")
		// log.Error(err, "Missing password in PostgreSQL Secret")
		return err
	}

	s.Status.DatabaseConnectionInfo.ConnectionURL = string(host) + ":" + string(port)
	// s.Status.DatabaseConnectionInfo.DatabaseName = string(databaseName) // See https://github.com/opdev/synapse-operator/issues/12
	s.Status.DatabaseConnectionInfo.DatabaseName = "synapse"
	s.Status.DatabaseConnectionInfo.User = string(user)
	s.Status.DatabaseConnectionInfo.Password = string(base64encode(string(password)))
	s.Status.DatabaseConnectionInfo.State = "READY"

	return nil
}

func (r *SynapseReconciler) getServiceIP(
	ctx context.Context,
	synapseKey types.NamespacedName,
	service *corev1.Service,
) (string, error) {
	if err := r.Get(ctx, synapseKey, service); err != nil {
		return "", err
	}

	serviceIP := service.Spec.ClusterIP
	if serviceIP == "" {
		err := errors.New("service IP not set")
		return "", err
	}

	return serviceIP, nil
}

func (r *SynapseReconciler) convertStructToMap(in interface{}) (map[string]interface{}, error) {
	var intermediate []byte
	var out map[string]interface{}
	intermediate, err := yaml.Marshal(in)
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(intermediate, &out); err != nil {
		return nil, err
	}

	return out, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *SynapseReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&synapsev1alpha1.Synapse{}).
		Owns(&corev1.Service{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Complete(r)
}
