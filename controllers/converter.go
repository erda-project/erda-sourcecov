/**
 * Copyright 2021 Terminus.io
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package controllers

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	appv1 "github.com/erda-project/erda-sourcecov/api/v1alpha1"
)

type AgentResources struct {
	StatefulSet    *appsv1.StatefulSet
	ServiceAccount *corev1.ServiceAccount
	RoleBinding    *rbacv1.RoleBinding
	Role           *rbacv1.Role
}

func getNamePrefix(app *appv1.Agent) string {
	return "source-cov-" + app.Name
}

func getNamespace(app *appv1.Agent) string {
	return app.Namespace
}

func getServiceAccountName(app *appv1.Agent) string {
	return getNamePrefix(app) + "-sa"
}

func getRoleName(app *appv1.Agent) string {
	return getNamePrefix(app) + "-role"
}

func getLables(app *appv1.Agent) map[string]string {
	return map[string]string{
		"agents." + appv1.GroupVersion.Group: app.Name,
	}
}

func convertServiceAccount(app *appv1.Agent, setOwnerRef SetOwnerRefFunc) *corev1.ServiceAccount {
	sa := &corev1.ServiceAccount{
		ObjectMeta: v1.ObjectMeta{
			Name:      getServiceAccountName(app),
			Namespace: getNamespace(app),
		},
	}
	setOwnerRef(app, sa)
	return sa
}

func convertRole(app *appv1.Agent, setOwnerRef SetOwnerRefFunc) *rbacv1.Role {
	role := &rbacv1.Role{
		ObjectMeta: v1.ObjectMeta{
			Name:      getRoleName(app),
			Namespace: getNamespace(app),
			Labels:    getLables(app),
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"apps"},
				Resources: []string{"deployments"},
				Verbs:     []string{"get", "watch", "list"},
			},
			{
				APIGroups: []string{"apps"},
				Resources: []string{"statefulsets"},
				Verbs:     []string{"get", "watch", "list"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"pods"},
				Verbs:     []string{"get", "watch", "list"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"pods/exec"},
				Verbs:     []string{"create"},
			},
		},
	}

	setOwnerRef(app, role)
	return role
}

func convertRoleBinding(app *appv1.Agent, setOwnerRef SetOwnerRefFunc) *rbacv1.RoleBinding {
	roleBinding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      getNamePrefix(app) + "-rb",
			Namespace: getNamespace(app),
			Labels:    getLables(app),
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     getRoleName(app),
		},
		Subjects: []rbacv1.Subject{
			{
				Kind: "ServiceAccount",
				Name: getServiceAccountName(app),
			},
		},
	}

	setOwnerRef(app, roleBinding)
	return roleBinding
}

const volumeName = "data"

func convertSts(app *appv1.Agent, setOwnerRef SetOwnerRefFunc) *appsv1.StatefulSet {
	var replicas int32 = 1
	labels := getLables(app)

	svcAccountName := getNamePrefix(app) + "-sa"
	selector := &metav1.LabelSelector{MatchLabels: labels}

	var vct []corev1.PersistentVolumeClaim
	vct = append(vct, corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: volumeName,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: &app.Spec.StorageClassName,
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{"storage": app.Spec.StorageSize},
			},
		},
	})

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      app.Name,
			Namespace: getNamespace(app),
			Labels:    labels,
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			Selector: selector,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: app.Spec.Annotations,
				},
				Spec: corev1.PodSpec{
					Containers:         newContainers(app),
					ServiceAccountName: svcAccountName,
					Affinity:           app.Spec.Affinity,
					Tolerations:        app.Spec.Tolerations,
					NodeSelector:       app.Spec.NodeSelector,
				},
			},
			VolumeClaimTemplates: vct,
		},
	}

	setOwnerRef(app, sts)
	return sts
}

type SetOwnerRefFunc func(agent *appv1.Agent, controlled metav1.Object)

func ConvertAgent(app *appv1.Agent, setOwnerRef SetOwnerRefFunc) *AgentResources {
	return &AgentResources{
		StatefulSet:    convertSts(app, setOwnerRef),
		Role:           convertRole(app, setOwnerRef),
		RoleBinding:    convertRoleBinding(app, setOwnerRef),
		ServiceAccount: convertServiceAccount(app, setOwnerRef),
	}
}

func newContainers(app *appv1.Agent) []corev1.Container {
	var envs []corev1.EnvVar

	for _, env := range app.Spec.Env {
		envs = append(envs, corev1.EnvVar{
			Name:  env.Name,
			Value: env.Value,
		})
	}

	var volumeMounts []corev1.VolumeMount

	volumeMounts = append(volumeMounts, corev1.VolumeMount{
		Name:      volumeName,
		MountPath: app.Spec.VolumePath,
		ReadOnly:  false,
	})

	return []corev1.Container{
		{
			Name:            "sourcecov-agent",
			Image:           app.Spec.Image,
			Env:             envs,
			Resources:       *app.Spec.Resources,
			ImagePullPolicy: corev1.PullIfNotPresent,
			VolumeMounts:    volumeMounts,
		},
	}
}

func FillDefaultValue(origin *appv1.Agent) *appv1.Agent {
	app := origin.DeepCopy()

	if app.Spec.VolumePath == "" {
		app.Spec.VolumePath = "/jacoco/work"
	}

	if app.Spec.Resources == nil {
		app.Spec.Resources = &corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				"cpu":    resource.MustParse("2"),
				"memory": resource.MustParse("4Gi"),
			},
			Requests: corev1.ResourceList{
				"cpu":    resource.MustParse("2"),
				"memory": resource.MustParse("4Gi"),
			},
		}
	}

	return app
}
