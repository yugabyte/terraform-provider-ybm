/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package managed

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// These tests drive the Aeon PG resources through the provider protocol, as Terraform does,
// against a stateful fake of the YBM /ycp routes.

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

func setManagedSpec(h *tfHarness, region, kind, name string, spec map[string]interface{}) {
	h.fake.mu.Lock()
	defer h.fake.mu.Unlock()
	for i, r := range h.fake.managed[region] {
		if r.Kind == kind && r.Name == name {
			h.fake.managed[region][i].Spec = mustMarshal(spec)
		}
	}
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

// sentBodies returns the bodies of the requests with the given method that mention name.
func sentBodies(h *tfHarness, method, name string) []string {
	h.fake.mu.Lock()
	defer h.fake.mu.Unlock()
	var out []string
	for i, req := range h.fake.requests {
		if body := h.fake.bodies[i]; body != nil && strings.HasPrefix(req, method+" ") {
			b, _ := json.Marshal(body)
			if strings.Contains(string(b), name) {
				out = append(out, string(b))
			}
		}
	}
	return out
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

// plannedKnown reports whether the plan knows the attribute's value.
func plannedKnown(t *testing.T, p tfPlanResult, attr string) bool {
	var attrs map[string]tftypes.Value
	if err := p.planned.As(&attrs); err != nil {
		t.Fatal(err)
	}
	return attrs[attr].IsKnown()
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

func sorted(values []string) []string {
	sort.Strings(values)
	return values
}

func TestAeonPgClusterLifecycle(t *testing.T) {
	h := newTfHarness(t)
	pool := testAccountID
	config := map[string]interface{}{
		"cluster_name":       "orders",
		"region":             "us-east-1",
		"branch_pg_settings": map[string]string{"work_mem": "32MB"},
		"branch_extensions":  []string{"vector"},
	}
	cluster := mustApply(t, h, "ybm_aeon_pg_cluster", tftypes.Value{}, config)
	for attr, want := range map[string]string{
		"state": "ready", "engine": "postgres", "environment_id": "managed-us-east-1", "cluster_id": fakeID(pool + "/orders"), "account_id": pool,
	} {
		if got := attrString(t, cluster, attr); got != want {
			t.Errorf("%s = %q, want %q", attr, got, want)
		}
	}
	// Unset values are left to the service and read back from what it applied.
	if spec := managedSpec(t, h, "us-east-1", ampKindCluster, pool+"/orders"); len(spec) != 0 {
		t.Errorf("cluster spec = %v", spec)
	}
	// The main branch's extensions are added once it exists, since a new branch takes none.
	if spec := managedSpec(t, h, "us-east-1", ampKindBranch, pool+"/orders/main"); fmt.Sprint(spec) != "map[extensions:[vector] pg_settings:map[work_mem:32MB]]" {
		t.Errorf("main branch spec = %v", spec)
	}
	if posts := sentBodies(h, "POST", "orders/main"); len(posts) != 1 || strings.Contains(posts[0], "vector") {
		t.Errorf("the main branch was created with its extensions: %v", posts)
	}
	requirePlanNoop(t, h, "ybm_aeon_pg_cluster", cluster, config)
	requirePlanNoop(t, h, "ybm_aeon_pg_cluster", h.read("ybm_aeon_pg_cluster", cluster), config)

	// The read-write endpoint on main is an ordinary endpoint resource.
	primaryConfig := map[string]interface{}{
		"cluster_name": "orders", "cluster_id": attrString(t, cluster, "cluster_id"), "endpoint_name": "primary", "role": "read_write",
		"credentials": creds("S3cretPassw0rd"), "pg_settings": map[string]string{"work_mem": "64MB"},
	}
	primary := mustApply(t, h, "ybm_aeon_pg_endpoint", tftypes.Value{}, primaryConfig)
	for attr, want := range map[string]string{
		"host": "primary.amp.example.com", "port": "5432", "state": "ready", "username": "cloud_admin", "database": "postgres",
		"cpu": "2", "memory": "16384Mi", "connectivity_mode": "cluster_ip", "branch_name": "main",
	} {
		if got := attrString(t, primary, attr); got != want {
			t.Errorf("%s = %q, want %q", attr, got, want)
		}
	}
	spec := managedSpec(t, h, "us-east-1", ampKindEndpoint, pool+"/orders/main/primary")
	if fmt.Sprint(spec) != "map[password:S3cretPassw0rd pg_settings:map[work_mem:64MB] role:read_write]" {
		t.Errorf("endpoint spec = %v", spec)
	}
	requirePlanNoop(t, h, "ybm_aeon_pg_endpoint", h.read("ybm_aeon_pg_endpoint", primary), primaryConfig)

	// A settings change keeps memory and the connection details known in the plan.
	primaryConfig["pg_settings"] = map[string]string{"work_mem": "128MB", "max_connections": "200"}
	p := h.plan("ybm_aeon_pg_endpoint", primary, primaryConfig)
	for _, attr := range []string{"memory", "host", "port", "tls_ca_certificate"} {
		if !plannedKnown(t, p, attr) {
			t.Errorf("%s is unknown in a plan that does not change it", attr)
		}
	}
	primary = mustApply(t, h, "ybm_aeon_pg_endpoint", primary, primaryConfig)

	// Resize in place; memory follows cpu when it is not set.
	primaryConfig["cpu"] = "4"
	if p := h.plan("ybm_aeon_pg_endpoint", primary, primaryConfig); p.requiresReplace || plannedKnown(t, p, "memory") {
		t.Fatalf("a resize replaces the endpoint or keeps its memory: %v", p.planned)
	}
	primary = mustApply(t, h, "ybm_aeon_pg_endpoint", primary, primaryConfig)
	if attrString(t, primary, "memory") != "32768Mi" {
		t.Errorf("memory = %s", attrString(t, primary, "memory"))
	}
	spec = managedSpec(t, h, "us-east-1", ampKindEndpoint, pool+"/orders/main/primary")
	if fmt.Sprint(spec["resources"]) != "map[cpu:4 memory:32768Mi]" || spec["password"] != "S3cretPassw0rd" || spec["role"] != "read_write" {
		t.Errorf("endpoint spec = %v", spec)
	}
	requirePlanNoop(t, h, "ybm_aeon_pg_endpoint", primary, primaryConfig)

	// The password is never read back, so a server that stops returning it, or returns a
	// verifier instead, changes nothing.
	spec["password"] = "SCRAM-SHA-256$4096:c2FsdA==$a2V5:a2V5"
	setManagedSpec(h, "us-east-1", ampKindEndpoint, pool+"/orders/main/primary", spec)
	requirePlanNoop(t, h, "ybm_aeon_pg_endpoint", h.read("ybm_aeon_pg_endpoint", primary), primaryConfig)
	requirePlanError(t, h, "ybm_aeon_pg_endpoint", primary, withAttrs(primaryConfig, map[string]interface{}{"credentials": creds("AnotherPassw0rd")}),
		"Password cannot be changed")
	requirePlanError(t, h, "ybm_aeon_pg_endpoint", primary, withAttrs(primaryConfig, map[string]interface{}{"credentials": nil}),
		"Password cannot be changed")

	// Settings: an emptied map clears them and reads back empty; removing the attribute after
	// managing it clears them too; never setting it leaves them alone.
	config["branch_pg_settings"] = map[string]string{}
	cluster = mustApply(t, h, "ybm_aeon_pg_cluster", cluster, config)
	if spec := managedSpec(t, h, "us-east-1", ampKindBranch, pool+"/orders/main"); fmt.Sprint(spec["pg_settings"]) != "map[]" {
		t.Errorf("branch pg_settings were not cleared: %v", spec["pg_settings"])
	}
	requirePlanNoop(t, h, "ybm_aeon_pg_cluster", h.read("ybm_aeon_pg_cluster", cluster), config)
	delete(primaryConfig, "pg_settings")
	primary = mustApply(t, h, "ybm_aeon_pg_endpoint", primary, primaryConfig)
	if spec := managedSpec(t, h, "us-east-1", ampKindEndpoint, pool+"/orders/main/primary"); fmt.Sprint(spec["pg_settings"]) != "map[]" {
		t.Errorf("endpoint pg_settings were not cleared: %v", spec["pg_settings"])
	}
	requirePlanNoop(t, h, "ybm_aeon_pg_endpoint", h.read("ybm_aeon_pg_endpoint", primary), primaryConfig)

	// Extensions are a set, can be added but not removed, and only the cluster's force a replacement.
	config["branch_extensions"] = []string{"postgis", "vector"}
	cluster = mustApply(t, h, "ybm_aeon_pg_cluster", cluster, config)
	requirePlanNoop(t, h, "ybm_aeon_pg_cluster", cluster, withAttrs(config, map[string]interface{}{"branch_extensions": []string{"vector", "postgis"}}))
	requirePlanError(t, h, "ybm_aeon_pg_cluster", cluster, withAttrs(config, map[string]interface{}{"branch_extensions": []string{"vector"}}),
		"Extension postgis cannot be removed")
	for attr, v := range map[string]interface{}{"pg_version": 17, "extensions": []string{"vector"}} {
		if p := h.plan("ybm_aeon_pg_cluster", cluster, withAttrs(config, map[string]interface{}{attr: v})); !p.requiresReplace {
			t.Errorf("a %s change does not force a replacement", attr)
		}
	}

	// Replacing the cluster gives it an unknown cluster_id, which replaces its endpoints too.
	if p := h.plan("ybm_aeon_pg_endpoint", primary, withAttrs(primaryConfig, map[string]interface{}{"cluster_id": unknown{}})); !p.requiresReplace {
		t.Error("an endpoint is not replaced with its cluster")
	}

	// A cluster with endpoints refuses deletion unless forced; without them it deletes.
	if diags := h.destroy("ybm_aeon_pg_cluster", cluster); !strings.Contains(diagText(diags), "endpoint "+pool+"/orders/main/primary") {
		t.Errorf("a cluster with an endpoint was deleted: %s", diagText(diags))
	}
	if diags := h.destroy("ybm_aeon_pg_endpoint", primary); hasErrors(diags) {
		t.Fatal(diagText(diags))
	}
	if diags := h.destroy("ybm_aeon_pg_cluster", cluster); hasErrors(diags) {
		t.Fatal(diagText(diags))
	}
	if managedDeclared(h, "us-east-1", ampKindCluster, pool+"/orders") || managedDeclared(h, "us-east-1", ampKindBranch, pool+"/orders/main") {
		t.Error("the cluster was not deleted")
	}
}

func TestAeonPgInvalidConfigurationFailsAtPlanTime(t *testing.T) {
	h := newTfHarness(t)
	endpoint := map[string]interface{}{"cluster_name": "orders", "endpoint_name": "primary", "role": "read_write", "credentials": creds("S3cretPassw0rd")}

	requirePlanError(t, h, "ybm_aeon_pg_endpoint", tftypes.Value{}, withAttrs(endpoint, map[string]interface{}{"suspend_timeout_seconds": 300}), "cannot suspend")
	requirePlanError(t, h, "ybm_aeon_pg_endpoint", tftypes.Value{}, withAttrs(endpoint, map[string]interface{}{
		"credentials": map[string]interface{}{"password_wo": "S3cretPassw0rd", "password_wo_version": 1},
	}), "Write-only passwords")
	requirePlanError(t, h, "ybm_aeon_pg_endpoint", tftypes.Value{}, withAttrs(endpoint, map[string]interface{}{
		"credentials": map[string]interface{}{"username": "app", "password": "S3cretPassw0rd"},
	}), "Only the cloud_admin user")
	requirePlanError(t, h, "ybm_aeon_pg_endpoint", tftypes.Value{}, withAttrs(endpoint, map[string]interface{}{"role": nil}), "does not take a password")
	requirePlanError(t, h, "ybm_aeon_pg_endpoint", tftypes.Value{}, withAttrs(endpoint, map[string]interface{}{"cpu": "two"}), "CPU quantity")
	requirePlanError(t, h, "ybm_aeon_pg_endpoint", tftypes.Value{}, withAttrs(endpoint, map[string]interface{}{"memory": "16 GB"}), "memory quantity")
	requirePlanError(t, h, "ybm_aeon_pg_branch", tftypes.Value{}, map[string]interface{}{"cluster_name": "orders", "branch_name": "dev", "ttl": "24h"}, "cannot expire")
	requirePlanError(t, h, "ybm_aeon_pg_branch", tftypes.Value{}, map[string]interface{}{"cluster_name": "orders", "branch_name": "main"}, "main")
	requirePlanError(t, h, "ybm_aeon_pg_branch", tftypes.Value{}, map[string]interface{}{
		"environment_id": "acme", "cluster_name": "orders", "branch_name": "dev",
	}, "need its storage_pool")
	requirePlanError(t, h, "ybm_aeon_pg_endpoint", tftypes.Value{}, map[string]interface{}{
		"storage_pool": "payments", "cluster_name": "orders", "endpoint_name": "replica",
	}, "only with environment_id")

	// Naming the default user explicitly is fine, and a read_write endpoint may have no password,
	// as a branch's does.
	mustApply(t, h, "ybm_aeon_pg_cluster", tftypes.Value{}, map[string]interface{}{"cluster_name": "orders", "region": "us-east-1"})
	config := withAttrs(endpoint, map[string]interface{}{
		"credentials": map[string]interface{}{"username": "cloud_admin", "password": "S3cretPassw0rd"},
	})
	state := mustApply(t, h, "ybm_aeon_pg_endpoint", tftypes.Value{}, config)
	requirePlanNoop(t, h, "ybm_aeon_pg_endpoint", h.read("ybm_aeon_pg_endpoint", state), config)
	if p := h.plan("ybm_aeon_pg_endpoint", tftypes.NewValue(h.resourceType("ybm_aeon_pg_endpoint"), nil),
		withAttrs(endpoint, map[string]interface{}{"endpoint_name": "other", "credentials": nil})); hasErrors(p.diags) {
		t.Errorf("a read_write endpoint without a password was refused: %s", diagText(p.diags))
	}
}

func TestAeonPgBranchesAndEndpoints(t *testing.T) {
	h := newTfHarness(t)
	pool := testAccountID
	cluster := mustApply(t, h, "ybm_aeon_pg_cluster", tftypes.Value{}, map[string]interface{}{"cluster_name": "orders", "region": "us-east-1"})
	clusterID := attrString(t, cluster, "cluster_id")

	// A cluster_id that is not the cluster's fails.
	if _, diags := h.apply("ybm_aeon_pg_branch", tftypes.Value{}, map[string]interface{}{
		"cluster_name": "orders", "cluster_id": "another-cluster", "branch_name": "dev",
	}); !strings.Contains(diagText(diags), "is not the ID of cluster orders") {
		t.Errorf("a branch was created with another cluster's ID: %s", diagText(diags))
	}

	branchConfig := map[string]interface{}{
		"cluster_name": "orders",
		"cluster_id":   clusterID,
		"branch_name":  "dev",
		"pg_settings":  map[string]string{"work_mem": "64MB"},
		"extensions":   []string{"vector"},
	}
	branch := mustApply(t, h, "ybm_aeon_pg_branch", tftypes.Value{}, branchConfig)
	if attrString(t, branch, "branch_id") != fakeID(pool+"/orders/dev") || attrString(t, branch, "parent_branch") != "main" {
		t.Errorf("branch = %v", branch)
	}
	spec := managedSpec(t, h, "us-east-1", ampKindBranch, pool+"/orders/dev")
	if fmt.Sprint(spec["extensions"]) != "[vector]" || fmt.Sprint(spec["pg_settings"]) != "map[work_mem:64MB]" {
		t.Errorf("branch spec = %v", spec)
	}
	// AMP refuses extensions on a new branch, so they must have been sent as a separate update.
	if posts, puts := sentBodies(h, "POST", "orders/dev"), sentBodies(h, "PUT", "orders/dev"); len(posts) != 1 || len(puts) != 1 ||
		strings.Contains(posts[0], "vector") {
		t.Errorf("creates = %v, updates = %v", posts, puts)
	}
	requirePlanNoop(t, h, "ybm_aeon_pg_branch", branch, branchConfig)

	branchConfig["pg_settings"] = map[string]string{}
	branchConfig["extensions"] = []string{"vector", "postgis"}
	branch = mustApply(t, h, "ybm_aeon_pg_branch", branch, branchConfig)
	if got := sorted(attrStrings(t, branch, "extensions")); fmt.Sprint(got) != "[postgis vector]" {
		t.Errorf("extensions = %v", got)
	}
	requirePlanNoop(t, h, "ybm_aeon_pg_branch", h.read("ybm_aeon_pg_branch", branch), branchConfig)
	requirePlanError(t, h, "ybm_aeon_pg_branch", branch, withAttrs(branchConfig, map[string]interface{}{"extensions": []string{"postgis"}}),
		"Extension vector cannot be removed")

	// A fork resolves its parent's ID and reads it back from the spec. It never set pg_settings,
	// so adding an extension must not send any, which would wipe those it inherited.
	childConfig := map[string]interface{}{"cluster_name": "orders", "branch_name": "feature", "parent_branch": "dev"}
	child := mustApply(t, h, "ybm_aeon_pg_branch", tftypes.Value{}, childConfig)
	if spec := managedSpec(t, h, "us-east-1", ampKindBranch, pool+"/orders/feature"); spec["parent_branch_id"] != fakeID(pool+"/orders/dev") {
		t.Errorf("child spec = %v", spec)
	}
	if attrString(t, child, "cluster_id") != clusterID {
		t.Errorf("an unset cluster_id read back as %q", attrString(t, child, "cluster_id"))
	}
	childConfig["extensions"] = []string{"vector"}
	child = mustApply(t, h, "ybm_aeon_pg_branch", child, childConfig)
	for _, body := range sentBodies(h, "PUT", "orders/feature") {
		if strings.Contains(body, "pg_settings") {
			t.Errorf("an update of a fork sent pg_settings: %s", body)
		}
	}
	imported := h.read("ybm_aeon_pg_branch", tfObject(t, h.resourceType("ybm_aeon_pg_branch"), map[string]interface{}{"cluster_name": "orders", "branch_name": "feature"}))
	if attrString(t, imported, "parent_branch") != "dev" {
		t.Errorf("imported parent_branch = %s", attrString(t, imported, "parent_branch"))
	}

	// A read-only replica of main, and the read-write endpoint of dev, which inherits the cluster's
	// password and so takes none.
	replicaConfig := map[string]interface{}{"cluster_name": "orders", "endpoint_name": "replica", "cpu": "1", "allowed_cidrs": []string{}}
	replica := mustApply(t, h, "ybm_aeon_pg_endpoint", tftypes.Value{}, replicaConfig)
	if attrString(t, replica, "role") != "read_only" || attrString(t, replica, "branch_name") != "main" ||
		attrString(t, replica, "host") != "replica.amp.example.com" || attrString(t, replica, "memory") != "8192Mi" {
		t.Errorf("replica = %v", replica)
	}
	requirePlanNoop(t, h, "ybm_aeon_pg_endpoint", replica, replicaConfig)
	requirePlanError(t, h, "ybm_aeon_pg_endpoint", replica, withAttrs(replicaConfig, map[string]interface{}{"credentials": creds("S3cretPassw0rd")}),
		"does not take a password")
	replicaConfig["cpu"] = "2"
	replicaConfig["memory"] = "32Gi"
	replicaConfig["allowed_cidrs"] = []string{"10.0.0.0/16", "10.1.0.0/16"}
	replica = mustApply(t, h, "ybm_aeon_pg_endpoint", replica, replicaConfig)
	spec = managedSpec(t, h, "us-east-1", ampKindEndpoint, pool+"/orders/main/replica")
	if fmt.Sprint(spec["resources"]) != "map[cpu:2 memory:32Gi]" || fmt.Sprint(spec["connectivity"]) != "map[allowed_cidrs:[10.0.0.0/16 10.1.0.0/16] mode:cluster_ip]" {
		t.Errorf("replica spec = %v", spec)
	}
	requirePlanNoop(t, h, "ybm_aeon_pg_endpoint", replica, withAttrs(replicaConfig, map[string]interface{}{"allowed_cidrs": []string{"10.1.0.0/16", "10.0.0.0/16"}}))

	rwConfig := map[string]interface{}{"cluster_name": "orders", "branch_name": "dev", "endpoint_name": "rw", "role": "read_write"}
	rw := mustApply(t, h, "ybm_aeon_pg_endpoint", tftypes.Value{}, rwConfig)
	if attrString(t, rw, "host") != "rw.amp.example.com" {
		t.Errorf("rw = %v", rw)
	}
	if spec := managedSpec(t, h, "us-east-1", ampKindEndpoint, pool+"/orders/dev/rw"); spec["password"] != nil {
		t.Errorf("rw spec = %v", spec)
	}

	// A branch with endpoints or forks, and a cluster with branches, refuse deletion unless forced.
	if diags := h.destroy("ybm_aeon_pg_branch", branch); !strings.Contains(diagText(diags), "endpoint "+pool+"/orders/dev/rw") ||
		!strings.Contains(diagText(diags), "branch "+pool+"/orders/feature") {
		t.Errorf("a branch with endpoints and forks was deleted: %s", diagText(diags))
	}
	if diags := h.destroy("ybm_aeon_pg_cluster", cluster); !strings.Contains(diagText(diags), "force_destroy") {
		t.Errorf("a cluster with branches was deleted: %s", diagText(diags))
	}
	if !managedDeclared(h, "us-east-1", ampKindBranch, pool+"/orders/dev") {
		t.Fatal("a refused deletion deleted the branch")
	}

	// Forced, deleting a branch takes its endpoints and forks with it.
	branchConfig["force_destroy"] = true
	branch = mustApply(t, h, "ybm_aeon_pg_branch", branch, branchConfig)
	if diags := h.destroy("ybm_aeon_pg_branch", branch); hasErrors(diags) {
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
	}{{"ybm_aeon_pg_endpoint", rw}, {"ybm_aeon_pg_branch", child}} {
		if diags := h.destroy(r.name, r.state); hasErrors(diags) {
			t.Fatalf("destroying a %s already gone: %s", r.name, diagText(diags))
		}
	}

	// Forced, deleting a cluster takes what is left with it.
	cluster = mustApply(t, h, "ybm_aeon_pg_cluster", cluster, map[string]interface{}{"cluster_name": "orders", "region": "us-east-1", "force_destroy": true})
	if diags := h.destroy("ybm_aeon_pg_cluster", cluster); hasErrors(diags) {
		t.Fatal(diagText(diags))
	}
	if managedDeclared(h, "us-east-1", ampKindEndpoint, pool+"/orders/main/replica") {
		t.Error("the replica survived its cluster's deletion")
	}
	if diags := h.destroy("ybm_aeon_pg_endpoint", replica); hasErrors(diags) {
		t.Fatal(diagText(diags))
	}
}

func TestByocEnvironment(t *testing.T) {
	h := newTfHarness(t)
	config := map[string]interface{}{"name": "Acme Prod", "placement": "onprem"}
	env := mustApply(t, h, "ybm_byoc_environment", tftypes.Value{}, config)
	if attrString(t, env, "environment_id") != "acme-prod" || attrString(t, env, "agent_api_key") != "key-acme-prod" {
		t.Errorf("environment = %v", env)
	}

	// The agent reports its own region and placement when it enrolls, which replaces nothing.
	h.fake.mu.Lock()
	h.fake.byoc["acme-prod"].state, h.fake.byoc["acme-prod"].region, h.fake.byoc["acme-prod"].placement = "READY", "us-east1", "cloud"
	h.fake.mu.Unlock()
	env = h.read("ybm_byoc_environment", env)
	if attrString(t, env, "region") != "us-east1" || attrString(t, env, "placement") != "onprem" {
		t.Errorf("environment = %v", env)
	}
	requirePlanNoop(t, h, "ybm_byoc_environment", env, config)

	// A name that derives the same ID, as after an import, is not a replacement; another name is.
	for name, replace := range map[string]bool{"acme-prod": false, "ACME PROD": false, "acme-staging": true} {
		p := h.plan("ybm_byoc_environment", env, withAttrs(config, map[string]interface{}{"name": name}))
		if hasErrors(p.diags) || p.requiresReplace != replace {
			t.Errorf("renaming to %q: replace = %v, %s", name, p.requiresReplace, diagText(p.diags))
		}
	}
	imported := h.read("ybm_byoc_environment", tfObject(t, h.resourceType("ybm_byoc_environment"), map[string]interface{}{"environment_id": "acme-prod"}))
	state := mustApply(t, h, "ybm_byoc_environment", imported, config)
	if attrString(t, state, "name") != "Acme Prod" || attrString(t, state, "environment_id") != "acme-prod" {
		t.Errorf("imported environment = %v", state)
	}

	if diags := h.destroy("ybm_byoc_environment", env); hasErrors(diags) {
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
	if _, diags := h.apply("ybm_aeon_pg_byoc_cluster", tftypes.Value{}, map[string]interface{}{
		"environment_id": "acme", "storage_pool": "missing", "cluster_name": "orders",
	}); !strings.Contains(diagText(diags), "ybm_byoc_storage_pool") {
		t.Errorf("a cluster was created in a missing pool: %s", diagText(diags))
	}

	// Parallel creates in one environment must not drop each other's clusters.
	names := []string{"a", "b", "c", "d", "e", "f"}
	states := make([]tftypes.Value, len(names))
	t.Run("parallel", func(t *testing.T) {
		for i, name := range names {
			i, name := i, name
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				states[i] = mustApply(t, h.with(t), "ybm_aeon_pg_byoc_cluster", tftypes.Value{}, map[string]interface{}{
					"environment_id": "acme", "storage_pool": "payments", "cluster_name": name,
					"branch_pg_settings": map[string]string{"work_mem": "64MB"},
				})
			})
		}
	})
	for _, name := range names {
		if _, ok := byocSpec(t, h, "acme", ampKindCluster, "payments/"+name); !ok {
			t.Errorf("cluster %s was lost", name)
		}
	}
	if t.Failed() {
		t.FailNow()
	}
	clusterConfig := map[string]interface{}{"environment_id": "acme", "storage_pool": "payments", "cluster_name": "a",
		"branch_pg_settings": map[string]string{"work_mem": "64MB"}}
	if attrString(t, states[0], "state") != "ready" || attrString(t, states[0], "storage_pool") != "payments" {
		t.Errorf("cluster = %v", states[0])
	}
	requirePlanNoop(t, h, "ybm_aeon_pg_byoc_cluster", h.read("ybm_aeon_pg_byoc_cluster", states[0]), clusterConfig)

	child := map[string]interface{}{"environment_id": "acme", "storage_pool": "payments", "cluster_name": "a"}
	branch := mustApply(t, h, "ybm_aeon_pg_branch", tftypes.Value{}, withAttrs(child, map[string]interface{}{"branch_name": "dev", "extensions": []string{"vector"}}))
	if spec, _ := byocSpec(t, h, "acme", ampKindBranch, "payments/a/dev"); fmt.Sprint(spec["extensions"]) != "[vector]" {
		t.Errorf("branch spec = %v", spec)
	}
	primary := mustApply(t, h, "ybm_aeon_pg_endpoint", tftypes.Value{}, withAttrs(child, map[string]interface{}{
		"endpoint_name": "primary", "role": "read_write", "credentials": creds("S3cretPassw0rd"), "cpu": "4", "memory": "64Gi",
	}))
	if spec, _ := byocSpec(t, h, "acme", ampKindEndpoint, "payments/a/main/primary"); fmt.Sprint(spec["resources"]) != "map[cpu:4 memory:64Gi]" ||
		spec["password"] != "S3cretPassw0rd" {
		t.Errorf("endpoint spec = %v", spec)
	}
	if attrString(t, primary, "host") != "primary.amp.example.com" {
		t.Errorf("endpoint = %v", primary)
	}

	// Change the main branch in place.
	clusterConfig["branch_pg_settings"] = map[string]string{"work_mem": "128MB"}
	state := mustApply(t, h, "ybm_aeon_pg_byoc_cluster", states[0], clusterConfig)
	if spec, _ := byocSpec(t, h, "acme", ampKindBranch, "payments/a/main"); fmt.Sprint(spec["pg_settings"]) != "map[work_mem:128MB]" {
		t.Errorf("main branch spec = %v", spec)
	}

	// Deleting a cluster with endpoints and branches needs force_destroy; then they go too.
	if diags := h.destroy("ybm_aeon_pg_byoc_cluster", state); !strings.Contains(diagText(diags), "force_destroy") {
		t.Errorf("a cluster with branches was deleted: %s", diagText(diags))
	}
	state = mustApply(t, h, "ybm_aeon_pg_byoc_cluster", state, withAttrs(clusterConfig, map[string]interface{}{"force_destroy": true}))
	if diags := h.destroy("ybm_aeon_pg_byoc_cluster", state); hasErrors(diags) {
		t.Fatal(diagText(diags))
	}
	for _, name := range []string{"payments/a", "payments/a/dev", "payments/a/main/primary"} {
		for _, kind := range []string{ampKindCluster, ampKindBranch, ampKindEndpoint} {
			if _, ok := byocSpec(t, h, "acme", kind, name); ok {
				t.Errorf("%s %s survived the cluster's deletion", kind, name)
			}
		}
	}
	if diags := h.destroy("ybm_aeon_pg_endpoint", primary); hasErrors(diags) {
		t.Fatal(diagText(diags))
	}
	if diags := h.destroy("ybm_aeon_pg_branch", branch); hasErrors(diags) {
		t.Fatal(diagText(diags))
	}
	for i := 1; i < len(names); i++ {
		if diags := h.destroy("ybm_aeon_pg_byoc_cluster", states[i]); hasErrors(diags) {
			t.Fatal(diagText(diags))
		}
	}

	// Destroying the pool leaves it declared, as pools cannot be deleted, and creating it again
	// fails and points at import.
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
	if _, diags := h.apply("ybm_byoc_storage_pool", tftypes.Value{}, poolConfig); !strings.Contains(diagText(diags), "import it") {
		t.Errorf("an existing pool was adopted: %s", diagText(diags))
	}
	imported := h.read("ybm_byoc_storage_pool", tfObject(t, h.resourceType("ybm_byoc_storage_pool"), map[string]interface{}{"environment_id": "acme", "name": "payments"}))
	requirePlanNoop(t, h, "ybm_byoc_storage_pool", imported, poolConfig)
}

func TestByocClusterWaitsForTheAgent(t *testing.T) {
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
	config := map[string]interface{}{"environment_id": "fresh", "storage_pool": "payments", "cluster_name": "orders"}
	state := mustApply(t, h, "ybm_aeon_pg_byoc_cluster", tftypes.Value{}, config)
	if attrString(t, state, "state") != "ready" {
		t.Errorf("cluster = %v", state)
	}

	// An environment whose agent will never act fails at once, for a create or an update.
	if _, diags := h.apply("ybm_aeon_pg_byoc_cluster", tftypes.Value{}, map[string]interface{}{
		"environment_id": "paused", "storage_pool": "payments", "cluster_name": "orders",
	}); !strings.Contains(diagText(diags), "SUSPENDED") {
		t.Errorf("a cluster was created in a suspended environment: %s", diagText(diags))
	}
	h.fake.setByocState("fresh", "DRAINING")
	if _, diags := h.apply("ybm_aeon_pg_byoc_cluster", state, withAttrs(config, map[string]interface{}{
		"branch_pg_settings": map[string]string{"work_mem": "64MB"},
	})); !strings.Contains(diagText(diags), "DRAINING") {
		t.Errorf("a cluster was updated in a draining environment: %s", diagText(diags))
	}
}
