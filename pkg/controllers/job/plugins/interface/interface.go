/*
Copyright 2019 The Volcano Authors.

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

package pluginsinterface

import (
	"k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"

	vkv1 "volcano.sh/volcano/pkg/apis/batch/v1alpha1"
)

// PluginClientset clientset
type PluginClientset struct {
	KubeClients kubernetes.Interface
}

// PluginInterface interface
type PluginInterface interface {
	// The unique name of Plugin.
	Name() string

	// for all pod when createJobPod
	OnPodCreate(pod *v1.Pod, job *vkv1.Job) error

	// do once when syncJob
	OnJobAdd(job *vkv1.Job) error

	// do once when killJob
	OnJobDelete(job *vkv1.Job) error
}
