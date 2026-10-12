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

func TestAmpClusterLifecycle(t *testing.T) {
	h := newTfHarness(t)
	pool := testAccountID
	config := map[string]interface{}{
		"cluster_name": "orders",
		"region":       "us-east-1",
		"password":     "S3cretPassw0rd",
		"pg_settings":  map[string]string{"work_mem": "64MB"},
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
	if got := attrStringMap(t, state, "pg_settings"); !reflect.DeepEqual(got, map[string]string{"work_mem": "64MB"}) {
		t.Errorf("pg_settings = %v", got)
	}
	requirePlanNoop(t, h, "ybm_amp_cluster", state, config)
	requirePlanNoop(t, h, "ybm_amp_cluster", h.read("ybm_amp_cluster", state), config)

	// Resize in place; memory follows cpu when it is not set.
	config["cpu"] = "4"
	config["pg_settings"] = map[string]string{"work_mem": "128MB", "max_connections": "200"}
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

	// Clearing pg_settings sends an empty map, which replaces the stored one.
	delete(config, "pg_settings")
	state = mustApply(t, h, "ybm_amp_cluster", state, config)
	if spec := managedSpec(t, h, "us-east-1", ampKindEndpoint, ampEndpointName(pool, "orders")); fmt.Sprint(spec["pg_settings"]) != "map[]" {
		t.Errorf("pg_settings were not cleared: %v", spec["pg_settings"])
	}
	requirePlanNoop(t, h, "ybm_amp_cluster", state, config)

	// The password cannot change, and a new PG version replaces the cluster.
	changed := map[string]interface{}{}
	for k, v := range config {
		changed[k] = v
	}
	changed["password"] = "AnotherPassw0rd"
	if p := h.plan("ybm_amp_cluster", state, changed); !hasErrors(p.diags) {
		t.Error("a password change was planned")
	}
	changed["password"] = config["password"]
	changed["pg_version"] = 17
	if p := h.plan("ybm_amp_cluster", state, changed); !p.requiresReplace {
		t.Error("a PG version change does not force a replacement")
	}

	if diags := h.destroy("ybm_amp_cluster", state); hasErrors(diags) {
		t.Fatal(diagText(diags))
	}
	if _, ok := findAmpDesired(h.fake.managed["us-east-1"], ampKindCluster, pool+"/orders"); ok {
		t.Error("the cluster was not deleted")
	}
}

func TestAmpBranchesAndEndpoints(t *testing.T) {
	h := newTfHarness(t)
	pool := testAccountID
	mustApply(t, h, "ybm_amp_cluster", tftypes.Value{}, map[string]interface{}{
		"cluster_name": "orders", "region": "us-east-1", "password": "S3cretPassw0rd",
	})

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

	branchConfig["pg_settings"] = map[string]string{"work_mem": "256MB"}
	branchConfig["extensions"] = []string{"vector", "postgis"}
	branch = mustApply(t, h, "ybm_amp_branch", branch, branchConfig)
	if got := attrStrings(t, branch, "extensions"); !reflect.DeepEqual(got, []string{"vector", "postgis"}) {
		t.Errorf("extensions = %v", got)
	}
	branchConfig["extensions"] = []string{"postgis"}
	if _, diags := h.apply("ybm_amp_branch", branch, branchConfig); !strings.Contains(diagText(diags), "cannot be removed") {
		t.Errorf("removing an extension was allowed: %s", diagText(diags))
	}
	branchConfig["extensions"] = []string{"vector", "postgis"}

	// A branch forked from another branch resolves its parent's ID.
	child := mustApply(t, h, "ybm_amp_branch", tftypes.Value{}, map[string]interface{}{
		"cluster_name": "orders", "branch_name": "feature", "parent_branch": "dev",
	})
	if spec := managedSpec(t, h, "us-east-1", ampKindBranch, pool+"/orders/feature"); spec["parent_branch_id"] != fakeID(pool+"/orders/dev") {
		t.Errorf("child spec = %v", spec)
	}
	if attrString(t, child, "parent_branch") != "dev" {
		t.Errorf("parent_branch = %s", attrString(t, child, "parent_branch"))
	}

	// Endpoints: a read-only replica of main and the read-write endpoint of dev.
	if _, diags := h.apply("ybm_amp_endpoint", tftypes.Value{}, map[string]interface{}{
		"cluster_name": "orders", "endpoint_name": "replica", "password": "S3cretPassw0rd",
	}); !strings.Contains(diagText(diags), "does not take a password") {
		t.Errorf("a read_only endpoint took a password: %s", diagText(diags))
	}
	replicaConfig := map[string]interface{}{"cluster_name": "orders", "endpoint_name": "replica", "cpu": "1"}
	replica := mustApply(t, h, "ybm_amp_endpoint", tftypes.Value{}, replicaConfig)
	if attrString(t, replica, "role") != "read_only" || attrString(t, replica, "branch_name") != "main" ||
		attrString(t, replica, "host") != "replica.amp.example.com" || attrString(t, replica, "memory") != "8192Mi" {
		t.Errorf("replica = %v", replica)
	}
	requirePlanNoop(t, h, "ybm_amp_endpoint", replica, replicaConfig)
	replicaConfig["cpu"] = "2"
	replicaConfig["memory"] = "32Gi"
	replica = mustApply(t, h, "ybm_amp_endpoint", replica, replicaConfig)
	if spec := managedSpec(t, h, "us-east-1", ampKindEndpoint, pool+"/orders/main/replica"); fmt.Sprint(spec["resources"]) != "map[cpu:2 memory:32Gi]" {
		t.Errorf("replica spec = %v", spec)
	}

	if _, diags := h.apply("ybm_amp_endpoint", tftypes.Value{}, map[string]interface{}{
		"cluster_name": "orders", "branch_name": "dev", "endpoint_name": "rw", "role": "read_write",
	}); !strings.Contains(diagText(diags), "requires a password") {
		t.Errorf("a read_write endpoint was created without a password: %s", diagText(diags))
	}
	rw := mustApply(t, h, "ybm_amp_endpoint", tftypes.Value{}, map[string]interface{}{
		"cluster_name": "orders", "branch_name": "dev", "endpoint_name": "rw", "role": "read_write", "password": "DevPassw0rd!",
	})
	if attrString(t, rw, "host") != "rw.amp.example.com" {
		t.Errorf("rw = %v", rw)
	}

	// Deleting a branch takes its endpoints with it.
	if diags := h.destroy("ybm_amp_branch", branch); hasErrors(diags) {
		t.Fatal(diagText(diags))
	}
	if _, ok := findAmpDesired(h.fake.managed["us-east-1"], ampKindEndpoint, pool+"/orders/dev/rw"); ok {
		t.Error("the branch's endpoint survived its deletion")
	}
	if diags := h.destroy("ybm_amp_endpoint", rw); hasErrors(diags) {
		t.Fatalf("destroying an endpoint already gone: %s", diagText(diags))
	}
	if diags := h.destroy("ybm_amp_endpoint", replica); hasErrors(diags) {
		t.Fatal(diagText(diags))
	}
	if _, ok := findAmpDesired(h.fake.managed["us-east-1"], ampKindEndpoint, pool+"/orders/main/replica"); ok {
		t.Error("the replica was not deleted")
	}
}

func TestByocClustersBranchesAndEndpoints(t *testing.T) {
	h := newTfHarness(t)
	pool := testAccountID
	h.fake.addByocEnv("acme", "READY")
	h.fake.addByocEnv("fresh", "PENDING")

	if _, diags := h.apply("ybm_byoc_cluster", tftypes.Value{}, map[string]interface{}{
		"environment_id": "fresh", "cluster_name": "orders", "password": "S3cretPassw0rd",
	}); !strings.Contains(diagText(diags), "install the agent") {
		t.Errorf("a cluster was created in a PENDING environment: %s", diagText(diags))
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
				"environment_id": "acme", "cluster_name": name, "password": "S3cretPassw0rd",
				"pg_settings": map[string]string{"work_mem": "64MB"},
			})
			if hasErrors(diags) {
				t.Errorf("create %s: %s", name, diagText(diags))
			}
			states[i] = state
		}(i, name)
	}
	wg.Wait()
	for _, name := range names {
		if _, ok := byocSpec(t, h, "acme", ampKindCluster, pool+"/"+name); !ok {
			t.Errorf("cluster %s was lost", name)
		}
	}
	if attrString(t, states[0], "host") != "primary.amp.example.com" {
		t.Errorf("cluster = %v", states[0])
	}
	if _, ok := byocSpec(t, h, "acme", ampKindStoragePool, pool); !ok {
		t.Error("the storage pool was not declared")
	}

	branch := mustApply(t, h, "ybm_amp_branch", tftypes.Value{}, map[string]interface{}{
		"environment_id": "acme", "cluster_name": "a", "branch_name": "dev", "extensions": []string{"vector"},
	})
	if spec, _ := byocSpec(t, h, "acme", ampKindBranch, pool+"/a/dev"); fmt.Sprint(spec["extensions"]) != "[vector]" {
		t.Errorf("branch spec = %v", spec)
	}
	endpoint := mustApply(t, h, "ybm_amp_endpoint", tftypes.Value{}, map[string]interface{}{
		"environment_id": "acme", "cluster_name": "a", "branch_name": "dev", "endpoint_name": "replica",
	})
	if attrString(t, endpoint, "host") != "replica.amp.example.com" {
		t.Errorf("endpoint = %v", endpoint)
	}

	// Resize a BYOC cluster in place.
	state := mustApply(t, h, "ybm_byoc_cluster", states[0], map[string]interface{}{
		"environment_id": "acme", "cluster_name": "a", "password": "S3cretPassw0rd", "cpu": "4", "memory": "64Gi",
	})
	if spec, _ := byocSpec(t, h, "acme", ampKindEndpoint, ampEndpointName(pool, "a")); fmt.Sprint(spec["resources"]) != "map[cpu:4 memory:64Gi]" || spec["password"] != "S3cretPassw0rd" {
		t.Errorf("endpoint spec = %v", spec)
	}

	// Deleting the cluster removes its branches and endpoints too; destroying them after is a no-op.
	if diags := h.destroy("ybm_byoc_cluster", state); hasErrors(diags) {
		t.Fatal(diagText(diags))
	}
	for _, name := range []string{pool + "/a", pool + "/a/dev", pool + "/a/dev/replica"} {
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
	h.fake.mu.Lock()
	defer h.fake.mu.Unlock()
	if left := h.fake.byoc["acme"].desired; len(left) != 0 {
		t.Errorf("desired state is not empty after destroying everything: %v", left)
	}
	// The final write empties the desired state, which YCP accepts only when asked to.
	for i := len(h.fake.requests) - 1; i >= 0; i-- {
		if strings.HasPrefix(h.fake.requests[i], "POST") {
			if h.fake.bodies[i]["allow_empty"] != true {
				t.Errorf("emptying desired state did not set allow_empty: %v", h.fake.bodies[i])
			}
			break
		}
	}
}
