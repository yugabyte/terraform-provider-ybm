/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package managed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/hashicorp/terraform-plugin-log/tflog"
	retry "github.com/sethvargo/go-retry"
	openapiclient "github.com/yugabyte/yugabytedb-managed-go-client-internal"
)

const (
	ampIntentCreate = "create"
	ampIntentUpdate = "update"
	ampIntentDelete = "delete"
)

// ampScope is where an AMP resource tree lives: the account's AMP Managed storage pool, or a BYOC
// environment's desired state. Both are written one resource at a time with the AMP Managed
// create/update/delete semantics, which the provider emulates for BYOC environments until YCP
// offers the same patch for them (CLOUDGA-37492).
type ampScope struct {
	client        *openapiclient.APIClient
	accountId     string
	environmentId string // empty for AMP Managed
}

func newAmpScope(client *openapiclient.APIClient, accountId string, environmentId string) ampScope {
	return ampScope{client: client, accountId: accountId, environmentId: environmentId}
}

func (s ampScope) isByoc() bool {
	return s.environmentId != ""
}

// pool is the storage pool every resource name starts with: the account ID, as YBM requires for
// AMP Managed and the provider uses for BYOC environments too.
func (s ampScope) pool() string {
	return s.accountId
}

// snapshot returns the desired and observed state of the scope, one entry per environment.
func (s ampScope) snapshot(ctx context.Context) ([]ampEnvironmentResources, error) {
	if !s.isByoc() {
		return listAmpManagedResources(ctx, s.client, s.accountId)
	}
	desired, err := getByocDesired(ctx, s.client, s.accountId, s.environmentId)
	if err != nil {
		return nil, err
	}
	env, err := getByocEnvironment(ctx, s.client, s.accountId, s.environmentId)
	if err != nil {
		return nil, err
	}
	return []ampEnvironmentResources{{EnvironmentID: env.ID, Region: env.Region, Desired: desired, Observed: env.Resources}}, nil
}

// apply performs one intent on the given resources. region is only used for AMP Managed.
func (s ampScope) apply(ctx context.Context, region string, intent string, items []ampPatchItem) error {
	if !s.isByoc() {
		method := map[string]string{
			ampIntentCreate: http.MethodPost,
			ampIntentUpdate: http.MethodPut,
			ampIntentDelete: http.MethodPatch,
		}[intent]
		patch := ampManagedPatch{Regions: []ampRegionPatch{{Region: region, Resources: items}}}
		_, err := ycpDo(ctx, s.client, method, s.accountId, "/clusters", nil, patch)
		return err
	}
	return updateByocDesired(ctx, s.client, s.accountId, s.environmentId, func(desired []ampResource) ([]ampResource, error) {
		return applyAmpIntent(desired, s.pool(), intent, items)
	})
}

// clusterRegion returns the region an AMP Managed cluster was created in; BYOC clusters have none.
func (s ampScope) clusterRegion(ctx context.Context, clusterName string) (string, error) {
	if s.isByoc() {
		return "", nil
	}
	snap, err := s.snapshot(ctx)
	if err != nil {
		return "", err
	}
	if env, ok := ampEnvDeclaring(snap, ampKindCluster, ampClusterName(s.pool(), clusterName)); ok {
		return env.Region, nil
	}
	return "", &ycpError{StatusCode: http.StatusNotFound, Message: fmt.Sprintf("cluster %s does not exist", clusterName)}
}

// ampEnvDeclaring returns the environment whose desired state declares the resource.
func ampEnvDeclaring(snap []ampEnvironmentResources, kind, name string) (ampEnvironmentResources, bool) {
	for _, env := range snap {
		if _, ok := findAmpDesired(env.Desired, kind, name); ok {
			return env, true
		}
	}
	return ampEnvironmentResources{}, false
}

// applyAmpIntent applies an intent to a desired state the way YCP applies an AMP Managed patch: a
// create of an existing resource and an update of a missing one fail, an update merges the spec's
// top-level keys, and a delete removes the resource with everything under it.
func applyAmpIntent(desired []ampResource, pool string, intent string, items []ampPatchItem) ([]ampResource, error) {
	for _, item := range items {
		idx := -1
		for i, r := range desired {
			if r.Kind == item.Kind && r.Name == item.Name {
				idx = i
				break
			}
		}
		switch intent {
		case ampIntentCreate:
			if idx >= 0 {
				return nil, &ycpError{StatusCode: http.StatusConflict, Message: fmt.Sprintf("%s %s already exists", item.Kind, item.Name)}
			}
			if _, ok := findAmpDesired(desired, ampKindStoragePool, pool); !ok && strings.HasPrefix(item.Name, pool+"/") {
				desired = append(desired, ampResource{Kind: ampKindStoragePool, Name: pool, Spec: mustMarshal(map[string]string{"owner_user_id": pool})})
			}
			desired = append(desired, ampResource{Kind: item.Kind, Name: item.Name, Spec: mustMarshal(item.Spec)})
		case ampIntentUpdate:
			if idx < 0 {
				return nil, &ycpError{StatusCode: http.StatusNotFound, Message: fmt.Sprintf("%s %s does not exist", item.Kind, item.Name)}
			}
			merged, err := mergeAmpSpec(desired[idx].Spec, item.Spec)
			if err != nil {
				return nil, err
			}
			desired[idx].Spec = merged
		case ampIntentDelete:
			desired = withoutAmpSubtree(desired, pool, item.Name)
		default:
			return nil, fmt.Errorf("unknown intent %q", intent)
		}
	}
	return desired, nil
}

// mergeAmpSpec overwrites the stored spec's top-level keys with the patch's; nested maps are
// replaced whole, as YCP does.
func mergeAmpSpec(stored json.RawMessage, patch interface{}) (json.RawMessage, error) {
	merged := map[string]json.RawMessage{}
	if len(stored) > 0 {
		if err := json.Unmarshal(stored, &merged); err != nil {
			return nil, fmt.Errorf("unable to parse the stored spec: %v", err)
		}
	}
	var changes map[string]json.RawMessage
	if err := json.Unmarshal(mustMarshal(patch), &changes); err != nil {
		return nil, fmt.Errorf("unable to parse the spec update: %v", err)
	}
	for k, v := range changes {
		merged[k] = v
	}
	return mustMarshal(merged), nil
}

// withoutAmpSubtree returns desired without the named resource and its descendants, and without
// the pool once nothing is left in it.
func withoutAmpSubtree(desired []ampResource, pool string, name string) []ampResource {
	kept := make([]ampResource, 0, len(desired))
	poolUsed := false
	for _, r := range desired {
		if r.Name == name || strings.HasPrefix(r.Name, name+"/") {
			continue
		}
		if strings.HasPrefix(r.Name, pool+"/") {
			poolUsed = true
		}
		kept = append(kept, r)
	}
	if poolUsed {
		return kept
	}
	out := kept[:0]
	for _, r := range kept {
		if !(r.Kind == ampKindStoragePool && r.Name == pool) {
			out = append(out, r)
		}
	}
	return out
}

type ampTarget struct {
	Kind string
	Name string
}

// ampResourceProgress reports whether a resource has converged on its desired generation, or the
// error that stops it ever converging.
func ampResourceProgress(desired []ampResource, observed []ampObserved, kind, name string) (bool, error) {
	o, ok := findAmpObserved(observed, kind, name)
	if ok && ampTerminalStates[o.State] {
		return false, fmt.Errorf("%s %s is %s: %s", kind, name, o.State, o.Error)
	}
	if !ok || o.State != ampStateReady {
		return false, nil
	}
	if kind == ampKindEndpoint && o.Attrs["address"] == "" {
		return false, nil
	}
	// generation is omitted while 0, and applied_generation only appears once the controller has
	// echoed one back, so the comparison applies only when both are present.
	if d, ok := findAmpDesired(desired, kind, name); ok && d.Generation > 0 {
		if applied, ok := o.Attrs["applied_generation"]; ok && applied != fmt.Sprint(d.Generation) {
			return false, nil
		}
	}
	return true, nil
}

// waitForAmp polls until every target has converged.
func waitForAmp(ctx context.Context, scope ampScope, targets []ampTarget) error {
	var terminal error
	retryPolicy := retry.WithMaxDuration(ampOperationTimeout, retry.NewConstant(ampPollInterval))
	err := retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		snap, err := scope.snapshot(ctx)
		if err != nil {
			return retry.RetryableError(err)
		}
		for _, t := range targets {
			env, ok := ampEnvDeclaring(snap, t.Kind, t.Name)
			if !ok {
				terminal = fmt.Errorf("%s %s is not declared", t.Kind, t.Name)
				return terminal
			}
			done, err := ampResourceProgress(env.Desired, env.Observed, t.Kind, t.Name)
			if err != nil {
				terminal = err
				return err
			}
			if !done {
				tflog.Debug(ctx, fmt.Sprintf("%s %s has not converged yet", t.Kind, t.Name))
				return retry.RetryableError(errors.New("not ready yet"))
			}
		}
		return nil
	})
	if terminal != nil {
		return terminal
	}
	if err != nil {
		hint := ""
		if scope.isByoc() {
			hint = "; check that the environment's agent is running"
		}
		return fmt.Errorf("timed out after %v waiting for the change to converge%s", ampOperationTimeout, hint)
	}
	return nil
}

// waitForAmpGone polls until no environment reports the resource any more.
func waitForAmpGone(ctx context.Context, scope ampScope, kind, name string) error {
	retryPolicy := retry.WithMaxDuration(ampOperationTimeout, retry.NewConstant(ampPollInterval))
	return retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		snap, err := scope.snapshot(ctx)
		if isYcpNotFound(err) {
			return nil
		}
		if err != nil {
			return retry.RetryableError(err)
		}
		for _, env := range snap {
			if _, ok := findAmpObserved(env.Observed, kind, name); ok {
				return retry.RetryableError(fmt.Errorf("%s %s is still being deleted", kind, name))
			}
		}
		return nil
	})
}
