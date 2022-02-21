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
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

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
		log.Error(err, "unable to fetch synapse")
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Get and validate homeserver ConfigMap
	var cm corev1.ConfigMap
	if err := r.Get(ctx, types.NamespacedName{Name: synapse.Spec.HomeserverConfigMapName, Namespace: synapse.Namespace}, &cm); err != nil {
		log.Error(err, "Failed to get ConfigMap", "ConfigMap.Namespace", synapse.Namespace, "ConfigMap.Name", synapse.Spec.HomeserverConfigMapName)
		return ctrl.Result{RequeueAfter: time.Duration(30)}, err
	}

	if err := r.ParseHomeserverConfigMap(ctx, &synapse, cm); err != nil {
		return ctrl.Result{RequeueAfter: time.Duration(30)}, err
	}

	// Reconcile Synapse resources: PVC, Deployment and Service
	var objectMeta metav1.ObjectMeta

	if synapse.Spec.CreateNewPostgreSQL {
		if result, err := r.createPostgresClusterForSynapse(ctx, synapse, cm); err != nil {
			return result, err
		}
	}

	objectMeta = setObjectMeta(synapse.Name, synapse.Namespace, map[string]string{})
	r.reconcileResource(r.persistentVolumeClaimForSynapse, &synapse, &corev1.PersistentVolumeClaim{}, objectMeta)

	objectMeta = setObjectMeta(synapse.Name, synapse.Namespace, map[string]string{})
	r.reconcileResource(r.deploymentForSynapse, &synapse, &appsv1.Deployment{}, objectMeta)
	// TODO: If a deployment is found, check that its Spec are correct.

	objectMeta = setObjectMeta(synapse.Name, synapse.Namespace, map[string]string{})
	r.reconcileResource(r.serviceForSynapse, &synapse, &corev1.Service{}, objectMeta)

	// Update the Synapse status if needed
	if synapse.Status.State != "RUNNING" {
		synapse.Status.State = "RUNNING"
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

// ParseHomeserverConfigMap loads the ConfigMap, which name is determined by
// Spec.HomeserverConfigMapName, run validation checks and fetch necesarry
// value needed to configure the Synapse Deployment.
func (r *SynapseReconciler) ParseHomeserverConfigMap(ctx context.Context, synapse *synapsev1alpha1.Synapse, cm corev1.ConfigMap) error {
	log := ctrllog.FromContext(ctx)

	// TODO:
	// - Ensure that key path is and log config file path are in /data
	// - Otherwise, edit homeserver.yaml with new paths

	// Load and validate homeserver.yaml
	homeserver := make(map[string]interface{})
	cm_data, ok := cm.Data["homeserver.yaml"]
	if !ok {
		err := errors.New("missing homeserver.yaml in ConfigMap")
		log.Error(err, "Missing homeserver.yaml in ConfigMap", "ConfigMap.Namespace", synapse.Namespace, "ConfigMap.Name", synapse.Spec.HomeserverConfigMapName)
		return err
	}

	// YAML Validation
	if err := yaml.Unmarshal([]byte(cm_data), homeserver); err != nil {
		log.Error(err, "Malformed homeserver.yaml")
		return err
	}

	// Fetch server_name and report_stats
	if _, ok := homeserver["server_name"]; !ok {
		err := errors.New("missing server_name key in homeserver.yaml")
		log.Error(err, "Missing server_name key in homeserver.yaml")
		return err
	}
	server_name, ok := homeserver["server_name"].(string)
	if !ok {
		err := errors.New("error converting server_name to string")
		log.Error(err, "Error converting server_name to string")
		return err
	}

	if _, ok := homeserver["report_stats"]; !ok {
		err := errors.New("missing report_stats key in homeserver.yaml")
		log.Error(err, "Missing report_stats key in homeserver.yaml")
		return err
	}
	report_stats, ok := homeserver["report_stats"].(bool)
	if !ok {
		err := errors.New("error converting report_stats to bool")
		log.Error(err, "Error converting report_stats to bool")
		return err
	}

	synapse.Status.HomeserverConfiguration.ServerName = server_name
	synapse.Status.HomeserverConfiguration.ReportStats = report_stats

	log.Info(
		"Loaded homeserver.yaml from ConfigMap successfully",
		"server_name:", synapse.Status.HomeserverConfiguration.ServerName,
		"report_stats:", synapse.Status.HomeserverConfiguration.ReportStats,
	)

	return nil
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
	r.reconcileResource(r.configMapForPostgresCluster, &synapse, &corev1.ConfigMap{}, objectMeta)

	// Create PostgresCluster for Synapse
	objectMeta = setObjectMeta(synapse.Name, synapse.Namespace, map[string]string{})
	r.reconcileResource(r.postgresClusterForSynapse, &synapse, &createdPostgresCluster, objectMeta)

	// Wait for PostgresCluster to be up
	if err := r.Get(ctx, types.NamespacedName{Name: createdPostgresCluster.Name, Namespace: createdPostgresCluster.Namespace}, &createdPostgresCluster); err != nil {
		return ctrl.Result{}, err
	}
	if !r.isPostgresClusterReady(createdPostgresCluster) {
		r.updateSynapseStatusDatabaseState(ctx, synapse, "NOT READY")
		// TODO: update synapse.status.databaseConnectionInfo.State
		err := errors.New("postgreSQL Database not ready yet")
		return ctrl.Result{RequeueAfter: time.Duration(5)}, err
	}

	// Update Synapse Status with PostgreSQL DB information
	if err := r.updateSynapseStatusWithPostgreSQLInfos(ctx, &synapse, createdPostgresCluster); err != nil {
		return ctrl.Result{}, err
	}

	// Update configMap data with PostgreSQL DB information
	if err := r.updateSynapseConfigMap(ctx, &cm, synapse); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *SynapseReconciler) isPostgresClusterReady(p pgov1beta1.PostgresCluster) bool {
	var status_found bool

	// // Get latest version of PostgresCluster
	// if err := r.Client.Get(context.TODO(), types.NamespacedName{Name: p.Name, Namespace: p.Namespace}, &p); err != nil {
	// 	return false
	// }

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

func (r *SynapseReconciler) updateSynapseStatusDatabaseState(ctx context.Context, synapse synapsev1alpha1.Synapse, state string) error {
	current := &synapsev1alpha1.Synapse{}

	r.Get(
		ctx,
		types.NamespacedName{Name: synapse.Name, Namespace: synapse.Namespace},
		current,
	)

	synapse.Status.DatabaseConnectionInfo.State = state

	if !reflect.DeepEqual(synapse.Status, current.Status) {
		if err := r.Status().Patch(ctx, &synapse, client.MergeFrom(current)); err != nil {
			return err
		}
	}

	return nil
}

func (r *SynapseReconciler) updateSynapseStatusWithPostgreSQLInfos(
	ctx context.Context,
	s *synapsev1alpha1.Synapse,
	createdPostgresCluster pgov1beta1.PostgresCluster,
) error {
	var postgresSecret corev1.Secret

	// Get latest Synapse version
	current := &synapsev1alpha1.Synapse{}
	if err := r.Get(
		ctx,
		types.NamespacedName{Name: s.Name, Namespace: s.Namespace},
		current,
	); err != nil {
		return err
	}

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

	if !reflect.DeepEqual(s.Status, current.Status) {
		if err := r.Status().Patch(ctx, s, client.MergeFrom(current)); err != nil {
			return err
		}
	}

	return nil
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

func (r *SynapseReconciler) updateSynapseConfigMap(
	ctx context.Context,
	cm *corev1.ConfigMap,
	s synapsev1alpha1.Synapse,
) error {
	// Get latest ConfigMap version
	if err := r.Get(
		ctx,
		types.NamespacedName{Name: cm.Name, Namespace: cm.Namespace},
		cm,
	); err != nil {
		return err
	}

	if err := r.updateSynapseConfigMapData(cm, s); err != nil {
		return err
	}

	// Update ConfigMap
	if err := r.Client.Update(ctx, cm); err != nil {
		return err
	}

	return nil
}

func (r *SynapseReconciler) updateSynapseConfigMapData(
	cm *corev1.ConfigMap,
	s synapsev1alpha1.Synapse,
) error {
	homeserver := make(map[string]interface{})
	databaseData := HomeserverPgsqlDatabase{}

	// Check if s.Status.DatabaseConnectionInfo contains necessary information
	if s.Status.DatabaseConnectionInfo == (synapsev1alpha1.SynapseStatusDatabaseConnectionInfo{}) {
		err := errors.New("missing DatabaseConnectionInfo in Synapse status")
		return err
	}

	if s.Status.DatabaseConnectionInfo.User == "" {
		err := errors.New("missing User in DatabaseConnectionInfo")
		return err
	}

	if s.Status.DatabaseConnectionInfo.Password == "" {
		err := errors.New("missing Password in DatabaseConnectionInfo")
		return err
	}
	decodedPassword := base64decode([]byte(s.Status.DatabaseConnectionInfo.Password))

	if s.Status.DatabaseConnectionInfo.DatabaseName == "" {
		err := errors.New("missing DatabaseName in DatabaseConnectionInfo")
		return err
	}

	if s.Status.DatabaseConnectionInfo.ConnectionURL == "" {
		err := errors.New("missing ConnectionURL in DatabaseConnectionInfo")
		return err
	}
	connectionURL := strings.Split(s.Status.DatabaseConnectionInfo.ConnectionURL, ":")
	if len(connectionURL) < 2 {
		err := errors.New("error parsing the Connection URL with value: " + s.Status.DatabaseConnectionInfo.ConnectionURL)
		return err
	}
	port, err := strconv.ParseInt(connectionURL[1], 10, 64)
	if err != nil {
		return err
	}

	// Populate databaseData
	databaseData.Name = "psycopg2"
	databaseData.Args.User = s.Status.DatabaseConnectionInfo.User
	databaseData.Args.Password = decodedPassword
	databaseData.Args.Database = s.Status.DatabaseConnectionInfo.DatabaseName
	databaseData.Args.Host = connectionURL[0]
	databaseData.Args.Port = port
	databaseData.Args.CpMin = 5
	databaseData.Args.CpMax = 10

	// Convert databaseData into a map[string]interface{}
	databaseDataMap, err := r.convertStructToMap(databaseData)
	if err != nil {
		return err
	}

	// Load homeserver.yaml from ConfigMap
	cm_data, ok := cm.Data["homeserver.yaml"]
	if !ok {
		err := errors.New("missing homeserver.yaml in ConfigMap")
		return err
	}
	if err := yaml.Unmarshal([]byte(cm_data), homeserver); err != nil {
		return err
	}

	// Save new database section of homeserver.yaml
	homeserver["database"] = databaseDataMap

	// Write homeserver.yaml into ConfigMap data
	if configMapData, err := yaml.Marshal(homeserver); err != nil {
		return err
	} else {
		cm.Data = map[string]string{"homeserver.yaml": string(configMapData)}
	}

	return nil
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
