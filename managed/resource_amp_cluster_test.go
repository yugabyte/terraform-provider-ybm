/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package managed

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	openapiclient "github.com/yugabyte/yugabytedb-managed-go-client-internal"
)

const testAccountID = "6c1f3c2e-1d2a-4f7b-9a0e-1b2c3d4e5f60"

func newTestYcpClient(t *testing.T, handler http.HandlerFunc) *openapiclient.APIClient {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	u, _ := url.Parse(server.URL)
	cfg := openapiclient.NewConfiguration()
	cfg.Host = u.Host
	cfg.Scheme = "http"
	cfg.UserAgent = "terraform-provider-ybm/test"
	client := openapiclient.NewAPIClient(cfg)
	client.GetConfig().AddDefaultHeader("Authorization", "Bearer token")
	return client
}

func TestYcpDoSendsAuthenticatedRequestAndUnwrapsData(t *testing.T) {
	var gotPath, gotQuery, gotAuth, gotAgent, gotMethod string
	var gotBody map[string]interface{}
	client := newTestYcpClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery, gotMethod = r.URL.Path, r.URL.RawQuery, r.Method
		gotAuth, gotAgent = r.Header.Get("Authorization"), r.Header.Get("User-Agent")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.Write([]byte(`{"data":{"environments":[]}}`))
	})

	data, err := ycpDo(context.Background(), client, http.MethodPost, testAccountID, "/clusters",
		url.Values{"kind": {"cluster"}}, map[string]string{"k": "v"})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/public/v1/accounts/"+testAccountID+"/ycp/clusters" {
		t.Errorf("path = %s", gotPath)
	}
	if gotMethod != http.MethodPost || gotQuery != "kind=cluster" {
		t.Errorf("method/query = %s %s", gotMethod, gotQuery)
	}
	if gotAuth != "Bearer token" || gotAgent != "terraform-provider-ybm/test" {
		t.Errorf("headers = %q %q", gotAuth, gotAgent)
	}
	if gotBody["k"] != "v" {
		t.Errorf("body = %v", gotBody)
	}
	if string(data) != `{"environments":[]}` {
		t.Errorf("data = %s", data)
	}
}

func TestYcpDoReportsApiErrorDetail(t *testing.T) {
	client := newTestYcpClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":{"status":404,"detail":"Environment not found"}}`))
	})
	_, err := ycpDo(context.Background(), client, http.MethodGet, testAccountID, "/environments/x", nil, nil)
	if !isYcpNotFound(err) || !strings.Contains(err.Error(), "Environment not found") {
		t.Fatalf("err = %v", err)
	}
}

func TestYcpDoAcceptsEmptyBody(t *testing.T) {
	client := newTestYcpClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})
	data, err := ycpDo(context.Background(), client, http.MethodPost, testAccountID, "/environments/x/desired", nil, map[string]int{})
	if err != nil || data != nil {
		t.Fatalf("data = %s, err = %v", data, err)
	}
}

func TestAmpDefaultMemory(t *testing.T) {
	for cpu, want := range map[string]string{"2": "16384Mi", "0.5": "4096Mi", "500m": "4096Mi", "4": "32768Mi"} {
		got, err := ampDefaultMemory(cpu)
		if err != nil || got != want {
			t.Errorf("ampDefaultMemory(%q) = %q, %v; want %q", cpu, got, err, want)
		}
	}
	for _, cpu := range []string{"", "two", "-1", "xm"} {
		if _, err := ampDefaultMemory(cpu); err == nil {
			t.Errorf("ampDefaultMemory(%q) succeeded", cpu)
		}
	}
}

func TestAmpEndpointSpecLeavesUnsetValuesToTheService(t *testing.T) {
	nullMap := types.Map{ElemType: types.StringType, Null: true}
	plan := AmpEndpoint{Credentials: &AmpCredentials{Password: types.String{Value: "S3cretPassw0rd"}}, Cpu: types.String{Unknown: true},
		Memory: types.String{Unknown: true}, ConnectivityMode: types.String{Unknown: true}, PgSettings: nullMap}
	c, err := newAmpEndpointConfig(ampRoleReadWrite, plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(c.spec(true))
	if string(b) != `{"role":"read_write","password":"S3cretPassw0rd"}` {
		t.Errorf("create spec = %s", b)
	}

	// On an update, a new cpu brings memory with it, and unmanaged collections are left out.
	prior := plan
	prior.Cpu, prior.Memory = types.String{Value: "2"}, types.String{Value: "16Gi"}
	plan.Cpu, plan.ConnectivityMode = types.String{Value: "4"}, types.String{Value: "cluster_ip"}
	c, err = newAmpEndpointConfig(ampRoleReadWrite, plan, &prior)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := json.Marshal(c.spec(false)); string(b) != `{"resources":{"cpu":"4","memory":"32768Mi"}}` {
		t.Errorf("update spec = %s", b)
	}

	// Removing managed collections from the configuration clears them, with the mode connectivity
	// already has.
	prior.AllowedCidrs = []types.String{{Value: "10.0.0.0/8"}}
	prior.PgSettings = types.Map{ElemType: types.StringType, Elems: map[string]attr.Value{"work_mem": types.String{Value: "64MB"}}}
	c, _ = newAmpEndpointConfig(ampRoleReadWrite, plan, &prior)
	if b, _ := json.Marshal(c.spec(false)); !strings.Contains(string(b), `"connectivity":{"mode":"cluster_ip","allowed_cidrs":[]},"pg_settings":{}`) {
		t.Errorf("update spec = %s", b)
	}
}

func TestAmpClusterConfigLeavesUnsetValuesToTheService(t *testing.T) {
	nullMap := types.Map{ElemType: types.StringType, Null: true}
	c := newAmpClusterConfig(types.Int64{Unknown: true}, types.String{Unknown: true}, nil, nullMap, nil, nullMap)
	if b, _ := json.Marshal(c.clusterSpec()); string(b) != `{}` {
		t.Errorf("cluster spec = %s", b)
	}
	if b, _ := json.Marshal(c.mainBranchSpec(true)); string(b) != `{}` {
		t.Errorf("main branch spec = %s", b)
	}
	c = newAmpClusterConfig(types.Int64{Value: 17}, types.String{Value: "postgres"}, nil, nullMap, []types.String{{Value: "vector"}}, nullMap)
	if b, _ := json.Marshal(c.mainBranchSpec(false)); string(b) != `{}` {
		t.Errorf("a new main branch got extensions: %s", b)
	}
	if b, _ := json.Marshal(c.mainBranchSpec(true)); string(b) != `{"extensions":["vector"]}` {
		t.Errorf("main branch spec = %s", b)
	}
}

func endpointObserved(state, address, applied string) ampObserved {
	attrs := map[string]string{"address": address}
	if applied != "" {
		attrs["applied_generation"] = applied
	}
	return ampObserved{Kind: ampKindEndpoint, ID: "endpoint/" + "p/c/main/primary", State: state, Attrs: attrs}
}

func TestAmpResourceProgress(t *testing.T) {
	endpoint := "p/c/main/primary"
	desired := []ampResource{{Kind: ampKindEndpoint, Name: endpoint, Generation: 2}, {Kind: ampKindBranch, Name: "p/c/dev", Generation: 3}}
	cases := []struct {
		name     string
		kind     string
		path     string
		observed []ampObserved
		done     bool
		failed   bool
	}{
		{"not observed", ampKindEndpoint, endpoint, nil, false, false},
		{"provisioning", ampKindEndpoint, endpoint, []ampObserved{endpointObserved("provisioning", "", "")}, false, false},
		{"ready without address", ampKindEndpoint, endpoint, []ampObserved{endpointObserved("ready", "", "2")}, false, false},
		{"stale generation", ampKindEndpoint, endpoint, []ampObserved{endpointObserved("ready", "h:5432", "1")}, false, false},
		{"converged", ampKindEndpoint, endpoint, []ampObserved{endpointObserved("ready", "h:5432", "2")}, true, false},
		{"no applied generation yet", ampKindEndpoint, endpoint, []ampObserved{endpointObserved("ready", "h:5432", "")}, true, false},
		{"rejected endpoint", ampKindEndpoint, endpoint, []ampObserved{endpointObserved("rejected", "", "")}, false, true},
		{"cluster drift", ampKindCluster, "p/c", []ampObserved{{Kind: ampKindCluster, ID: "cluster/p/c", State: "drift"}}, false, true},
		{"branch needs no address", ampKindBranch, "p/c/dev", []ampObserved{{Kind: ampKindBranch, ID: "branch/p/c/dev", State: "ready", Attrs: map[string]string{"applied_generation": "3"}}}, true, false},
		{"branch stale", ampKindBranch, "p/c/dev", []ampObserved{{Kind: ampKindBranch, ID: "branch/p/c/dev", State: "ready", Attrs: map[string]string{"applied_generation": "2"}}}, false, false},
	}
	for _, tc := range cases {
		done, waitingOn, err := ampResourceProgress(desired, tc.observed, tc.kind, tc.path)
		if done != tc.done || (err != nil) != tc.failed {
			t.Errorf("%s: done = %v, err = %v", tc.name, done, err)
		}
		if !done && err == nil && waitingOn == "" {
			t.Errorf("%s: no reason given for waiting", tc.name)
		}
	}
}

func TestReadAmpCluster(t *testing.T) {
	desired := []ampResource{
		{Kind: ampKindCluster, Name: "p/c", Spec: json.RawMessage(`{"extensions":["vector"]}`)},
		{Kind: ampKindBranch, Name: "p/c/main", Spec: json.RawMessage(`{"pg_settings":{"work_mem":"32MB","jit":"off"},"extensions":["postgis"]}`)},
	}
	observed := []ampObserved{
		{Kind: ampKindCluster, ID: "cluster/p/c", State: "ready", Attrs: map[string]string{"id": "amp-uuid", "pg_version": "18", "engine": "postgres"}},
	}
	s, found, err := readAmpCluster(desired, observed, "p", "c")
	if err != nil || !found {
		t.Fatalf("found = %v, err = %v", found, err)
	}
	// What the spec leaves out is read from what AMP applied.
	want := ampClusterStatus{
		Config: ampClusterConfig{PgVersion: 18, Engine: "postgres", Extensions: []string{"vector"},
			BranchPgSettings: &map[string]string{"work_mem": "32MB", "jit": "off"}, BranchExtensions: []string{"postgis"}},
		ClusterID: "amp-uuid",
		State:     "ready",
	}
	if !reflect.DeepEqual(s, want) {
		t.Errorf("status = %+v", s)
	}
	if _, found, _ := readAmpCluster(desired, observed, "p", "other"); found {
		t.Error("found a cluster that is not declared")
	}
}

func TestReadAmpEndpoint(t *testing.T) {
	desired := []ampResource{{Kind: ampKindEndpoint, Name: "p/c/main/primary", Spec: json.RawMessage(
		`{"role":"read_write","password":"pw123456","connectivity":{"allowed_cidrs":["10.0.0.0/16"]},"pg_settings":{"work_mem":"64MB"}}`)}}
	observed := []ampObserved{{Kind: ampKindEndpoint, ID: "endpoint/p/c/main/primary", State: "ready", Attrs: map[string]string{
		"id": "ep-uuid", "address": "db.example.com:5432", "tls_ca_crt": "CA", "cpu": "2", "memory": "16Gi", "connectivity_mode": "cluster_ip"}}}
	s, found, err := readAmpEndpoint(desired, observed, "p/c/main/primary")
	if err != nil || !found {
		t.Fatalf("found = %v, err = %v", found, err)
	}
	want := ampEndpointStatus{
		Config: ampEndpointConfig{Role: "read_write", Cpu: "2", Memory: "16Gi", ConnectivityMode: "cluster_ip",
			AllowedCidrs: &[]string{"10.0.0.0/16"}, PgSettings: &map[string]string{"work_mem": "64MB"}},
		EndpointID: "ep-uuid", State: "ready", Host: "db.example.com", Port: "5432", TlsCaCertificate: "CA",
	}
	if !reflect.DeepEqual(s, want) {
		t.Errorf("status = %+v", s)
	}
}

func TestCollectionsReadBackOnlyWhenManaged(t *testing.T) {
	nullMap := types.Map{ElemType: types.StringType, Null: true}
	emptyMap := types.Map{ElemType: types.StringType, Elems: map[string]attr.Value{}}
	if got := mapToTf(nil, emptyMap); got.Null || len(got.Elems) != 0 {
		t.Errorf("an empty map read back as %v", got)
	}
	// Settings Terraform does not manage, such as those a fork inherits, stay out of state.
	if got := mapToTf(map[string]string{"a": "b"}, nullMap); !got.Null {
		t.Errorf("an unmanaged map read back as %v", got)
	}
	if got := stringsToTf(nil, []types.String{}); got == nil || len(got) != 0 {
		t.Errorf("an empty set read back as %v", got)
	}
	if got := stringsToTf([]string{"a"}, nil); got != nil {
		t.Errorf("an unmanaged set read back as %v", got)
	}
	if got := managedMap(nullMap, nullMap); got != nil {
		t.Errorf("an unmanaged map is sent as %v", *got)
	}
	if got := managedMap(nullMap, emptyMap); got == nil || len(*got) != 0 {
		t.Errorf("a removed map is sent as %v", got)
	}
}

func TestByocEnvironmentID(t *testing.T) {
	for name, want := range map[string]string{"acme-prod": "acme-prod", " Acme Prod ": "acme-prod", "acme_prod/eu": "acme-prod-eu"} {
		if got := byocEnvironmentID(name); got != want {
			t.Errorf("byocEnvironmentID(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestIsYcpTransient(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want bool
	}{
		{&ycpError{StatusCode: 500}, true},
		{&ycpError{StatusCode: 503}, true},
		{&ycpError{StatusCode: 429}, true},
		{&ycpError{StatusCode: 401}, false},
		{&ycpError{StatusCode: 403}, false},
		{&ycpError{StatusCode: 404}, false},
		{&url.Error{Op: "Get", URL: "http://x", Err: io.EOF}, true},
		{fmt.Errorf("unable to parse the cluster list: bad json"), false},
	} {
		if got := isYcpTransient(tc.err); got != tc.want {
			t.Errorf("isYcpTransient(%v) = %v", tc.err, got)
		}
	}
}

func TestWaitForAmpFailsFastOnAPermanentError(t *testing.T) {
	ampPollInterval = time.Millisecond
	t.Cleanup(func() { ampPollInterval = 10 * time.Second })
	calls := 0
	client := newTestYcpClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":{"status":403,"detail":"Forbidden"}}`))
	})
	err := waitForAmp(context.Background(), newAmpScope(client, testAccountID, "", ""), ampClusterTargets(testAccountID, "c"))
	if err == nil || !strings.Contains(err.Error(), "Forbidden") || calls != 1 {
		t.Errorf("calls = %d, err = %v", calls, err)
	}
}

func TestWaitForAmpReportsWhatItWaitedOn(t *testing.T) {
	ampPollInterval = time.Millisecond
	ampOperationTimeout = 20 * time.Millisecond
	t.Cleanup(func() { ampPollInterval, ampOperationTimeout = 10*time.Second, 60*time.Minute })
	fake := newFakeYcp()
	fake.addByocEnv("env", "READY")
	fake.addByocPool("env", "p")
	fake.byoc["env"].desired = append(fake.byoc["env"].desired, ampResource{Kind: ampKindCluster, Name: "p/c"})
	client := newTestYcpClient(t, func(w http.ResponseWriter, r *http.Request) {
		// The agent never reports anything.
		if strings.HasSuffix(r.URL.Path, "/environments/env") {
			w.Write([]byte(`{"data":{"id":"env","state":"READY","resources":[]}}`))
			return
		}
		fake.ServeHTTP(w, r)
	})
	err := waitForAmp(context.Background(), newAmpScope(client, testAccountID, "env", "p"), []ampTarget{{Kind: ampKindCluster, Name: "p/c"}})
	if err == nil || !strings.Contains(err.Error(), "timed out") || !strings.Contains(err.Error(), "cluster p/c has not been reported yet") {
		t.Errorf("err = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = waitForAmp(ctx, newAmpScope(client, testAccountID, "env", "p"), []ampTarget{{Kind: ampKindCluster, Name: "p/c"}})
	if err == nil || strings.Contains(err.Error(), "timed out") {
		t.Errorf("a cancelled wait reported %v", err)
	}
}
