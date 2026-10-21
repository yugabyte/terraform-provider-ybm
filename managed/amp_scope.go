/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package managed

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
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

// ampScope is where an AMP resource tree lives: the account's AMP Managed storage pool, or a
// storage pool in a BYOC environment's desired state. Both are written one resource at a time with
// the AMP Managed create/update/delete semantics, which the provider emulates for BYOC environments
// until YCP offers the same patch for them (CLOUDGA-37492).
type ampScope struct {
	client        *openapiclient.APIClient
	accountId     string
	environmentId string // empty for AMP Managed
	storagePool   string
}

// newAmpScope returns the scope of a storage pool. YBM requires the account ID as the AMP Managed
// pool, so storagePool only applies to a BYOC environment, where it is required.
func newAmpScope(client *openapiclient.APIClient, accountId string, environmentId string, storagePool string) ampScope {
	if environmentId == "" {
		storagePool = accountId
	}
	return ampScope{client: client, accountId: accountId, environmentId: environmentId, storagePool: storagePool}
}

func (s ampScope) isByoc() bool {
	return s.environmentId != ""
}

// pool is the storage pool every resource name starts with.
func (s ampScope) pool() string {
	return s.storagePool
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
		if intent == ampIntentCreate {
			if _, ok := findAmpDesired(desired, ampKindStoragePool, s.pool()); !ok {
				return nil, &ycpError{StatusCode: http.StatusNotFound, Message: fmt.Sprintf(
					"storage pool %s does not exist in environment %s; create it with ybm_byoc_storage_pool", s.pool(), s.environmentId)}
			}
		}
		return applyAmpIntent(desired, intent, items)
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
func applyAmpIntent(desired []ampResource, intent string, items []ampPatchItem) ([]ampResource, error) {
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
			desired = withoutAmpSubtree(desired, item.Name)
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

// withoutAmpSubtree returns desired without the named resource and its descendants.
func withoutAmpSubtree(desired []ampResource, name string) []ampResource {
	kept := make([]ampResource, 0, len(desired))
	for _, r := range desired {
		if r.Name == name || strings.HasPrefix(r.Name, name+"/") {
			continue
		}
		kept = append(kept, r)
	}
	return kept
}

// ampForks returns the paths of the branches forked from the given branch, directly or through
// other forks. A fork names its parent by ID, so its path is not under its parent's.
func ampForks(env ampEnvironmentResources, clusterPath, branchPath string) []string {
	var forks []string
	parents := []string{branchPath}
	for len(parents) > 0 {
		parent := parents[0]
		parents = parents[1:]
		o, ok := findAmpObserved(env.Observed, ampKindBranch, parent)
		if !ok || o.Attrs["id"] == "" {
			continue
		}
		for _, r := range env.Desired {
			if r.Kind != ampKindBranch || !strings.HasPrefix(r.Name, clusterPath+"/") {
				continue
			}
			var spec ampBranchSpec
			if json.Unmarshal(r.Spec, &spec) == nil && spec.ParentBranchID == o.Attrs["id"] {
				forks = append(forks, r.Name)
				parents = append(parents, r.Name)
			}
		}
	}
	return forks
}

// ampDependents returns the declared resources that deleting the given roots would delete with
// them, other than those in keep.
func ampDependents(env ampEnvironmentResources, roots []string, keep map[string]bool) []string {
	var dependents []string
	for _, r := range env.Desired {
		if keep[r.Kind+" "+r.Name] {
			continue
		}
		for _, root := range roots {
			if r.Name == root || strings.HasPrefix(r.Name, root+"/") {
				dependents = append(dependents, r.Kind+" "+r.Name)
				break
			}
		}
	}
	sort.Strings(dependents)
	return dependents
}

type ampTarget struct {
	Kind string
	Name string
}

// ampResourceProgress reports whether a resource has converged on its desired generation, what it
// is waiting on if not, or the error that stops it ever converging.
func ampResourceProgress(desired []ampResource, observed []ampObserved, kind, name string) (bool, string, error) {
	o, ok := findAmpObserved(observed, kind, name)
	if ok && ampTerminalStates[o.State] {
		return false, "", fmt.Errorf("%s %s is %s: %s", kind, name, o.State, o.Error)
	}
	if !ok {
		return false, fmt.Sprintf("%s %s has not been reported yet", kind, name), nil
	}
	if o.State != ampStateReady {
		return false, fmt.Sprintf("%s %s is %s", kind, name, o.State), nil
	}
	if kind == ampKindEndpoint && o.Attrs["address"] == "" {
		return false, fmt.Sprintf("%s %s has no address yet", kind, name), nil
	}
	// generation is omitted while 0, and applied_generation only appears once the controller has
	// echoed one back, so the comparison applies only when both are present.
	if d, ok := findAmpDesired(desired, kind, name); ok && d.Generation > 0 {
		if applied, ok := o.Attrs["applied_generation"]; ok && applied != fmt.Sprint(d.Generation) {
			return false, fmt.Sprintf("%s %s has applied generation %s of %d", kind, name, applied, d.Generation), nil
		}
	}
	return true, "", nil
}

// ampPoll calls check until it reports nothing left to wait on. A transient API error is retried
// like a wait; any other error stops the poll at once.
func ampPoll(ctx context.Context, what string, check func(context.Context) (waitingOn string, err error)) error {
	var terminal error
	last := "no response yet"
	retryPolicy := retry.WithMaxDuration(ampOperationTimeout, retry.NewConstant(ampPollInterval))
	err := retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		waitingOn, err := check(ctx)
		switch {
		case err != nil && !isYcpTransient(err):
			terminal = err
			return err
		case err != nil:
			last = err.Error()
			return retry.RetryableError(err)
		case waitingOn != "":
			last = waitingOn
			tflog.Debug(ctx, "Waiting for "+what+": "+waitingOn)
			return retry.RetryableError(fmt.Errorf("%s", waitingOn))
		}
		return nil
	})
	switch {
	case terminal != nil:
		return terminal
	case err == nil:
		return nil
	case ctx.Err() != nil:
		return fmt.Errorf("stopped waiting for %s: %v", what, ctx.Err())
	}
	return fmt.Errorf("timed out after %v waiting for %s; last status: %s", ampOperationTimeout, what, last)
}

// waitForAmp polls until every target has converged.
func waitForAmp(ctx context.Context, scope ampScope, targets []ampTarget) error {
	what := "the change to converge"
	if scope.isByoc() {
		what += " (check that the environment's agent is running)"
	}
	return ampPoll(ctx, what, func(ctx context.Context) (string, error) {
		snap, err := scope.snapshot(ctx)
		if err != nil {
			return "", err
		}
		for _, t := range targets {
			env, ok := ampEnvDeclaring(snap, t.Kind, t.Name)
			if !ok {
				return "", fmt.Errorf("%s %s is not declared", t.Kind, t.Name)
			}
			done, waitingOn, err := ampResourceProgress(env.Desired, env.Observed, t.Kind, t.Name)
			if err != nil || !done {
				return waitingOn, err
			}
		}
		return "", nil
	})
}

// waitForAmpGone polls until no environment reports any of the resources any more.
func waitForAmpGone(ctx context.Context, scope ampScope, targets []ampTarget) error {
	return ampPoll(ctx, "the deletion to complete", func(ctx context.Context) (string, error) {
		snap, err := scope.snapshot(ctx)
		if isYcpNotFound(err) {
			return "", nil
		}
		if err != nil {
			return "", err
		}
		for _, env := range snap {
			for _, t := range targets {
				if _, ok := findAmpObserved(env.Observed, t.Kind, t.Name); ok {
					return fmt.Sprintf("%s %s is still being deleted", t.Kind, t.Name), nil
				}
			}
		}
		return "", nil
	})
}
