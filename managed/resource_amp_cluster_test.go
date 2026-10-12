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
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

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

func TestNewAmpClusterConfigAppliesDefaults(t *testing.T) {
	c, err := newAmpClusterConfig(types.Int64{Unknown: true}, types.String{Unknown: true}, nil,
		types.String{Value: "S3cretPassw0rd"}, types.String{Unknown: true}, types.String{Unknown: true},
		types.String{Null: true}, nil, types.Map{ElemType: types.StringType, Null: true})
	if err != nil {
		t.Fatal(err)
	}
	want := ampClusterConfig{PgVersion: 18, Engine: "postgres", Endpoint: ampEndpointConfig{
		Role: "read_write", Password: "S3cretPassw0rd", Cpu: "2", Memory: "16384Mi", ConnectivityMode: "cluster_ip", PgSettings: map[string]string{},
	}}
	if !reflect.DeepEqual(c, want) {
		t.Errorf("config = %+v", c)
	}

	// An update carries only what AMP can change in place, with nested maps sent whole.
	b, _ := json.Marshal(c.Endpoint.spec(false))
	if string(b) != `{"resources":{"cpu":"2","memory":"16384Mi"},"connectivity":{"mode":"cluster_ip","allowed_cidrs":[]},"pg_settings":{}}` {
		t.Errorf("update spec = %s", b)
	}
	b, _ = json.Marshal(c.Endpoint.spec(true))
	if !strings.Contains(string(b), `"role":"read_write","password":"S3cretPassw0rd"`) {
		t.Errorf("create spec = %s", b)
	}
}

func endpointObserved(state, address, applied string) ampObserved {
	attrs := map[string]string{"address": address}
	if applied != "" {
		attrs["applied_generation"] = applied
	}
	return ampObserved{Kind: ampKindEndpoint, ID: "endpoint/" + ampEndpointName("p", "c"), State: state, Attrs: attrs}
}

func TestAmpResourceProgress(t *testing.T) {
	endpoint := ampEndpointName("p", "c")
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
		done, err := ampResourceProgress(desired, tc.observed, tc.kind, tc.path)
		if done != tc.done || (err != nil) != tc.failed {
			t.Errorf("%s: done = %v, err = %v", tc.name, done, err)
		}
	}
}

func TestReadAmpCluster(t *testing.T) {
	desired := []ampResource{
		{Kind: ampKindCluster, Name: "p/c", Spec: json.RawMessage(`{"pg_version":17,"extensions":["vector"]}`)},
		{Kind: ampKindEndpoint, Name: "p/c/main/primary", Generation: 1, Spec: json.RawMessage(
			`{"role":"read_write","password":"pw123456","resources":{"cpu":"2","memory":"16Gi"},"connectivity":{"mode":"nlb_internal","allowed_cidrs":["10.0.0.0/16"]},"pg_settings":{"work_mem":"64MB"}}`)},
	}
	observed := []ampObserved{
		{Kind: ampKindCluster, ID: "cluster/p/c", State: "ready", Attrs: map[string]string{"id": "amp-uuid"}},
		{Kind: ampKindEndpoint, ID: "endpoint/p/c/main/primary", State: "ready", Attrs: map[string]string{"id": "ep-uuid", "address": "db.example.com:5432", "tls_ca_crt": "CA"}},
	}
	s, found, err := readAmpCluster(desired, observed, "p", "c")
	if err != nil || !found {
		t.Fatalf("found = %v, err = %v", found, err)
	}
	endpointConfig := ampEndpointConfig{Role: "read_write", Password: "pw123456", Cpu: "2", Memory: "16Gi",
		ConnectivityMode: "nlb_internal", AllowedCidrs: []string{"10.0.0.0/16"}, PgSettings: map[string]string{"work_mem": "64MB"}}
	want := ampClusterStatus{
		Config:    ampClusterConfig{PgVersion: 17, Engine: "postgres", Extensions: []string{"vector"}, Endpoint: endpointConfig},
		ClusterID: "amp-uuid",
		Endpoint: ampEndpointStatus{Config: endpointConfig, EndpointID: "ep-uuid", State: "ready",
			Host: "db.example.com", Port: "5432", TlsCaCertificate: "CA"},
	}
	if !reflect.DeepEqual(s, want) {
		t.Errorf("status = %+v", s)
	}

	if _, found, _ := readAmpCluster(desired, observed, "p", "other"); found {
		t.Error("found a cluster that is not declared")
	}
}
