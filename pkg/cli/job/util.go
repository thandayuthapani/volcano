/*
Copyright 2018 The Vulcan Authors.

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

package job

import (
	"fmt"
	"os"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	vkbatchv1 "volcano.sh/volcano/pkg/apis/batch/v1alpha1"
	vkbusv1 "volcano.sh/volcano/pkg/apis/bus/v1alpha1"
	"volcano.sh/volcano/pkg/apis/helpers"
	"volcano.sh/volcano/pkg/client/clientset/versioned"
)

func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	return os.Getenv("USERPROFILE") // windows
}

func buildConfig(master, kubeconfig string) (*rest.Config, error) {
	return clientcmd.BuildConfigFromFlags(master, kubeconfig)
}

// populateResourceListV1 takes strings of form <resourceName1>=<value1>,<resourceName1>=<value2>
// and returns ResourceList.
func populateResourceListV1(spec string) (v1.ResourceList, error) {
	// empty input gets a nil response to preserve generator test expected behaviors
	if spec == "" {
		return nil, nil
	}

	result := v1.ResourceList{}
	resourceStatements := strings.Split(spec, ",")
	for _, resourceStatement := range resourceStatements {
		parts := strings.Split(resourceStatement, "=")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid argument syntax %v, expected <resource>=<value>", resourceStatement)
		}
		resourceName := v1.ResourceName(parts[0])
		resourceQuantity, err := resource.ParseQuantity(parts[1])
		if err != nil {
			return nil, err
		}
		result[resourceName] = resourceQuantity
	}
	return result, nil
}

func createJobCommand(config *rest.Config, ns, name string, action vkbatchv1.Action) error {
	jobClient := versioned.NewForConfigOrDie(config)
	job, err := jobClient.BatchV1alpha1().Jobs(ns).Get(name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	ctrlRef := metav1.NewControllerRef(job, helpers.JobKind)
	cmd := &vkbusv1.Command{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: fmt.Sprintf("%s-%s-",
				job.Name, strings.ToLower(string(action))),
			Namespace: job.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*ctrlRef,
			},
		},
		TargetObject: ctrlRef,
		Action:       string(action),
	}

	if _, err := jobClient.BusV1alpha1().Commands(ns).Create(cmd); err != nil {
		return err
	}

	return nil
}
