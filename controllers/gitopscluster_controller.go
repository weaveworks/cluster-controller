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
	"fmt"
	"time"

	"github.com/fluxcd/pkg/apis/meta"
	"github.com/fluxcd/pkg/runtime/conditions"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/source"

	gitopsv1alpha1 "github.com/weaveworks/cluster-controller/api/v1alpha1"
)

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

// GitopsClusterReconciler reconciles a GitopsCluster object
type GitopsClusterReconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	ConfigParser func(b []byte) (client.Client, error)
}

// NewGitopsClusterReconciler creates and returns a configured
// reconciler ready for use.
func NewGitopsClusterReconciler(c client.Client, s *runtime.Scheme) *GitopsClusterReconciler {
	return &GitopsClusterReconciler{
		Client:       c,
		Scheme:       s,
		ConfigParser: kubeConfigBytesToClient,
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
		log.Error(err, "failed to get Cluster")

		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if cluster.Spec.SecretRef != nil {
		name := types.NamespacedName{
			Namespace: cluster.GetNamespace(),
			Name:      cluster.Spec.SecretRef.Name,
		}
		var secret corev1.Secret
		if err := r.Get(ctx, name, &secret); err != nil {
			if apierrors.IsNotFound(err) {
				// TODO: this could _possibly_ be controllable by the
				// `GitopsCluster` itself.
				log.Info("waiting for cluster secret to be available")
				conditions.MarkFalse(cluster, meta.ReadyCondition, gitopsv1alpha1.WaitingForSecretReason, "")
				if err := r.Status().Update(ctx, cluster); err != nil {
					log.Error(err, "failed to update Cluster status")
					return ctrl.Result{}, err
				}
				return ctrl.Result{RequeueAfter: MissingSecretRequeueTime}, nil
			}
			e := fmt.Errorf("failed to get secret %q: %w", name, err)
			conditions.MarkFalse(cluster, meta.ReadyCondition, gitopsv1alpha1.WaitingForSecretReason, e.Error())
			if err := r.Status().Update(ctx, cluster); err != nil {
				log.Error(err, "failed to update Cluster status")
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, e
		}

		log.Info("Secret found", "secret", name)

		ready, err := r.checkControlPlaneReadiness(ctx, *cluster)
		if err != nil {
			conditions.MarkFalse(cluster, meta.ReadyCondition, gitopsv1alpha1.WaitingForSecretReason, err.Error())
			if err := r.Status().Update(ctx, cluster); err != nil {
				log.Error(err, "failed to update Cluster status")
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, err
		}

		if !ready {
			log.Info("waiting for control plane to be ready", "cluster", cluster.Name)

			return ctrl.Result{RequeueAfter: cluster.ClusterReadinessRequeue()}, nil
		}

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

		ready, err := r.checkControlPlaneReadiness(ctx, *cluster)
		if err != nil {
			conditions.MarkFalse(cluster, meta.ReadyCondition, gitopsv1alpha1.WaitingForCAPIClusterReason, err.Error())
			if err := r.Status().Update(ctx, cluster); err != nil {
				log.Error(err, "failed to update Cluster status")
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, fmt.Errorf("failed to check readiness of cluster %s: %w", cluster.Name, err)
		}

		if !ready {
			log.Info("waiting for control plane to be ready", "cluster", cluster.Name)

			return ctrl.Result{RequeueAfter: cluster.ClusterReadinessRequeue()}, nil
		}

		conditions.MarkTrue(cluster, meta.ReadyCondition, gitopsv1alpha1.CAPIClusterFoundReason, "")
		if err := r.Status().Update(ctx, cluster); err != nil {
			log.Error(err, "failed to update Cluster status")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *GitopsClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetCache().IndexField(context.TODO(), &gitopsv1alpha1.GitopsCluster{}, SecretNameIndexKey, r.indexGitopsClusterBySecretName); err != nil {
		return fmt.Errorf("failed setting index fields: %w", err)
	}

	if err := mgr.GetCache().IndexField(context.TODO(), &gitopsv1alpha1.GitopsCluster{}, CAPIClusterNameIndexKey, r.indexGitopsClusterByCAPIClusterName); err != nil {
		return fmt.Errorf("failed setting index fields: %w", err)
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&gitopsv1alpha1.GitopsCluster{}).
		Watches(
			&source.Kind{Type: &corev1.Secret{}},
			handler.EnqueueRequestsFromMapFunc(r.requestsForSecretChange),
		).
		Watches(
			&source.Kind{Type: &clusterv1.Cluster{}},
			handler.EnqueueRequestsFromMapFunc(r.requestsForCAPIClusterChange),
		).
		Complete(r)
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

func (r *GitopsClusterReconciler) requestsForSecretChange(o client.Object) []ctrl.Request {
	secret, ok := o.(*corev1.Secret)
	if !ok {
		panic(fmt.Sprintf("Expected a Secret but got a %T", o))
	}

	ctx := context.Background()
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

func (r *GitopsClusterReconciler) requestsForCAPIClusterChange(o client.Object) []ctrl.Request {
	cluster, ok := o.(*clusterv1.Cluster)
	if !ok {
		panic(fmt.Sprintf("Expected a CAPI Cluster but got a %T", o))
	}

	ctx := context.Background()
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

func (r *GitopsClusterReconciler) checkControlPlaneReadiness(ctx context.Context, cluster gitopsv1alpha1.GitopsCluster) (bool, error) {
	clusterName := types.NamespacedName{Name: cluster.GetName(), Namespace: cluster.GetNamespace()}
	clusterClient, err := r.clientForCluster(ctx, cluster)
	if err != nil {
		return false, fmt.Errorf("failed to create client of cluster %s: %w", clusterName, err)
	}

	ready, err := IsControlPlaneReady(ctx, clusterClient)
	if err != nil {
		return false, fmt.Errorf("failed to check readiness of cluster %s: %w", clusterName, err)
	}

	if !ready {
		return false, nil
	}

	return true, nil
}

func (r *GitopsClusterReconciler) clientForCluster(ctx context.Context, cluster gitopsv1alpha1.GitopsCluster) (client.Client, error) {
	kubeConfigBytes, err := r.getKubeConfig(ctx, cluster)
	if err != nil {
		return nil, err
	}

	client, err := r.ConfigParser(kubeConfigBytes)
	if err != nil {
		return nil, fmt.Errorf("getting client for cluster %s: %w", cluster.Name, err)
	}
	return client, nil
}

func (r *GitopsClusterReconciler) getKubeConfig(ctx context.Context, cluster gitopsv1alpha1.GitopsCluster) ([]byte, error) {
	var name string
	if cluster.Spec.CAPIClusterRef != nil {
		name = cluster.Spec.CAPIClusterRef.Name + "-kubeconfig"
	} else {
		name = cluster.Spec.SecretRef.Name
	}

	secretName := types.NamespacedName{
		Namespace: cluster.GetNamespace(),
		Name:      name,
	}

	var secret corev1.Secret
	if err := r.Client.Get(ctx, secretName, &secret); err != nil {
		return nil, fmt.Errorf("unable to read KubeConfig secret %q error: %w", secretName, err)
	}

	var kubeConfig []byte
	for k := range secret.Data {
		if k == "value" || k == "value.yaml" {
			kubeConfig = secret.Data[k]
			break
		}
	}

	if len(kubeConfig) == 0 {
		return nil, fmt.Errorf("KubeConfig secret %q doesn't contain a 'value' key ", secretName)
	}

	return kubeConfig, nil
}

func kubeConfigBytesToClient(b []byte) (client.Client, error) {
	restConfig, err := clientcmd.RESTConfigFromKubeConfig(b)
	if err != nil {
		return nil, fmt.Errorf("failed to parse KubeConfig from secret: %w", err)
	}
	restMapper, err := apiutil.NewDynamicRESTMapper(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create RESTMapper from config: %w", err)
	}

	client, err := client.New(restConfig, client.Options{Mapper: restMapper})
	if err != nil {
		return nil, fmt.Errorf("failed to create a client from config: %w", err)
	}
	return client, nil
}
