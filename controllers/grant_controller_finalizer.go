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

	mariadbv1alpha1 "github.com/mariadb-operator/mariadb-operator/api/v1alpha1"
	mariadbclient "github.com/mariadb-operator/mariadb-operator/pkg/client"
	"github.com/mariadb-operator/mariadb-operator/pkg/controller/sql"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	grantFinalizerName = "grant.mariadb.mmontes.io/finalizer"
)

type wrappedGrantFinalizer struct {
	client.Client
	grant *mariadbv1alpha1.Grant
}

func newWrappedGrantFinalizer(client client.Client, grant *mariadbv1alpha1.Grant) sql.WrappedFinalizer {
	return &wrappedGrantFinalizer{
		Client: client,
		grant:  grant,
	}
}

func (wf *wrappedGrantFinalizer) AddFinalizer(ctx context.Context) error {
	if wf.ContainsFinalizer() {
		return nil
	}
	return wf.patch(ctx, wf.grant, func(gmd *mariadbv1alpha1.Grant) {
		controllerutil.AddFinalizer(wf.grant, grantFinalizerName)
	})
}

func (wf *wrappedGrantFinalizer) RemoveFinalizer(ctx context.Context) error {
	if !wf.ContainsFinalizer() {
		return nil
	}
	return wf.patch(ctx, wf.grant, func(gmd *mariadbv1alpha1.Grant) {
		controllerutil.RemoveFinalizer(wf.grant, grantFinalizerName)
	})
}

func (wf *wrappedGrantFinalizer) ContainsFinalizer() bool {
	return controllerutil.ContainsFinalizer(wf.grant, grantFinalizerName)
}

func (wf *wrappedGrantFinalizer) Reconcile(ctx context.Context, mdbClient *mariadbclient.Client) error {
	err := wait.PollUntilContextTimeout(ctx, 1*time.Second, 10*time.Second, true, func(ctx context.Context) (bool, error) {
		var user mariadbv1alpha1.User
		if err := wf.Get(ctx, userKey(wf.grant), &user); err != nil {
			if apierrors.IsNotFound(err) {
				return true, nil
			}
			return true, err
		}
		return false, nil
	})
	// User does not exist
	if err == nil {
		return nil
	}
	if err != nil && !wait.Interrupted(err) {
		return fmt.Errorf("error checking if user exists in MariaDB: %v", err)
	}

	var opts []mariadbclient.GrantOption
	if wf.grant.Spec.GrantOption {
		opts = append(opts, mariadbclient.WithGrantOption())
	}
	if err := mdbClient.Revoke(
		ctx,
		wf.grant.Spec.Privileges,
		wf.grant.Spec.Database,
		wf.grant.Spec.Table,
		wf.grant.AccountName(),
		opts...,
	); err != nil {
		return fmt.Errorf("error revoking grant in MariaDB: %v", err)
	}
	return nil
}

func (wf *wrappedGrantFinalizer) patch(ctx context.Context, grant *mariadbv1alpha1.Grant,
	patchFn func(*mariadbv1alpha1.Grant)) error {
	patch := client.MergeFrom(grant.DeepCopy())
	patchFn(grant)

	if err := wf.Client.Patch(ctx, grant, patch); err != nil {
		return fmt.Errorf("error patching Grant: %v", err)
	}
	return nil
}
