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

package controllers_test

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/weaveworks/cluster-controller/controllers"
	"github.com/weaveworks/cluster-controller/test"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	gitopsv1alpha1 "github.com/weaveworks/cluster-controller/api/v1alpha1"
	//+kubebuilder:scaffold:imports
)

var k8sManager ctrl.Manager
var k8sClient client.Client
var testEnv *envtest.Environment
var kubeConfig []byte

func TestMain(m *testing.M) {
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	cfg, err := testEnv.Start()
	if err != nil {
		log.Fatalf("starting test env failed: %s", err)
	}

	user, err := testEnv.ControlPlane.AddUser(envtest.User{
		Name:   "envtest-admin",
		Groups: []string{"system:masters"},
	}, nil)
	if err != nil {
		log.Fatalf("add user failed: %s", err)
	}

	kubeConfig, err = user.KubeConfig()
	if err != nil {
		log.Fatalf("get user kubeconfig failed: %s", err)
	}

	err = gitopsv1alpha1.AddToScheme(scheme.Scheme)
	if err != nil {
		log.Fatalf("add GitopsCluster to schema failed: %s", err)
	}

	k8sManager, err = ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
	})
	if err != nil {
		log.Fatalf("initializing controller manager failed: %s", err)
	}

	r := controllers.NewGitopsClusterReconciler(k8sManager.GetClient(), scheme.Scheme, &test.FakeEventRecorder{}, controllers.Options{})
	err = (r).SetupWithManager(k8sManager)
	if err != nil {
		log.Fatalf("setup cluster controller failed: %s", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		err = k8sManager.Start(ctx)
		if err != nil {
			log.Fatalf("starting controller manager failed: %s", err)
		}
	}()

	k8sClient = k8sManager.GetClient()
	if k8sClient == nil {
		log.Fatalf("failed getting k8s client: k8sManager.GetClient() returned nil")
	}

	retCode := m.Run()

	cancel()

	err = testEnv.Stop()
	if err != nil {
		log.Fatalf("stoping test env failed: %s", err)
	}

	os.Exit(retCode)

}
