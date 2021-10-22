// Copyright (c) 2021 Terminus, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package controllers

import (
	"context"
	"encoding/json"
	"reflect"
	"time"

	errorx "github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	appv1 "github.com/erda-project/erda-sourcecov/api/v1alpha1"
)

// AgentReconciler reconciles a Agent object
type AgentReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=sourcecov.erda.cloud,resources=agents,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=sourcecov.erda.cloud,resources=agents/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=sourcecov.erda.cloud,resources=agents/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps,resources=deployments/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps,resources=statefulsets/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch
//+kubebuilder:rbac:groups=core,resources=pods/exec,verbs=create

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.9.2/pkg/reconcile

func (r *AgentReconciler) upsertResource(ctx context.Context, object client.Object) error {
	if err := r.Client.Create(ctx, object); err != nil {
		if errors.IsAlreadyExists(err) {
			return r.Client.Update(ctx, object)
		}

		return err
	}

	return nil
}

func (r *AgentReconciler) CreateResource(ctx context.Context, agent *appv1.Agent) (err error) {
	resources := ConvertAgent(agent, func(agent *appv1.Agent, controlled metav1.Object) {
		ctrl.SetControllerReference(agent, controlled, r.Scheme)
	})

	// create sa
	if err = r.upsertResource(ctx, resources.ServiceAccount); err != nil {
		err = errorx.WithStack(err)
		return
	}

	// create role
	if err = r.upsertResource(ctx, resources.Role); err != nil {
		err = errorx.WithStack(err)
		return
	}

	// create rolebinding
	if err = r.upsertResource(ctx, resources.RoleBinding); err != nil {
		err = errorx.WithStack(err)
		return
	}

	// create sts
	if err = r.Client.Create(ctx, resources.StatefulSet); err != nil {
		err = errorx.WithStack(err)
		return
	}

	// 更新 crd 资源的 Annotations
	data, _ := json.Marshal(agent.Spec)
	if agent.Annotations != nil {
		agent.Annotations["spec"] = string(data)
	} else {
		agent.Annotations = map[string]string{"spec": string(data)}
	}
	err = errorx.WithStack(r.Client.Update(ctx, agent))
	return
}

func (r *AgentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	logger := log.FromContext(ctx).WithValues("agent", req.NamespacedName)
	result = ctrl.Result{}

	defer func() {
		if err != nil {
			logger.Error(err, "failed to reconcile")
		}
	}()

	// 获取 crd 资源
	agent := &appv1.Agent{}
	if err = r.Client.Get(ctx, req.NamespacedName, agent); err != nil {
		if errors.IsNotFound(err) {
			err = nil
			return
		}
		err = errorx.WithStack(err)
		return
	}

	// crd 资源已经标记为删除
	if agent.DeletionTimestamp != nil {
		return
	}

	agent = FillDefaultValue(agent)

	oldDeploy := &appsv1.StatefulSet{}
	if err = r.Client.Get(ctx, req.NamespacedName, oldDeploy); err != nil {
		if !errors.IsNotFound(err) {
			err = errorx.WithStack(err)
			return
		}

		err = errorx.WithStack(r.CreateResource(ctx, agent))
		if err == nil {
			result = ctrl.Result{RequeueAfter: time.Minute}
		}
		return
	}

	// deployment 存在，更新
	oldSpec := appv1.AgentSpec{}
	if err = json.Unmarshal([]byte(agent.Annotations["spec"]), &oldSpec); err != nil {
		err = errorx.WithStack(err)
		return
	}

	if !reflect.DeepEqual(agent.Spec, oldSpec) {
		err = errorx.WithStack(r.UpdateResource(ctx, oldDeploy, agent))
		if err == nil {
			result = ctrl.Result{RequeueAfter: time.Minute}
		}
		return
	}

	stsList := &appsv1.StatefulSetList{}

	listOption := []client.ListOption{
		client.InNamespace(agent.Namespace),
		client.MatchingLabels(getLables(agent)),
	}

	err = errorx.Wrapf(r.Client.List(ctx, stsList, listOption...), "failed to list statefulset")
	if err != nil {
		return
	}

	if stsList.Size() == 0 {
		logger.Info("no sts found")
		return
	}

	if !reflect.DeepEqual(stsList.Items[0].Status, agent.Status.StatefulSetStatus) {
		stsList.Items[0].Status.DeepCopyInto(&agent.Status.StatefulSetStatus)
		err = errorx.Wrapf(r.Client.Status().Update(ctx, agent), "failed to update agent status")
	}

	if err == nil {
		logger.Info("succeed to update status")
	}

	return
}

func (r *AgentReconciler) UpdateResource(ctx context.Context, oldDeploy *appsv1.StatefulSet, agent *appv1.Agent) (err error) {
	resources := ConvertAgent(agent, func(agent *appv1.Agent, controlled metav1.Object) {
		ctrl.SetControllerReference(agent, controlled, r.Scheme)
	})

	if err = r.upsertResource(ctx, resources.Role); err != nil {
		err = errorx.WithStack(err)
		return
	}

	if err = r.upsertResource(ctx, resources.RoleBinding); err != nil {
		err = errorx.WithStack(err)
		return
	}

	if err = r.upsertResource(ctx, resources.ServiceAccount); err != nil {
		err = errorx.WithStack(err)
		return
	}

	// 更新deployment
	newDeploy := resources.StatefulSet
	oldDeploy.Spec = newDeploy.Spec
	if err = r.Client.Update(ctx, oldDeploy); err != nil {
		err = errorx.WithStack(err)
		return
	}

	// 更新 crd 资源的 Annotations
	data, _ := json.Marshal(agent.Spec)
	if agent.Annotations != nil {
		agent.Annotations["spec"] = string(data)
	} else {
		agent.Annotations = map[string]string{"spec": string(data)}
	}

	return errorx.WithStack(r.Client.Update(ctx, agent))
}

// SetupWithManager sets up the controller with the Manager.
func (r *AgentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appv1.Agent{}).
		Owns(&appsv1.StatefulSet{}).
		Complete(r)
}
