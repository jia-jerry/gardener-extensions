// Copyright (c) 2019 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
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

package infrastructure

import (
	"context"

	"github.com/gardener/gardener-extensions/controllers/provider-alicloud/pkg/alicloud"
	apisalicloud "github.com/gardener/gardener-extensions/controllers/provider-alicloud/pkg/apis/alicloud"
	apisalicloudhelper "github.com/gardener/gardener-extensions/controllers/provider-alicloud/pkg/apis/alicloud/helper"
	alicloudv1alpha1 "github.com/gardener/gardener-extensions/controllers/provider-alicloud/pkg/apis/alicloud/v1alpha1"
	confighelper "github.com/gardener/gardener-extensions/controllers/provider-alicloud/pkg/apis/config/helper"
	extensioncontroller "github.com/gardener/gardener-extensions/pkg/controller"
	"github.com/gardener/gardener-extensions/pkg/util"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/retry"
)

func appendMachineImage(machineImages []apisalicloud.MachineImage, machineImage apisalicloud.MachineImage) []apisalicloud.MachineImage {
	if _, err := apisalicloudhelper.FindMachineImage(machineImages, machineImage.Name, machineImage.Version); err != nil {
		return append(machineImages, machineImage)
	}
	return machineImages
}

func (a *actuator) generateMachineConfig(ctx context.Context, infra *extensionsv1alpha1.Infrastructure, cluster *extensioncontroller.Cluster) error {
	var (
		machineImages []apisalicloud.MachineImage
	)
	for _, worker := range cluster.Shoot.Spec.Provider.Workers {
		imageID, err := confighelper.FindImageForRegion(a.machineImageMapping, worker.Machine.Image.Name, worker.Machine.Image.Version, infra.Spec.Region)
		if err != nil {
			if providerStatus := infra.Status.ProviderStatus; providerStatus != nil {
				infrastructureStatus := &apisalicloud.InfrastructureStatus{}
				if _, _, err := a.decoder.Decode(providerStatus.Raw, nil, infrastructureStatus); err != nil {
					return errors.Wrapf(err, "could not decode infrastructure status of infrastructure '%s'", util.ObjectName(infra))
				}

				machineImage, err := apisalicloudhelper.FindMachineImage(infrastructureStatus.MachineImages, worker.Machine.Image.Name, worker.Machine.Image.Version)
				if err != nil {
					return err
				}
				imageID = machineImage.ID
			} else {
				return err
			}
		}
		machineImages = appendMachineImage(machineImages, apisalicloud.MachineImage{
			Name:    worker.Machine.Image.Name,
			Version: worker.Machine.Image.Version,
			ID:      imageID,
		})
	}
	a.machineImages = machineImages

	return nil
}

// shareCustomizedImages checks whether Shoot's Alicloud account has permissions to use the customized images. If it can't
// access them, these images will be shared with it from Seed's Alicloud account.
func (a *actuator) shareCustomizedImages(ctx context.Context, infra *extensionsv1alpha1.Infrastructure, cluster *extensioncontroller.Cluster) error {
	if a.machineImages == nil {
		if err := a.generateMachineConfig(ctx, infra, cluster); err != nil {
			return err
		}
	}

	if a.alicloudECSClient == nil {
		a.logger.Info("Creating Alicloud ECS client for Seed", "infrastructure", infra.Name)
		seedCloudProviderCredentials, err := alicloud.ReadCredentialsFromSecretRef(ctx, a.client, a.machineImageOwnerSecretRef)
		if err != nil {
			return err
		}
		a.alicloudECSClient, err = a.newClientFactory.NewECSClient(ctx, infra.Spec.Region, seedCloudProviderCredentials.AccessKeyID, seedCloudProviderCredentials.AccessKeySecret)
		if err != nil {
			return err
		}
	}

	_, shootCloudProviderCredentials, err := a.getConfigAndCredentialsForInfra(ctx, infra)
	if err != nil {
		return err
	}
	a.logger.Info("Creating Alicloud ECS client for Shoot", "infrastructure", infra.Name)
	shootAlicloudECSClient, err := a.newClientFactory.NewECSClient(ctx, infra.Spec.Region, shootCloudProviderCredentials.AccessKeyID, shootCloudProviderCredentials.AccessKeySecret)
	if err != nil {
		return err
	}
	a.logger.Info("Creating Alicloud STS client for Shoot", "infrastructure", infra.Name)
	shootAlicloudSTSClient, err := a.newClientFactory.NewSTSClient(ctx, infra.Spec.Region, shootCloudProviderCredentials.AccessKeyID, shootCloudProviderCredentials.AccessKeySecret)
	if err != nil {
		return err
	}

	shootCloudProviderAccountID, err := shootAlicloudSTSClient.GetAccountIDFromCallerIdentity(ctx)
	if err != nil {
		return err
	}

	a.logger.Info("Sharing customized image with Shoot's Alicloud account from Seed", "infrastructure", infra.Name)
	for _, machineImage := range a.machineImages {
		exists, err := shootAlicloudECSClient.CheckIfImageExists(ctx, machineImage.ID)
		if err != nil {
			return err
		}
		if exists {
			continue
		}
		err = a.alicloudECSClient.ShareImageToAccount(ctx, machineImage.ID, shootCloudProviderAccountID)
		if err != nil {
			return err
		}
	}

	return nil
}

// getMachineImages returns the used machine images for the `Infrastructure` resource.
func (a *actuator) getMachineImages(ctx context.Context, infra *extensionsv1alpha1.Infrastructure, cluster *extensioncontroller.Cluster) (runtime.Object, error) {
	if a.machineImages == nil {
		if err := a.generateMachineConfig(ctx, infra, cluster); err != nil {
			return nil, err
		}
	}

	var (
		infrastructureStatus = &apisalicloud.InfrastructureStatus{
			TypeMeta: metav1.TypeMeta{
				APIVersion: apisalicloud.SchemeGroupVersion.String(),
				Kind:       "InfrastructureStatus",
			},
			MachineImages: a.machineImages,
		}

		infrastructureStatusV1alpha1 = &alicloudv1alpha1.InfrastructureStatus{
			TypeMeta: metav1.TypeMeta{
				APIVersion: alicloudv1alpha1.SchemeGroupVersion.String(),
				Kind:       "InfrastructureStatus",
			},
		}
	)

	if err := a.scheme.Convert(infrastructureStatus, infrastructureStatusV1alpha1, nil); err != nil {
		return nil, err
	}

	return infrastructureStatusV1alpha1, nil
}

func (a *actuator) updateInfrastructureStatusMachineImages(ctx context.Context, infra *extensionsv1alpha1.Infrastructure, machineImages runtime.Object) error {
	if machineImages == nil {
		return nil
	}

	return extensioncontroller.TryUpdateStatus(ctx, retry.DefaultBackoff, a.client, infra, func() error {
		infra.Status.ProviderStatus = &runtime.RawExtension{Object: machineImages}
		return nil
	})
}
