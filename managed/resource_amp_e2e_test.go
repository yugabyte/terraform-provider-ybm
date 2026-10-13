/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package managed

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// These tests drive the AMP resources through the provider protocol, as Terraform does, against
// a stateful fake of the YBM /ycp routes.

func managedSpec(t *testing.T, h *tfHarness, region, kind, name string) map[string]interface{} {
	t.Helper()
	h.fake.mu.Lock()
	defer h.fake.mu.Unlock()
	r, ok := findAmpDesired(h.fake.managed[region], kind, name)
	if !ok {
		t.Fatalf("%s %s is not declared", kind, name)
	}
	var spec map[string]interface{}
	_ = json.Unmarshal(r.Spec, &spec)
	return spec
}

func managedDeclared(h *tfHarness, region, kind, name string) bool {
	h.fake.mu.Lock()
	defer h.fake.mu.Unlock()
	_, ok := findAmpDesired(h.fake.managed[region], kind, name)
	return ok
}

func byocSpec(t *testing.T, h *tfHarness, env, kind, name string) (map[string]interface{}, bool) {
	t.Helper()
	h.fake.mu.Lock()
	defer h.fake.mu.Unlock()
	r, ok := findAmpDesired(h.fake.byoc[env].desired, kind, name)
	var spec map[string]interface{}
	_ = json.Unmarshal(r.Spec, &spec)
	return spec, ok
}

func mustApply(t *testing.T, h *tfHarness, name string, prior tftypes.Value, config map[string]interface{}) tftypes.Value {
	t.Helper()
	state, diags := h.apply(name, prior, config)
	if hasErrors(diags) {
		t.Fatalf("apply %s: %s", name, diagText(diags))
	}
	return state
}

func requirePlanNoop(t *testing.T, h *tfHarness, name string, state tftypes.Value, config map[string]interface{}) {
	t.Helper()
	p := h.plan(name, state, config)
	if hasErrors(p.diags) {
		t.Fatalf("plan %s: %s", name, diagText(p.diags))
	}
	if p.requiresReplace || !p.planned.Equal(state) {
		diffs, _ := state.Diff(p.planned)
		t.Errorf("plan %s is not empty: %v", name, diffs)
	}
}

func requirePlanError(t *testing.T, h *tfHarness, name string, state tftypes.Value, config map[string]interface{}, want string) {
	t.Helper()
	if state.Type() == nil {
		state = tftypes.NewValue(h.resourceType(name), nil)
	}
	if p := h.plan(name, state, config); !strings.Contains(diagText(p.diags), want) {
		t.Errorf("plan %s: want an error containing %q, got %q", name, want, diagText(p.diags))
	}
}

func withAttrs(config map[string]interface{}, changes map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	for k, v := range config {
		out[k] = v
	}
	for k, v := range changes {
		out[k] = v
	}
	return out
}

func creds(password string) map[string]interface{} {
	return map[string]interface{}{"password": password}
}

func TestAmpClusterLifecycle(t *testing.T) {
	h := newTfHarness(t)
	pool := testAccountID
	config := map[string]interface{}{
		"cluster_name":         "orders",
		"region":               "us-east-1",
		"credentials":          creds("S3cretPassw0rd"),
		"branch_pg_settings":   map[string]string{"work_mem": "32MB"},
		"endpoint_pg_settings": map[string]string{"work_mem": "64MB"},
		"allowed_cidrs":        []string{},
	}
	state := mustApply(t, h, "ybm_amp_cluster", tftypes.Value{}, config)

	for attr, want := range map[string]string{
		"host": "primary.amp.example.com", "port": "5432", "state": "ready", "username": "cloud_admin",
		"database": "postgres", "memory": "16384Mi", "cpu": "2", "connectivity_mode": "cluster_ip",
		"environment_id": "managed-us-east-1", "cluster_id": fakeID(pool + "/orders"), "account_id": pool,
	} {
		if got := attrString(t, state, attr); got != want {
			t.Errorf("%s = %q, want %q", attr, got, want)
		}
	}
	// The two levels of settings go to the main branch and the read-write endpoint.
	if spec := managedSpec(t, h, "us-east-1", ampKindBranch, pool+"/orders/main"); fmt.Sprint(spec["pg_settings"]) != "map[work_mem:32MB]" {
		t.Errorf("main branch spec = %v", spec)
	}
	if spec := managedSpec(t, h, "us-east-1", ampKindEndpoint, ampEndpointName(pool, "orders")); fmt.Sprint(spec["pg_settings"]) != "map[work_mem:64MB]" {
		t.Errorf("endpoint spec = %v", spec)
	}
	requirePlanNoop(t, h, "ybm_amp_cluster", state, config)
	requirePlanNoop(t, h, "ybm_amp_cluster", h.read("ybm_amp_cluster", state), config)

	// Resize in place; memory follows cpu when it is not set.
	config["cpu"] = "4"
	config["endpoint_pg_settings"] = map[string]string{"work_mem": "128MB", "max_connections": "200"}
	if p := h.plan("ybm_amp_cluster", state, config); p.requiresReplace {
		t.Fatal("a resize forces a replacement")
	}
	state = mustApply(t, h, "ybm_amp_cluster", state, config)
	if attrString(t, state, "memory") != "32768Mi" {
		t.Errorf("memory = %s", attrString(t, state, "memory"))
	}
	spec := managedSpec(t, h, "us-east-1", ampKindEndpoint, ampEndpointName(pool, "orders"))
	if fmt.Sprint(spec["resources"]) != "map[cpu:4 memory:32768Mi]" || fmt.Sprint(spec["pg_settings"]) != "map[max_connections:200 work_mem:128MB]" {
		t.Errorf("endpoint spec = %v", spec)
	}
	if spec["password"] != "S3cretPassw0rd" || spec["role"] != "read_write" {
		t.Errorf("an update lost the create-time fields: %v", spec)
	}
	requirePlanNoop(t, h, "ybm_amp_cluster", state, config)

	// Removing a key drops it from the spec, so AMP resets the setting.
	config["endpoint_pg_settings"] = map[string]string{"max_connections": "200"}
	state = mustApply(t, h, "ybm_amp_cluster", state, config)
	if spec := managedSpec(t, h, "us-east-1", ampKindEndpoint, ampEndpointName(pool, "orders")); fmt.Sprint(spec["pg_settings"]) != "map[max_connections:200]" {
		t.Errorf("the removed key is still set: %v", spec["pg_settings"])
	}

	// Emptied maps clear every setting, and read back empty rather than null.
	config["endpoint_pg_settings"] = map[string]string{}
	config["branch_pg_settings"] = map[string]string{}
	state = mustApply(t, h, "ybm_amp_cluster", state, config)
	if spec := managedSpec(t, h, "us-east-1", ampKindEndpoint, ampEndpointName(pool, "orders")); fmt.Sprint(spec["pg_settings"]) != "map[]" {
		t.Errorf("endpoint pg_settings were not cleared: %v", spec["pg_settings"])
	}
	if spec := managedSpec(t, h, "us-east-1", ampKindBranch, pool+"/orders/main"); fmt.Sprint(spec["pg_settings"]) != "map[]" {
		t.Errorf("branch pg_settings were not cleared: %v", spec["pg_settings"])
	}
	requirePlanNoop(t, h, "ybm_amp_cluster", state, config)
	requirePlanNoop(t, h, "ybm_amp_cluster", h.read("ybm_amp_cluster", state), config)

	// Unsetting them clears them too.
	delete(config, "endpoint_pg_settings")
	delete(config, "branch_pg_settings")
	state = mustApply(t, h, "ybm_amp_cluster", state, config)
	requirePlanNoop(t, h, "ybm_amp_cluster", state, config)

	// The password cannot change, and a new PG version replaces the cluster.
	requirePlanError(t, h, "ybm_amp_cluster", state, withAttrs(config, map[string]interface{}{"credentials": creds("AnotherPassw0rd")}), "Password cannot be changed")
	if p := h.plan("ybm_amp_cluster", state, withAttrs(config, map[string]interface{}{"pg_version": 17})); !p.requiresReplace {
		t.Error("a PG version change does not force a replacement")
	}

	if diags := h.destroy("ybm_amp_cluster", state); hasErrors(diags) {
		t.Fatal(diagText(diags))
	}
	if managedDeclared(h, "us-east-1", ampKindCluster, pool+"/orders") || managedDeclared(h, "us-east-1", ampKindBranch, pool+"/orders/main") {
		t.Error("the cluster was not deleted")
	}
}

func TestAmpPlaceholdersAreRejectedAtPlanTime(t *testing.T) {
	h := newTfHarness(t)
	cluster := map[string]interface{}{"cluster_name": "orders", "region": "us-east-1", "credentials": creds("S3cretPassw0rd")}

	requirePlanError(t, h, "ybm_amp_cluster", tftypes.Value{}, withAttrs(cluster, map[string]interface{}{"suspend_timeout_seconds": 300}), "cannot suspend")
	requirePlanError(t, h, "ybm_amp_cluster", tftypes.Value{}, withAttrs(cluster, map[string]interface{}{
		"credentials": map[string]interface{}{"password_wo": "S3cretPassw0rd", "password_wo_version": 1},
	}), "Write-only passwords")
	requirePlanError(t, h, "ybm_amp_cluster", tftypes.Value{}, withAttrs(cluster, map[string]interface{}{
		"credentials": map[string]interface{}{"username": "app", "password": "S3cretPassw0rd"},
	}), "Only the cloud_admin user")
	requirePlanError(t, h, "ybm_amp_cluster", tftypes.Value{}, withAttrs(cluster, map[string]interface{}{"credentials": map[string]interface{}{}}),
		"requires credentials.password")
	requirePlanError(t, h, "ybm_amp_branch", tftypes.Value{}, map[string]interface{}{"cluster_name": "orders", "branch_name": "dev", "ttl": "24h"}, "cannot expire")
	requirePlanError(t, h, "ybm_amp_endpoint", tftypes.Value{}, map[string]interface{}{
		"cluster_name": "orders", "endpoint_name": "replica", "suspend_timeout_seconds": 300,
	}, "cannot suspend")
	requirePlanError(t, h, "ybm_amp_branch", tftypes.Value{}, map[string]interface{}{
		"environment_id": "acme", "cluster_name": "orders", "branch_name": "dev",
	}, "need its storage_pool")
	requirePlanError(t, h, "ybm_amp_endpoint", tftypes.Value{}, map[string]interface{}{
		"storage_pool": "payments", "cluster_name": "orders", "endpoint_name": "replica",
	}, "only with environment_id")

	// Naming the default user explicitly is fine.
	config := withAttrs(cluster, map[string]interface{}{
		"credentials": map[string]interface{}{"username": "cloud_admin", "password": "S3cretPassw0rd"},
	})
	state := mustApply(t, h, "ybm_amp_cluster", tftypes.Value{}, config)
	requirePlanNoop(t, h, "ybm_amp_cluster", h.read("ybm_amp_cluster", state), config)
}

func TestAmpBranchesAndEndpoints(t *testing.T) {
	h := newTfHarness(t)
	pool := testAccountID
	clusterConfig := map[string]interface{}{"cluster_name": "orders", "region": "us-east-1", "credentials": creds("S3cretPassw0rd")}
	cluster := mustApply(t, h, "ybm_amp_cluster", tftypes.Value{}, clusterConfig)

	branchConfig := map[string]interface{}{
		"cluster_name": "orders",
		"branch_name":  "dev",
		"pg_settings":  map[string]string{"work_mem": "64MB"},
		"extensions":   []string{"vector"},
	}
	branch := mustApply(t, h, "ybm_amp_branch", tftypes.Value{}, branchConfig)
	if attrString(t, branch, "branch_id") != fakeID(pool+"/orders/dev") || attrString(t, branch, "parent_branch") != "main" {
		t.Errorf("branch = %v", branch)
	}
	spec := managedSpec(t, h, "us-east-1", ampKindBranch, pool+"/orders/dev")
	if fmt.Sprint(spec["extensions"]) != "[vector]" || fmt.Sprint(spec["pg_settings"]) != "map[work_mem:64MB]" {
		t.Errorf("branch spec = %v", spec)
	}
	// AMP refuses extensions on a new branch, so they must have been sent as a separate update.
	var creates, updates int
	for i, req := range h.fake.requests {
		if body := h.fake.bodies[i]; body != nil && strings.Contains(fmt.Sprint(body), "orders/dev") {
			switch {
			case strings.HasPrefix(req, "POST"):
				creates++
				if strings.Contains(fmt.Sprint(body), "vector") {
					t.Error("the branch was created with extensions")
				}
			case strings.HasPrefix(req, "PUT"):
				updates++
			}
		}
	}
	if creates != 1 || updates != 1 {
		t.Errorf("creates = %d, updates = %d", creates, updates)
	}
	requirePlanNoop(t, h, "ybm_amp_branch", branch, branchConfig)

	branchConfig["pg_settings"] = map[string]string{}
	branchConfig["extensions"] = []string{"vector", "postgis"}
	branch = mustApply(t, h, "ybm_amp_branch", branch, branchConfig)
	if got := attrStrings(t, branch, "extensions"); !reflect.DeepEqual(got, []string{"vector", "postgis"}) {
		t.Errorf("extensions = %v", got)
	}
	requirePlanNoop(t, h, "ybm_amp_branch", h.read("ybm_amp_branch", branch), branchConfig)
	if _, diags := h.apply("ybm_amp_branch", branch, withAttrs(branchConfig, map[string]interface{}{"extensions": []string{"postgis"}})); !strings.Contains(diagText(diags), "cannot be removed") {
		t.Errorf("removing an extension was allowed: %s", diagText(diags))
	}

	// A branch forked from another branch resolves its parent's ID, and reads it back from the spec.
	childConfig := map[string]interface{}{"cluster_name": "orders", "branch_name": "feature", "parent_branch": "dev"}
	child := mustApply(t, h, "ybm_amp_branch", tftypes.Value{}, childConfig)
	if spec := managedSpec(t, h, "us-east-1", ampKindBranch, pool+"/orders/feature"); spec["parent_branch_id"] != fakeID(pool+"/orders/dev") {
		t.Errorf("child spec = %v", spec)
	}
	imported := h.read("ybm_amp_branch", tfObject(t, h.resourceType("ybm_amp_branch"), map[string]interface{}{"cluster_name": "orders", "branch_name": "feature"}))
	if attrString(t, imported, "parent_branch") != "dev" {
		t.Errorf("imported parent_branch = %s", attrString(t, imported, "parent_branch"))
	}

	// Endpoints: a read-only replica of main and the read-write endpoint of dev.
	requirePlanError(t, h, "ybm_amp_endpoint", tftypes.Value{}, map[string]interface{}{
		"cluster_name": "orders", "endpoint_name": "replica", "credentials": creds("S3cretPassw0rd"),
	}, "does not take a password")
	replicaConfig := map[string]interface{}{"cluster_name": "orders", "endpoint_name": "replica", "cpu": "1", "allowed_cidrs": []string{}}
	replica := mustApply(t, h, "ybm_amp_endpoint", tftypes.Value{}, replicaConfig)
	if attrString(t, replica, "role") != "read_only" || attrString(t, replica, "branch_name") != "main" ||
		attrString(t, replica, "host") != "replica.amp.example.com" || attrString(t, replica, "memory") != "8192Mi" {
		t.Errorf("replica = %v", replica)
	}
	requirePlanNoop(t, h, "ybm_amp_endpoint", replica, replicaConfig)
	// A password cannot be added to a read_only endpoint later either.
	requirePlanError(t, h, "ybm_amp_endpoint", replica, withAttrs(replicaConfig, map[string]interface{}{"credentials": creds("S3cretPassw0rd")}),
		"does not take a password")
	replicaConfig["cpu"] = "2"
	replicaConfig["memory"] = "32Gi"
	replica = mustApply(t, h, "ybm_amp_endpoint", replica, replicaConfig)
	if spec := managedSpec(t, h, "us-east-1", ampKindEndpoint, pool+"/orders/main/replica"); fmt.Sprint(spec["resources"]) != "map[cpu:2 memory:32Gi]" {
		t.Errorf("replica spec = %v", spec)
	}

	rwConfig := map[string]interface{}{"cluster_name": "orders", "branch_name": "dev", "endpoint_name": "rw", "role": "read_write"}
	requirePlanError(t, h, "ybm_amp_endpoint", tftypes.Value{}, rwConfig, "requires credentials.password")
	rwConfig["credentials"] = creds("DevPassw0rd!")
	rw := mustApply(t, h, "ybm_amp_endpoint", tftypes.Value{}, rwConfig)
	if attrString(t, rw, "host") != "rw.amp.example.com" {
		t.Errorf("rw = %v", rw)
	}
	// Removing the password of a read_write endpoint is refused at plan time.
	requirePlanError(t, h, "ybm_amp_endpoint", rw, withAttrs(rwConfig, map[string]interface{}{"credentials": nil}), "requires credentials.password")

	// A branch with endpoints or forks, and a cluster with branches, refuse deletion unless forced.
	if diags := h.destroy("ybm_amp_branch", branch); !strings.Contains(diagText(diags), "endpoint "+pool+"/orders/dev/rw") ||
		!strings.Contains(diagText(diags), "branch "+pool+"/orders/feature") {
		t.Errorf("a branch with endpoints and forks was deleted: %s", diagText(diags))
	}
	if diags := h.destroy("ybm_amp_cluster", cluster); !strings.Contains(diagText(diags), "force_destroy") {
		t.Errorf("a cluster with branches was deleted: %s", diagText(diags))
	}
	if !managedDeclared(h, "us-east-1", ampKindBranch, pool+"/orders/dev") {
		t.Fatal("a refused deletion deleted the branch")
	}

	// Forced, deleting a branch takes its endpoints and forks with it.
	branchConfig["force_destroy"] = true
	branch = mustApply(t, h, "ybm_amp_branch", branch, branchConfig)
	if diags := h.destroy("ybm_amp_branch", branch); hasErrors(diags) {
		t.Fatal(diagText(diags))
	}
	for _, name := range []string{pool + "/orders/dev/rw", pool + "/orders/feature"} {
		if managedDeclared(h, "us-east-1", ampKindEndpoint, name) || managedDeclared(h, "us-east-1", ampKindBranch, name) {
			t.Errorf("%s survived its branch's deletion", name)
		}
	}
	for _, r := range []struct {
		name  string
		state tftypes.Value
	}{{"ybm_amp_endpoint", rw}, {"ybm_amp_branch", child}} {
		if diags := h.destroy(r.name, r.state); hasErrors(diags) {
			t.Fatalf("destroying a %s already gone: %s", r.name, diagText(diags))
		}
	}
	if diags := h.destroy("ybm_amp_endpoint", replica); hasErrors(diags) {
		t.Fatal(diagText(diags))
	}
	if managedDeclared(h, "us-east-1", ampKindEndpoint, pool+"/orders/main/replica") {
		t.Error("the replica was not deleted")
	}
	// With only what the cluster resource owns left, the cluster deletes without force.
	if diags := h.destroy("ybm_amp_cluster", cluster); hasErrors(diags) {
		t.Fatal(diagText(diags))
	}
}

func TestByocStoragePoolsAndClusters(t *testing.T) {
	h := newTfHarness(t)
	h.fake.addByocEnv("acme", "READY")

	poolConfig := map[string]interface{}{"environment_id": "acme", "name": "payments"}
	pool := mustApply(t, h, "ybm_byoc_storage_pool", tftypes.Value{}, poolConfig)
	if spec, ok := byocSpec(t, h, "acme", ampKindStoragePool, "payments"); !ok || spec["owner_user_id"] != testAccountID {
		t.Errorf("pool spec = %v", spec)
	}
	if attrString(t, pool, "owner_user_id") != testAccountID || attrString(t, pool, "state") != "ready" {
		t.Errorf("pool = %v", pool)
	}
	requirePlanNoop(t, h, "ybm_byoc_storage_pool", pool, poolConfig)

	// A cluster in a pool that does not exist fails, and points at ybm_byoc_storage_pool.
	if _, diags := h.apply("ybm_byoc_cluster", tftypes.Value{}, map[string]interface{}{
		"environment_id": "acme", "storage_pool": "missing", "cluster_name": "orders", "credentials": creds("S3cretPassw0rd"),
	}); !strings.Contains(diagText(diags), "ybm_byoc_storage_pool") {
		t.Errorf("a cluster was created in a missing pool: %s", diagText(diags))
	}

	// Parallel creates in one environment must not drop each other's clusters.
	names := []string{"a", "b", "c", "d", "e", "f"}
	states := make([]tftypes.Value, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			state, diags := h.apply("ybm_byoc_cluster", tftypes.Value{}, map[string]interface{}{
				"environment_id": "acme", "storage_pool": "payments", "cluster_name": name, "credentials": creds("S3cretPassw0rd"),
				"endpoint_pg_settings": map[string]string{"work_mem": "64MB"},
			})
			if hasErrors(diags) {
				t.Errorf("create %s: %s", name, diagText(diags))
			}
			states[i] = state
		}(i, name)
	}
	wg.Wait()
	for _, name := range names {
		if _, ok := byocSpec(t, h, "acme", ampKindCluster, "payments/"+name); !ok {
			t.Errorf("cluster %s was lost", name)
		}
	}
	if attrString(t, states[0], "host") != "primary.amp.example.com" || attrString(t, states[0], "storage_pool") != "payments" {
		t.Errorf("cluster = %v", states[0])
	}

	branchConfig := map[string]interface{}{
		"environment_id": "acme", "storage_pool": "payments", "cluster_name": "a", "branch_name": "dev", "extensions": []string{"vector"},
	}
	branch := mustApply(t, h, "ybm_amp_branch", tftypes.Value{}, branchConfig)
	if spec, _ := byocSpec(t, h, "acme", ampKindBranch, "payments/a/dev"); fmt.Sprint(spec["extensions"]) != "[vector]" {
		t.Errorf("branch spec = %v", spec)
	}
	endpoint := mustApply(t, h, "ybm_amp_endpoint", tftypes.Value{}, map[string]interface{}{
		"environment_id": "acme", "storage_pool": "payments", "cluster_name": "a", "branch_name": "dev", "endpoint_name": "replica",
	})
	if attrString(t, endpoint, "host") != "replica.amp.example.com" {
		t.Errorf("endpoint = %v", endpoint)
	}

	// Resize a BYOC cluster in place.
	clusterConfig := map[string]interface{}{
		"environment_id": "acme", "storage_pool": "payments", "cluster_name": "a", "credentials": creds("S3cretPassw0rd"), "cpu": "4", "memory": "64Gi",
	}
	state := mustApply(t, h, "ybm_byoc_cluster", states[0], clusterConfig)
	if spec, _ := byocSpec(t, h, "acme", ampKindEndpoint, ampEndpointName("payments", "a")); fmt.Sprint(spec["resources"]) != "map[cpu:4 memory:64Gi]" || spec["password"] != "S3cretPassw0rd" {
		t.Errorf("endpoint spec = %v", spec)
	}

	// Deleting a cluster with branches needs force_destroy; then its branches and endpoints go too.
	if diags := h.destroy("ybm_byoc_cluster", state); !strings.Contains(diagText(diags), "force_destroy") {
		t.Errorf("a cluster with branches was deleted: %s", diagText(diags))
	}
	state = mustApply(t, h, "ybm_byoc_cluster", state, withAttrs(clusterConfig, map[string]interface{}{"force_destroy": true}))
	if diags := h.destroy("ybm_byoc_cluster", state); hasErrors(diags) {
		t.Fatal(diagText(diags))
	}
	for _, name := range []string{"payments/a", "payments/a/dev", "payments/a/dev/replica"} {
		for _, kind := range []string{ampKindCluster, ampKindBranch, ampKindEndpoint} {
			if _, ok := byocSpec(t, h, "acme", kind, name); ok {
				t.Errorf("%s %s survived the cluster's deletion", kind, name)
			}
		}
	}
	if diags := h.destroy("ybm_amp_endpoint", endpoint); hasErrors(diags) {
		t.Fatal(diagText(diags))
	}
	if diags := h.destroy("ybm_amp_branch", branch); hasErrors(diags) {
		t.Fatal(diagText(diags))
	}
	for i := 1; i < len(names); i++ {
		if diags := h.destroy("ybm_byoc_cluster", states[i]); hasErrors(diags) {
			t.Fatal(diagText(diags))
		}
	}

	// Destroying the pool leaves it declared, as AMP does not delete pools, and creating it again
	// adopts it.
	diags := h.destroy("ybm_byoc_storage_pool", pool)
	if hasErrors(diags) || !strings.Contains(diagText(diags), "was not deleted") {
		t.Errorf("destroy pool: %s", diagText(diags))
	}
	h.fake.mu.Lock()
	left := desiredNames(h.fake.byoc["acme"].desired)
	h.fake.mu.Unlock()
	if strings.Join(left, ",") != "storage-pool:payments" {
		t.Errorf("desired state after destroying everything: %v", left)
	}
	mustApply(t, h, "ybm_byoc_storage_pool", tftypes.Value{}, poolConfig)
	if _, diags := h.apply("ybm_byoc_storage_pool", tftypes.Value{}, withAttrs(poolConfig, map[string]interface{}{
		"owner_user_id": "11111111-2222-3333-4444-555555555555",
	})); !strings.Contains(diagText(diags), "already exists with owner") {
		t.Errorf("a pool was adopted with another owner: %s", diagText(diags))
	}
}

func TestByocClusterWaitsForTheAgentToEnroll(t *testing.T) {
	h := newTfHarness(t)
	h.fake.addByocEnv("fresh", "PENDING")
	h.fake.addByocPool("fresh", "payments")
	h.fake.addByocEnv("paused", "SUSPENDED")
	h.fake.addByocPool("paused", "payments")

	// The agent enrolls while the cluster waits, as when helm_release installs it in the same apply.
	go func() {
		time.Sleep(20 * time.Millisecond)
		h.fake.setByocState("fresh", "ENROLLING")
		time.Sleep(20 * time.Millisecond)
		h.fake.setByocState("fresh", "READY")
	}()
	state := mustApply(t, h, "ybm_byoc_cluster", tftypes.Value{}, map[string]interface{}{
		"environment_id": "fresh", "storage_pool": "payments", "cluster_name": "orders", "credentials": creds("S3cretPassw0rd"),
	})
	if attrString(t, state, "state") != "ready" {
		t.Errorf("cluster = %v", state)
	}

	// An environment whose agent will never act fails at once.
	if _, diags := h.apply("ybm_byoc_cluster", tftypes.Value{}, map[string]interface{}{
		"environment_id": "paused", "storage_pool": "payments", "cluster_name": "orders", "credentials": creds("S3cretPassw0rd"),
	}); !strings.Contains(diagText(diags), "SUSPENDED") {
		t.Errorf("a cluster was created in a suspended environment: %s", diagText(diags))
	}
}
