/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package managed

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
)

func desiredNames(resources []ampResource) []string {
	names := make([]string, 0, len(resources))
	for _, r := range resources {
		names = append(names, r.Kind+":"+r.Name)
	}
	return names
}

func TestApplyAmpIntentMatchesManagedPatchSemantics(t *testing.T) {
	foreign := ampResource{Kind: ampKindCluster, Name: "other-pool/legacy", Spec: json.RawMessage(`{"pg_version":16}`), Generation: 4}
	desired := []ampResource{foreign}

	create := func(kind, name string, spec interface{}) {
		t.Helper()
		var err error
		desired, err = applyAmpIntent(desired, ampIntentCreate, []ampPatchItem{{Kind: kind, Name: name, Spec: spec}})
		if err != nil {
			t.Fatal(err)
		}
	}
	create(ampKindCluster, "p/a", map[string]int{"pg_version": 18})
	create(ampKindEndpoint, "p/a/main/primary", map[string]interface{}{"role": "read_write", "resources": map[string]string{"cpu": "2", "memory": "16Gi"}})
	create(ampKindBranch, "p/a/dev", map[string]interface{}{})
	create(ampKindEndpoint, "p/a/dev/rw", map[string]interface{}{})
	create(ampKindCluster, "p/ab", map[string]int{"pg_version": 18})

	got := strings.Join(desiredNames(desired), ",")
	want := "cluster:other-pool/legacy,cluster:p/a,endpoint:p/a/main/primary,branch:p/a/dev,endpoint:p/a/dev/rw,cluster:p/ab"
	if got != want {
		t.Errorf("desired = %s", got)
	}
	if string(desired[0].Spec) != `{"pg_version":16}` || desired[0].Generation != 4 {
		t.Errorf("foreign resource changed: %+v", desired[0])
	}

	// A create of an existing resource is a conflict, an update of a missing one is not found.
	_, err := applyAmpIntent(desired, ampIntentCreate, []ampPatchItem{{Kind: ampKindCluster, Name: "p/a", Spec: map[string]int{}}})
	if e, ok := err.(*ycpError); !ok || e.StatusCode != http.StatusConflict {
		t.Errorf("duplicate create: %v", err)
	}
	_, err = applyAmpIntent(desired, ampIntentUpdate, []ampPatchItem{{Kind: ampKindBranch, Name: "p/a/missing", Spec: map[string]int{}}})
	if !isYcpNotFound(err) {
		t.Errorf("update of a missing resource: %v", err)
	}

	// An update merges top-level keys and replaces nested maps whole.
	desired, err = applyAmpIntent(desired, ampIntentUpdate, []ampPatchItem{{Kind: ampKindEndpoint, Name: "p/a/main/primary",
		Spec: map[string]interface{}{"resources": map[string]string{"cpu": "4"}}}})
	if err != nil {
		t.Fatal(err)
	}
	ep, _ := findAmpDesired(desired, ampKindEndpoint, "p/a/main/primary")
	if string(ep.Spec) != `{"resources":{"cpu":"4"},"role":"read_write"}` {
		t.Errorf("merged spec = %s", ep.Spec)
	}

	// A delete cascades by path and is a no-op for a missing resource.
	desired, _ = applyAmpIntent(desired, ampIntentDelete, []ampPatchItem{{Kind: ampKindBranch, Name: "p/a/dev"}})
	desired, _ = applyAmpIntent(desired, ampIntentDelete, []ampPatchItem{{Kind: ampKindBranch, Name: "p/a/dev"}})
	if got := strings.Join(desiredNames(desired), ","); got != "cluster:other-pool/legacy,cluster:p/a,endpoint:p/a/main/primary,cluster:p/ab" {
		t.Errorf("after deleting the branch: %s", got)
	}
	desired, _ = applyAmpIntent(desired, ampIntentDelete, []ampPatchItem{{Kind: ampKindCluster, Name: "p/a"}})
	desired, _ = applyAmpIntent(desired, ampIntentDelete, []ampPatchItem{{Kind: ampKindCluster, Name: "p/ab"}})
	if got := strings.Join(desiredNames(desired), ","); got != "cluster:other-pool/legacy" {
		t.Errorf("after deleting both clusters: %s", got)
	}
}

func TestByocScopeApplySerializesConcurrentWriters(t *testing.T) {
	fake := newFakeYcp()
	fake.addByocEnv("env", "READY")
	fake.addByocPool("env", "pool")
	client := newTestYcpClient(t, fake.ServeHTTP)
	scope := newAmpScope(client, testAccountID, "env", "pool")

	var wg sync.WaitGroup
	names := []string{"a", "b", "c", "d", "e", "f", "g", "h"}
	for _, name := range names {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			items := []ampPatchItem{{Kind: ampKindCluster, Name: ampClusterName(scope.pool(), name), Spec: map[string]int{"pg_version": 18}}}
			if err := scope.apply(context.Background(), "", ampIntentCreate, items); err != nil {
				t.Error(err)
			}
		}(name)
	}
	wg.Wait()

	for _, name := range names {
		if _, ok := findAmpDesired(fake.byoc["env"].desired, ampKindCluster, ampClusterName(scope.pool(), name)); !ok {
			t.Errorf("cluster %s was lost to a concurrent write", name)
		}
	}
}

func TestByocScopeCreateRequiresTheStoragePool(t *testing.T) {
	fake := newFakeYcp()
	fake.addByocEnv("env", "READY")
	scope := newAmpScope(newTestYcpClient(t, fake.ServeHTTP), testAccountID, "env", "payments")
	err := scope.apply(context.Background(), "", ampIntentCreate, []ampPatchItem{{Kind: ampKindCluster, Name: "payments/a", Spec: map[string]int{}}})
	if !isYcpNotFound(err) || !strings.Contains(err.Error(), "ybm_byoc_storage_pool") {
		t.Fatalf("err = %v", err)
	}
}

func TestAmpForksAndDependents(t *testing.T) {
	branch := func(name, parent string) ampResource {
		return ampResource{Kind: ampKindBranch, Name: name, Spec: mustMarshal(ampBranchSpec{ParentBranchID: parent})}
	}
	env := ampEnvironmentResources{
		Desired: []ampResource{
			{Kind: ampKindCluster, Name: "p/c"},
			branch("p/c/main", ""),
			{Kind: ampKindEndpoint, Name: "p/c/main/primary"},
			branch("p/c/dev", ""),
			{Kind: ampKindEndpoint, Name: "p/c/dev/rw"},
			branch("p/c/feature", fakeID("p/c/dev")),
			branch("p/c/hotfix", fakeID("p/c/feature")),
			branch("p/c/other", fakeID("p/c/main")),
			{Kind: ampKindCluster, Name: "p/cd"},
		},
	}
	for _, r := range env.Desired {
		env.Observed = append(env.Observed, ampObserved{Kind: r.Kind, ID: r.Kind + "/" + r.Name, Attrs: map[string]string{"id": fakeID(r.Name)}})
	}

	forks := ampForks(env, "p/c", "p/c/dev")
	if got := strings.Join(forks, ","); got != "p/c/feature,p/c/hotfix" {
		t.Errorf("forks = %s", got)
	}
	got := ampDependents(env, append([]string{"p/c/dev"}, forks...), map[string]bool{"branch p/c/dev": true})
	if strings.Join(got, ",") != "branch p/c/feature,branch p/c/hotfix,endpoint p/c/dev/rw" {
		t.Errorf("branch dependents = %v", got)
	}
	owned := map[string]bool{"cluster p/c": true, "branch p/c/main": true, "endpoint p/c/main/primary": true}
	got = ampDependents(env, []string{"p/c"}, owned)
	if strings.Join(got, ",") != "branch p/c/dev,branch p/c/feature,branch p/c/hotfix,branch p/c/other,endpoint p/c/dev/rw" {
		t.Errorf("cluster dependents = %v", got)
	}
}
