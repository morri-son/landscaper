// Copyright 2020 Copyright (c) 2020 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file.
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

package installations

import (
	"context"
	"reflect"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/gardener/landscaper/pkg/apis/config"
	lsv1alpha1 "github.com/gardener/landscaper/pkg/apis/core/v1alpha1"
	lsv1alpha1helper "github.com/gardener/landscaper/pkg/apis/core/v1alpha1/helper"
	"github.com/gardener/landscaper/pkg/landscaper/blueprints"
	"github.com/gardener/landscaper/pkg/landscaper/installations"
	"github.com/gardener/landscaper/pkg/landscaper/operation"
	blueprintsregistry "github.com/gardener/landscaper/pkg/landscaper/registry/blueprints"
	"github.com/gardener/landscaper/pkg/landscaper/registry/blueprints/manager"
	componentsregistry "github.com/gardener/landscaper/pkg/landscaper/registry/components"
	"github.com/gardener/landscaper/pkg/utils/kubernetes"
	"github.com/gardener/landscaper/pkg/utils/oci/cache"
)

func NewActuator(log logr.Logger, regConfig *config.RegistriesConfiguration) (reconcile.Reconciler, error) {
	op := &operation.Operation{}
	_ = op.InjectLogger(log)

	var sharedCache cache.Cache
	if regConfig.Components.OCI != nil && regConfig.Components.OCI.Cache != nil {
		var err error
		sharedCache, err = cache.NewCache(log, cache.WithConfiguration(regConfig.Components.OCI.Cache))
		if err != nil {
			return nil, err
		}
	}
	componentRegistryMgr, err := componentsregistry.New(sharedCache)
	if err != nil {
		return nil, err
	}
	_ = op.InjectComponentsRegistry(componentRegistryMgr)

	// add once a local registry
	if regConfig.Components.Local != nil {
		localReg, err := componentsregistry.NewLocalClient(log, regConfig.Components.Local.ConfigPaths...)
		if err != nil {
			return nil, errors.Wrapf(err, "unable to create local components registry")
		}
		if err := componentRegistryMgr.Set(localReg); err != nil {
			return nil, errors.Wrap(err, "unable to add local components registry to manager")
		}
	}
	log.V(3).Info("setup components registry")

	if regConfig.Blueprints.OCI != nil && regConfig.Blueprints.OCI.Cache != nil {
		var err error
		sharedCache, err = cache.NewCache(log, cache.WithConfiguration(regConfig.Blueprints.OCI.Cache))
		if err != nil {
			return nil, err
		}
	}
	blueprintsRegistryMgr := manager.New(sharedCache)
	_ = op.InjectBlueprintsRegistry(blueprintsRegistryMgr)

	// add once a local registry
	if regConfig.Blueprints.Local != nil {
		localReg, err := blueprintsregistry.NewLocalRegistry(log, regConfig.Blueprints.Local.ConfigPaths...)
		if err != nil {
			return nil, errors.Wrapf(err, "unable to create local blueprint registry")
		}
		if err := blueprintsRegistryMgr.Set(blueprintsregistry.LocalAccessType, blueprintsregistry.LocalAccessCodec, localReg); err != nil {
			return nil, errors.Wrap(err, "unable to add local blueprint registry to manager")
		}
	}
	log.V(3).Info("setup blueprints registry")

	return &actuator{
		Interface:             op,
		registriesConfig:      regConfig,
		componentsRegistryMgr: componentRegistryMgr,
		blueprintRegistryMgr:  blueprintsRegistryMgr,
	}, nil
}

// NewTestActuator creates a new actuator that is only meant for testing.
func NewTestActuator(op operation.Interface, configuration *config.RegistriesConfiguration) *actuator {
	return &actuator{
		Interface:             op,
		registriesConfig:      configuration,
		componentsRegistryMgr: &componentsregistry.Manager{},
		blueprintRegistryMgr:  manager.New(nil),
	}
}

type actuator struct {
	operation.Interface
	registriesConfig      *config.RegistriesConfiguration
	blueprintRegistryMgr  manager.Interface
	componentsRegistryMgr *componentsregistry.Manager
}

func (a *actuator) Reconcile(req reconcile.Request) (reconcile.Result, error) {
	ctx := context.Background()
	defer ctx.Done()
	a.Log().Info("reconcile", "resource", req.NamespacedName)

	inst := &lsv1alpha1.Installation{}
	if err := a.Client().Get(ctx, req.NamespacedName, inst); err != nil {
		if apierrors.IsNotFound(err) {
			a.Log().V(5).Info(err.Error())
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	if inst.DeletionTimestamp.IsZero() && !kubernetes.HasFinalizer(inst, lsv1alpha1.LandscaperFinalizer) {
		controllerutil.AddFinalizer(inst, lsv1alpha1.LandscaperFinalizer)
		if err := a.Client().Update(ctx, inst); err != nil {
			return reconcile.Result{Requeue: true}, err
		}
		return reconcile.Result{}, nil
	}

	// remove the reconcile annotation if it exists
	if lsv1alpha1helper.HasOperation(inst.ObjectMeta, lsv1alpha1.ReconcileOperation) {
		delete(inst.Annotations, lsv1alpha1.OperationAnnotation)
		if err := a.Client().Update(ctx, inst); err != nil {
			return reconcile.Result{Requeue: true}, err
		}
		if err := a.reconcile(ctx, inst); err != nil {
			return reconcile.Result{}, err
		}
		return reconcile.Result{}, nil
	}

	if lsv1alpha1helper.HasOperation(inst.ObjectMeta, lsv1alpha1.ForceReconcileOperation) {
		if err := a.reconcile(ctx, inst); err != nil {
			return reconcile.Result{}, err
		}
		return reconcile.Result{}, nil
	}

	if lsv1alpha1helper.HasOperation(inst.ObjectMeta, lsv1alpha1.AbortOperation) {
		// todo: handle abort..
		a.Log().Info("do abort")
	}

	if lsv1alpha1helper.IsCompletedInstallationPhase(inst.Status.Phase) && inst.Status.ObservedGeneration == inst.Generation {
		return reconcile.Result{}, nil
	}

	if err := a.reconcile(ctx, inst); err != nil {
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
}

func (a *actuator) reconcile(ctx context.Context, inst *lsv1alpha1.Installation) error {
	old := inst.DeepCopy()

	instOp, err := a.initPrerequisites(ctx, inst)
	if err != nil {
		return err
	}
	internalInstallation := instOp.Inst

	if !inst.DeletionTimestamp.IsZero() {
		err := EnsureDeletion(ctx, instOp)
		if err != nil && !reflect.DeepEqual(inst.Status, old.Status) {
			if err2 := a.Client().Status().Update(ctx, inst); err2 != nil {
				return errors.Wrapf(err, "update error: %s", err2.Error())
			}
		}
		return err
	}

	if lsv1alpha1helper.HasOperation(inst.ObjectMeta, lsv1alpha1.ForceReconcileOperation) {
		// need to return and not continue with export validation
		return a.forceReconcile(ctx, instOp, internalInstallation)
	}

	err = a.Ensure(ctx, instOp, internalInstallation)
	if !reflect.DeepEqual(inst.Status, old.Status) {
		if err2 := a.Client().Status().Update(ctx, inst); err2 != nil {
			if err != nil {
				err2 = errors.Wrapf(err, "update error: %s", err.Error())
			}
			return err2
		}
	}
	return err
}

func (a *actuator) initPrerequisites(ctx context.Context, inst *lsv1alpha1.Installation) (*installations.Operation, error) {
	if err := a.setupRegistries(ctx, inst.Spec.RegistryPullSecrets); err != nil {
		return nil, err
	}

	intBlueprint, err := blueprints.Resolve(ctx, a.Interface, inst.Spec.Blueprint, nil)
	if err != nil {
		// todo: set to failed and set last Error
		return nil, err
	}

	internalInstallation, err := installations.New(inst, intBlueprint)
	if err != nil {
		return nil, errors.Wrap(err, "unable to create internal representation of installation")
	}

	instOp, err := installations.NewInstallationOperationFromOperation(ctx, a.Interface, internalInstallation)
	if err != nil {
		return nil, errors.Wrap(err, "unable to create installation operation")
	}
	return instOp, nil
}

func (a *actuator) forceReconcile(ctx context.Context, instOp *installations.Operation, inst *installations.Installation) error {
	inst.Info.Status.Phase = lsv1alpha1.ComponentPhasePending
	if err := a.StartNewReconcile(ctx, instOp, inst); err != nil {
		return err
	}

	delete(inst.Info.Annotations, lsv1alpha1.OperationAnnotation)
	if err := a.Client().Update(ctx, inst.Info); err != nil {
		return err
	}

	inst.Info.Status.ObservedGeneration = inst.Info.Generation
	inst.Info.Status.Phase = lsv1alpha1.ComponentPhaseProgressing
	return nil
}