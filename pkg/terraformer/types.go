// Copyright (c) 2018 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
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

package terraformer

import (
	"time"

	"github.com/sirupsen/logrus"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// terraformer is a struct containing configuration parameters for the Terraform script it acts on.
// * purpose is a one-word description depicting what the Terraformer does (e.g. 'infrastructure').
// * namespace is the namespace in which the Terraformer will act.
// * image is the Docker image name of the Terraformer image.
// * configName is the name of the ConfigMap containing the main Terraform file ('main.tf').
// * variablesName is the name of the Secret containing the Terraform variables ('terraform.tfvars').
// * stateName is the name of the ConfigMap containing the Terraform state ('terraform.tfstate').
// * podName is the name of the Pod which will validate the Terraform file.
// * jobName is the name of the Job which will execute the Terraform file.
// * variablesEnvironment is a map of environment variables which will be injected in the resulting
//   Terraform job/pod. These variables should contain Terraform variables (i.e., must be prefixed
//   with TF_VAR_).
// * configurationDefined indicates whether the required configuration ConfigMaps/Secrets have been
//   successfully defined.
type terraformer struct {
	logger       logrus.FieldLogger
	client       client.Client
	coreV1Client corev1client.CoreV1Interface

	purpose   string
	namespace string
	image     string

	configName           string
	variablesName        string
	stateName            string
	podName              string
	jobName              string
	variablesEnvironment map[string]string
	configurationDefined bool

	jobBackoffLimit       int32
	activeDeadlineSeconds int64

	deadlineCleaning time.Duration
	deadlinePod      time.Duration
	deadlineJob      time.Duration
}

const (
	numberOfConfigResources = 3

	// TerraformerConfigSuffix is the suffix used for the ConfigMap which stores the Terraform configuration and variables declaration.
	TerraformerConfigSuffix = ".tf-config"

	// TerraformerVariablesSuffix is the suffix used for the Secret which stores the Terraform variables definition.
	TerraformerVariablesSuffix = ".tf-vars"

	// TerraformerStateSuffix is the suffix used for the ConfigMap which stores the Terraform state.
	TerraformerStateSuffix = ".tf-state"

	// TerraformerPodSuffix is the suffix used for the name of the Pod which validates the Terraform configuration.
	TerraformerPodSuffix = ".tf-pod"

	// TerraformerJobSuffix is the suffix used for the name of the Job which executes the Terraform configuration.
	TerraformerJobSuffix = ".tf-job"
)

// Terraformer is the Terraformer interface.
type Terraformer interface {
	SetVariablesEnvironment(tfVarsEnvironment map[string]string) Terraformer
	SetJobBackoffLimit(int32) Terraformer
	SetActiveDeadlineSeconds(int64) Terraformer
	SetDeadlineCleaning(time.Duration) Terraformer
	SetDeadlinePod(time.Duration) Terraformer
	SetDeadlineJob(time.Duration) Terraformer
	InitializeWith(initializer Initializer) Terraformer
	Apply() error
	Destroy() error
	GetStateOutputVariables(variables ...string) (map[string]string, error)
	ConfigExists() (bool, error)
}

// Initializer can initialize a Terraformer.
type Initializer interface {
	Initialize(config *InitializerConfig) error
}

// Factory is a factory that can produce Terraformer and Initializer.
type Factory interface {
	NewForConfig(logger logrus.FieldLogger, config *rest.Config, purpose, namespace, name, image string) (Terraformer, error)
	New(logger logrus.FieldLogger, client client.Client, coreV1Client corev1client.CoreV1Interface, purpose, namespace, name, image string) Terraformer
	DefaultInitializer(c client.Client, main, variables string, tfVars []byte) Initializer
}
