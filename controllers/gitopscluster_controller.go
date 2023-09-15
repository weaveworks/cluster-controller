/*
Copyright 2022.

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

package controllers

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/fluxcd/pkg/apis/meta"
	"github.com/fluxcd/pkg/runtime/conditions"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"

	gitopsv1alpha1 "github.com/weaveworks/cluster-controller/api/v1alpha1"
)

// GitOpsClusterFinalizer is the finalizer key used to detect when we need to
// finalize a GitOps cluster.
const GitOpsClusterFinalizer = "clusters.gitops.weave.works"

// GitOpsClusterProvisionedAnnotation if applied to a GitOpsCluster indicates
// that it should have a ready Provisioned condition.
const GitOpsClusterProvisionedAnnotation = "clusters.gitops.weave.works/provisioned"

const (
	// SecretNameIndexKey is the key used for indexing secret
	// resources based on their name.
	SecretNameIndexKey string = "SecretNameIndexKey"
	// CAPIClusterNameIndexKey is the key used for indexing CAPI cluster
	// resources based on their name.
	CAPIClusterNameIndexKey string = "CAPIClusterNameIndexKey"

	// MissingSecretRequeueTime is the period after which a secret will be
	// checked if it doesn't exist.
	MissingSecretRequeueTime = time.Second * 30
)

type eventRecorder interface {
	Event(object runtime.Object, eventType, reason, message string)
}

// GitopsClusterReconciler reconciles a GitopsCluster object
type GitopsClusterReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	Options       Options
	ConfigParser  func(b []byte) (client.Client, error)
	EventRecorder eventRecorder

	lastConnectivityCheck map[string]time.Time
}

// Options configures the GitOpsClusterReconciler features.
type Options struct {
	CAPIEnabled        bool
	DefaultRequeueTime time.Duration
}

// NewGitopsClusterReconciler creates and returns a configured
// reconciler ready for use.
func NewGitopsClusterReconciler(c client.Client, s *runtime.Scheme, recorder eventRecorder, opts Options) *GitopsClusterReconciler {
	return &GitopsClusterReconciler{
		Client:                c,
		Scheme:                s,
		EventRecorder:         recorder,
		Options:               opts,
		lastConnectivityCheck: map[string]time.Time{},
	}
}

// +kubebuilder:rbac:groups=gitops.weave.works,resources=gitopsclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=gitops.weave.works,resources=gitopsclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=gitops.weave.works,resources=gitopsclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;watch;list
// +kubebuilder:rbac:groups="cluster.x-k8s.io",resources=clusters,verbs=get;watch;list

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *GitopsClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Fetch the Cluster
	cluster := &gitopsv1alpha1.GitopsCluster{}
	if err := r.Get(ctx, req.NamespacedName, cluster); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Check if CAPI component is enabled
	if cluster.Spec.CAPIClusterRef != nil && !r.Options.CAPIEnabled {
		log.Info(
			"CAPIClusterRef found but CAPI support is disabled, ignoring.",
			"CAPI cluster",
			cluster.Spec.CAPIClusterRef.Name)

		e := fmt.Errorf(
			"CAPIClusterRef %q found but CAPI support is disabled",
			cluster.Spec.CAPIClusterRef.Name)

		conditions.MarkFalse(cluster, meta.ReadyCondition, gitopsv1alpha1.CAPINotEnabled, e.Error())

		if err := r.Status().Update(ctx, cluster); err != nil {
			log.Error(err, "failed to update Cluster status")
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, nil
	}

	// examine DeletionTimestamp to determine if object is under deletion
	if cluster.ObjectMeta.DeletionTimestamp.IsZero() {
		if cluster.Spec.SecretRef != nil || cluster.Spec.CAPIClusterRef != nil {
			if !controllerutil.ContainsFinalizer(cluster, GitOpsClusterFinalizer) {
				controllerutil.AddFinalizer(cluster, GitOpsClusterFinalizer)
				if err := r.Update(ctx, cluster); err != nil {
					return ctrl.Result{}, err
				}
			}
		}
	} else {
		if controllerutil.ContainsFinalizer(cluster, GitOpsClusterFinalizer) {
			err := r.reconcileDeletedReferences(ctx, cluster)
			if err != nil {
				return ctrl.Result{}, err
			}
			controllerutil.RemoveFinalizer(cluster, GitOpsClusterFinalizer)
			if err := r.Update(ctx, cluster); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil
		}
	}

	if cluster.Spec.SecretRef != nil {
		name := types.NamespacedName{
			Namespace: cluster.GetNamespace(),
			Name:      cluster.Spec.SecretRef.Name,
		}

		if metav1.HasAnnotation(cluster.ObjectMeta, GitOpsClusterProvisionedAnnotation) {
			conditions.MarkTrue(cluster, gitopsv1alpha1.ClusterProvisionedCondition, gitopsv1alpha1.ClusterProvisionedReason, "Cluster Provisioned annotation detected")
		}

		var secret corev1.Secret
		if err := r.Get(ctx, name, &secret); err != nil {
			e := fmt.Errorf("failed to get secret %q: %w", name, err)
			if apierrors.IsNotFound(err) {
				// TODO: this could _possibly_ be controllable by the
				// `GitopsCluster` itself.
				log.Info("waiting for cluster secret to be available")
				conditions.MarkFalse(cluster, meta.ReadyCondition, gitopsv1alpha1.WaitingForSecretReason, e.Error())
				if err := r.Status().Update(ctx, cluster); err != nil {
					log.Error(err, "failed to update Cluster status")
					return ctrl.Result{}, err
				}
				return ctrl.Result{RequeueAfter: MissingSecretRequeueTime}, nil
			}
			conditions.MarkFalse(cluster, meta.ReadyCondition, gitopsv1alpha1.WaitingForSecretReason, e.Error())
			if err := r.Status().Update(ctx, cluster); err != nil {
				log.Error(err, "failed to update Cluster status")
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, e
		}

		log.Info("Secret found", "secret", name)

		conditions.MarkTrue(cluster, meta.ReadyCondition, gitopsv1alpha1.SecretFoundReason, "")
		if err := r.Status().Update(ctx, cluster); err != nil {
			log.Error(err, "failed to update Cluster status")
			return ctrl.Result{}, err
		}
	}

	if cluster.Spec.CAPIClusterRef != nil {
		name := types.NamespacedName{
			Namespace: cluster.GetNamespace(),
			Name:      cluster.Spec.CAPIClusterRef.Name,
		}
		var capiCluster clusterv1.Cluster
		if err := r.Get(ctx, name, &capiCluster); err != nil {
			e := fmt.Errorf("failed to get CAPI cluster %q: %w", name, err)
			conditions.MarkFalse(cluster, meta.ReadyCondition, gitopsv1alpha1.WaitingForCAPIClusterReason, e.Error())
			if err := r.Status().Update(ctx, cluster); err != nil {
				log.Error(err, "failed to update Cluster status")
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, e
		}

		log.Info("CAPI Cluster found", "CAPI cluster", name)

		if !capiCluster.Status.ControlPlaneReady {
			conditions.MarkFalse(cluster, meta.ReadyCondition, gitopsv1alpha1.WaitingForControlPlaneReadyStatusReason, "Waiting for ControlPlaneReady status")
		} else {
			conditions.MarkTrue(cluster, meta.ReadyCondition, gitopsv1alpha1.ControlPlaneReadyStatusReason, "")
		}
		if clusterv1.ClusterPhase(capiCluster.Status.Phase) == clusterv1.ClusterPhaseProvisioned {
			conditions.MarkTrue(cluster, gitopsv1alpha1.ClusterProvisionedCondition, gitopsv1alpha1.ClusterProvisionedReason, "CAPI Cluster has been provisioned")
		}
		if err := r.Status().Update(ctx, cluster); err != nil {
			log.Error(err, "failed to update Cluster status")
			return ctrl.Result{}, err
		}
	}

	if err := r.verifyConnectivity(ctx, cluster); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: r.Options.DefaultRequeueTime}, nil
}

func (r *GitopsClusterReconciler) reconcileDeletedReferences(ctx context.Context, gc *gitopsv1alpha1.GitopsCluster) error {
	log := log.FromContext(ctx)

	if gc.Spec.CAPIClusterRef != nil {
		var capiCluster clusterv1.Cluster
		name := types.NamespacedName{
			Namespace: gc.GetNamespace(),
			Name:      gc.Spec.CAPIClusterRef.Name,
		}
		if err := r.Get(ctx, name, &capiCluster); err != nil {
			return client.IgnoreNotFound(err)
		}

		conditions.MarkFalse(gc, meta.ReadyCondition,
			gitopsv1alpha1.WaitingForCAPIClusterDeletionReason,
			"waiting for CAPI cluster to be deleted")
		if err := r.Status().Update(ctx, gc); err != nil {
			log.Error(err, "failed to update Cluster status")
			return err
		}
		return errors.New("waiting for CAPI cluster to be deleted")
	}

	if gc.Spec.SecretRef != nil {
		var secret corev1.Secret
		name := types.NamespacedName{
			Namespace: gc.GetNamespace(),
			Name:      gc.Spec.SecretRef.Name,
		}
		if err := r.Get(ctx, name, &secret); err != nil {
			return client.IgnoreNotFound(err)
		}

		conditions.MarkFalse(gc, meta.ReadyCondition,
			gitopsv1alpha1.WaitingForSecretDeletionReason,
			"waiting for access secret to be deleted")
		if err := r.Status().Update(ctx, gc); err != nil {
			log.Error(err, "failed to update Cluster status")
			return err
		}
		return errors.New("waiting for access secret to be deleted")
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *GitopsClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetCache().IndexField(context.TODO(), &gitopsv1alpha1.GitopsCluster{}, SecretNameIndexKey, r.indexGitopsClusterBySecretName); err != nil {
		return fmt.Errorf("failed setting index fields: %w", err)
	}

	if err := mgr.GetCache().IndexField(context.TODO(), &gitopsv1alpha1.GitopsCluster{}, CAPIClusterNameIndexKey, r.indexGitopsClusterByCAPIClusterName); err != nil {
		return fmt.Errorf("failed setting index fields: %w", err)
	}

	builder := ctrl.NewControllerManagedBy(mgr).
		For(&gitopsv1alpha1.GitopsCluster{}).
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.requestsForSecretChange),
		)

	if r.Options.CAPIEnabled {
		builder.Watches(
			&clusterv1.Cluster{},
			handler.EnqueueRequestsFromMapFunc(r.requestsForCAPIClusterChange),
		)

	}

	return builder.Complete(r)
}

func (r *GitopsClusterReconciler) indexGitopsClusterBySecretName(o client.Object) []string {
	c, ok := o.(*gitopsv1alpha1.GitopsCluster)
	if !ok {
		panic(fmt.Sprintf("Expected a GitopsCluster, got %T", o))
	}

	if c.Spec.SecretRef != nil {
		return []string{c.Spec.SecretRef.Name}
	}

	return nil
}

func (r *GitopsClusterReconciler) indexGitopsClusterByCAPIClusterName(o client.Object) []string {
	c, ok := o.(*gitopsv1alpha1.GitopsCluster)
	if !ok {
		panic(fmt.Sprintf("Expected a GitopsCluster, got %T", o))
	}

	if c.Spec.CAPIClusterRef != nil {
		return []string{c.Spec.CAPIClusterRef.Name}
	}

	return nil
}

func (r *GitopsClusterReconciler) requestsForSecretChange(ctx context.Context, o client.Object) []ctrl.Request {
	secret, ok := o.(*corev1.Secret)
	if !ok {
		panic(fmt.Sprintf("Expected a Secret but got a %T", o))
	}

	var list gitopsv1alpha1.GitopsClusterList
	if err := r.Client.List(ctx, &list, client.MatchingFields{SecretNameIndexKey: secret.GetName()}); err != nil {
		return nil
	}

	var reqs []ctrl.Request
	for _, i := range list.Items {
		name := client.ObjectKey{Namespace: i.Namespace, Name: i.Name}
		reqs = append(reqs, ctrl.Request{NamespacedName: name})
	}
	return reqs
}

func (r *GitopsClusterReconciler) requestsForCAPIClusterChange(ctx context.Context, o client.Object) []ctrl.Request {
	cluster, ok := o.(*clusterv1.Cluster)
	if !ok {
		panic(fmt.Sprintf("Expected a CAPI Cluster but got a %T", o))
	}

	var list gitopsv1alpha1.GitopsClusterList
	if err := r.Client.List(ctx, &list, client.MatchingFields{CAPIClusterNameIndexKey: cluster.GetName()}); err != nil {
		return nil
	}

	var reqs []ctrl.Request
	for _, i := range list.Items {
		name := client.ObjectKey{Namespace: i.Namespace, Name: i.Name}
		reqs = append(reqs, ctrl.Request{NamespacedName: name})
	}
	return reqs
}

func (r *GitopsClusterReconciler) verifyConnectivity(ctx context.Context, cluster *gitopsv1alpha1.GitopsCluster) error {
	log := log.FromContext(ctx)

	// avoid checking the cluster if it's under deletion.
	if !cluster.ObjectMeta.DeletionTimestamp.IsZero() {
		return nil
	}

	log.Info("checking connectivity", "cluster", cluster.Name)

	nsName := types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Name}

	lastCheck := r.lastConnectivityCheck[nsName.String()]

	if time.Since(lastCheck) < 30*time.Second {
		return nil
	}

	r.lastConnectivityCheck[nsName.String()] = time.Now()

	config, err := r.restConfigFromSecret(ctx, cluster)
	if err != nil {
		conditions.MarkFalse(cluster, gitopsv1alpha1.ClusterConnectivity, gitopsv1alpha1.ClusterConnectionFailedReason, fmt.Sprintf("failed creating rest config from secret: %s", err))
		if err := r.Status().Update(ctx, cluster); err != nil {
			log.Error(err, "failed to update Cluster status")
			return err
		}

		return nil
	}

	if _, err := client.New(config, client.Options{}); err != nil {
		conditions.MarkFalse(cluster, gitopsv1alpha1.ClusterConnectivity, gitopsv1alpha1.ClusterConnectionFailedReason, fmt.Sprintf("failed connecting to the cluster: %s", err))
		if err := r.Status().Update(ctx, cluster); err != nil {
			log.Error(err, "failed to update Cluster status")
			return err
		}

		return nil
	}

	conditions.MarkTrue(cluster, gitopsv1alpha1.ClusterConnectivity, gitopsv1alpha1.ClusterConnectionSucceededReason, "cluster connectivity is ok")
	if err := r.Status().Update(ctx, cluster); err != nil {
		log.Error(err, "failed to update Cluster status")
		return err
	}

	return nil
}

func (r *GitopsClusterReconciler) restConfigFromSecret(ctx context.Context, cluster *gitopsv1alpha1.GitopsCluster) (*rest.Config, error) {
	log := log.FromContext(ctx)

	var secretRef string

	if cluster.Spec.CAPIClusterRef != nil {
		secretRef = fmt.Sprintf("%s-kubeconfig", cluster.Spec.CAPIClusterRef.Name)
	}

	if secretRef == "" && cluster.Spec.SecretRef != nil {
		secretRef = cluster.Spec.SecretRef.Name
	}

	if secretRef == "" {
		return nil, errors.New("no secret ref found")
	}

	key := types.NamespacedName{
		Name:      secretRef,
		Namespace: cluster.Namespace,
	}

	var secret v1.Secret
	if err := r.Get(ctx, key, &secret); err != nil {
		log.Error(err, "unable to fetch secret for GitOps Cluster", "cluster", cluster.Name)

		return nil, err
	}

	var data []byte

	for k := range secret.Data {
		if k == "value" || k == "value.yaml" {
			data = secret.Data[k]

			break
		}
	}

	if len(data) == 0 {
		return nil, errors.New("no data present in cluster secret")
	}

	restCfg, err := clientcmd.RESTConfigFromKubeConfig([]byte(data))
	if err != nil {
		log.Error(err, "unable to create kubconfig from GitOps Cluster secret data", "cluster", cluster.Name)

		return nil, err
	}

	return restCfg, nil
}
