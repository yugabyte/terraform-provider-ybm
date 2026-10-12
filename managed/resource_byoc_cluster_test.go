/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package managed

import (
	"context"
	"encoding/json"
	"io"
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

func TestWithByocClusterAddsPoolOnceAndKeepsOtherResources(t *testing.T) {
	config := ampClusterConfig{PgVersion: 18, Engine: "postgres", Password: "pw123456", Cpu: "2", Memory: "16Gi", ConnectivityMode: "cluster_ip"}
	foreign := ampResource{Kind: ampKindCluster, Name: "other-pool/legacy", Spec: json.RawMessage(`{"pg_version":16}`), Generation: 4}

	desired, err := withByocCluster([]ampResource{foreign}, "p", "a", config, true)
	if err != nil {
		t.Fatal(err)
	}
	desired, err = withByocCluster(desired, "p", "b", config, true)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(desiredNames(desired), ",")
	want := "cluster:other-pool/legacy,storage-pool:p,cluster:p/a,endpoint:p/a/main/primary,cluster:p/b,endpoint:p/b/main/primary"
	if got != want {
		t.Errorf("desired = %s", got)
	}
	if string(desired[0].Spec) != `{"pg_version":16}` || desired[0].Generation != 4 {
		t.Errorf("foreign resource changed: %+v", desired[0])
	}

	if _, err := withByocCluster(desired, "p", "a", config, true); err == nil {
		t.Error("created a cluster twice")
	}
	if _, err := withByocCluster(desired, "p", "missing", config, false); err == nil {
		t.Error("updated a cluster that does not exist")
	}

	config.Cpu, config.Memory = "4", "32Gi"
	updated, err := withByocCluster(desired, "p", "a", config, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(updated) != len(desired) {
		t.Fatalf("update changed the resource count: %v", desiredNames(updated))
	}
	ep, _ := findAmpDesired(updated, ampKindEndpoint, "p/a/main/primary")
	if !strings.Contains(string(ep.Spec), `"cpu":"4","memory":"32Gi"`) || !strings.Contains(string(ep.Spec), `"password":"pw123456"`) {
		t.Errorf("endpoint spec = %s", ep.Spec)
	}
}

func TestWithoutByocClusterDropsDescendantsAndUnusedPool(t *testing.T) {
	desired := []ampResource{
		{Kind: ampKindStoragePool, Name: "p"},
		{Kind: ampKindCluster, Name: "p/a"},
		{Kind: ampKindBranch, Name: "p/a/dev"},
		{Kind: ampKindEndpoint, Name: "p/a/main/primary"},
		{Kind: ampKindCluster, Name: "p/ab"},
		{Kind: ampKindEndpoint, Name: "p/ab/main/primary"},
		{Kind: ampKindCluster, Name: "q/x"},
	}

	desired = withoutByocCluster(desired, "p", "a")
	if got := strings.Join(desiredNames(desired), ","); got != "storage-pool:p,cluster:p/ab,endpoint:p/ab/main/primary,cluster:q/x" {
		t.Errorf("after removing a: %s", got)
	}
	desired = withoutByocCluster(desired, "p", "ab")
	if got := strings.Join(desiredNames(desired), ","); got != "cluster:q/x" {
		t.Errorf("after removing ab: %s", got)
	}
}

// fakeByocEnvironment stores desired state the way YCP does, replacing it on every POST.
type fakeByocEnvironment struct {
	mu      sync.Mutex
	desired []ampResource
	posts   []map[string]interface{}
}

func (f *fakeByocEnvironment) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Method == http.MethodPost {
		b, _ := io.ReadAll(r.Body)
		var body map[string]interface{}
		_ = json.Unmarshal(b, &body)
		f.posts = append(f.posts, body)
		var parsed struct {
			Resources []ampResource `json:"resources"`
		}
		_ = json.Unmarshal(b, &parsed)
		f.desired = parsed.Resources
		w.WriteHeader(http.StatusAccepted)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"data": map[string]interface{}{"resources": f.desired}})
}

func TestUpdateByocDesiredSerializesConcurrentWriters(t *testing.T) {
	fake := &fakeByocEnvironment{}
	client := newTestYcpClient(t, fake.handle)
	config := ampClusterConfig{PgVersion: 18, Engine: "postgres", Password: "pw123456", Cpu: "2", Memory: "16Gi", ConnectivityMode: "cluster_ip"}

	var wg sync.WaitGroup
	names := []string{"a", "b", "c", "d", "e", "f", "g", "h"}
	for _, name := range names {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			err := updateByocDesired(context.Background(), client, testAccountID, "env", func(d []ampResource) ([]ampResource, error) {
				return withByocCluster(d, "p", name, config, true)
			})
			if err != nil {
				t.Error(err)
			}
		}(name)
	}
	wg.Wait()

	for _, name := range names {
		if _, ok := findAmpDesired(fake.desired, ampKindCluster, "p/"+name); !ok {
			t.Errorf("cluster %s was lost to a concurrent write", name)
		}
	}

	// Removing every cluster leaves an empty desired state, which YCP accepts only when asked to.
	for _, name := range names {
		if err := updateByocDesired(context.Background(), client, testAccountID, "env", func(d []ampResource) ([]ampResource, error) {
			return withoutByocCluster(d, "p", name), nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	last := fake.posts[len(fake.posts)-1]
	if len(fake.desired) != 0 || last["allow_empty"] != true {
		t.Errorf("last post = %v", last)
	}
}
