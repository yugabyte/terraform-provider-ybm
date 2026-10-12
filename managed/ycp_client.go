/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package managed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	openapiclient "github.com/yugabyte/yugabytedb-managed-go-client-internal"
)

// The YCP routes are passthroughs whose bodies the generated client types as
// map[string]map[string]interface{}, which can neither carry the `regions` array nor decode a list
// `data`. They are called here over plain HTTP, reusing the generated client's host, scheme,
// headers and HTTP client so auth and user agent stay identical.

const (
	ampKindStoragePool = "storage-pool"
	ampKindCluster     = "cluster"
	ampKindBranch      = "branch"
	ampKindEndpoint    = "endpoint"

	// Every AMP cluster gets a default branch named main; the provider puts its one read-write
	// endpoint there under a fixed name.
	ampDefaultBranch   = "main"
	ampPrimaryEndpoint = "primary"

	// Connection details AMP endpoints always use.
	ampUsername = "cloud_admin"
	ampDatabase = "postgres"

	ampStateReady = "ready"
)

// Observed states the reconcile agent reports for a resource it cannot or will not converge.
var ampTerminalStates = map[string]bool{
	"rejected":   true,
	"error":      true,
	"drift":      true,
	"blocked":    true,
	"unreadable": true,
}

type ycpError struct {
	StatusCode int
	Message    string
}

func (e *ycpError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Message)
}

func isYcpNotFound(err error) bool {
	if e, ok := err.(*ycpError); ok {
		return e.StatusCode == http.StatusNotFound
	}
	return false
}

// ycpDo calls a YCP route below /public/v1/accounts/{accountId}/ycp and returns the `data` member
// of the response, or nil when the response has no body.
func ycpDo(ctx context.Context, apiClient *openapiclient.APIClient, method string, accountId string, subPath string, query url.Values, body interface{}) (json.RawMessage, error) {
	cfg := apiClient.GetConfig()
	base, err := cfg.ServerURLWithContext(ctx, "YcpApiService.ListAmpManagedClusters")
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(base + "/public/v1/accounts/" + url.PathEscape(accountId) + "/ycp" + subPath)
	if err != nil {
		return nil, err
	}
	// Mirrors the generated client, which lets the configured host and scheme win.
	if cfg.Host != "" {
		u.Host = cfg.Host
	}
	if cfg.Scheme != "" {
		u.Scheme = cfg.Scheme
	}
	if query != nil {
		u.RawQuery = query.Encode()
	}

	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reqBody = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), reqBody)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	for k, v := range cfg.DefaultHeader {
		req.Header.Set(k, v)
	}
	req.Header.Set("User-Agent", cfg.UserAgent)

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(respBody))
		if apiErr := getAPIError(respBody); apiErr != nil {
			if d, ok := apiErr.GetErrorOk(); ok && d.GetDetail() != "" {
				msg = d.GetDetail()
			}
		}
		return nil, &ycpError{StatusCode: resp.StatusCode, Message: msg}
	}
	if len(bytes.TrimSpace(respBody)) == 0 {
		return nil, nil
	}
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(respBody, &envelope); err != nil {
		return nil, fmt.Errorf("unable to parse the YCP response: %v", err)
	}
	return envelope.Data, nil
}

// ampResource is a desired-state entry. Spec stays raw so entries the provider does not own pass
// through a read-modify-write unchanged.
type ampResource struct {
	Kind       string          `json:"kind"`
	Name       string          `json:"name"`
	Spec       json.RawMessage `json:"spec,omitempty"`
	Generation int64           `json:"generation,omitempty"`
}

// ampObserved is the state the reconcile agent reports for one resource.
type ampObserved struct {
	Kind  string            `json:"kind"`
	ID    string            `json:"id"` // "<kind>/<full name>"
	State string            `json:"state"`
	Error string            `json:"error"`
	Attrs map[string]string `json:"attrs"`
}

type ampPatchItem struct {
	Kind   string      `json:"kind"`
	Name   string      `json:"name"`
	Spec   interface{} `json:"spec,omitempty"`
	Create bool        `json:"create,omitempty"`
	Update bool        `json:"update,omitempty"`
	Delete bool        `json:"delete,omitempty"`
}

type ampRegionPatch struct {
	Region    string         `json:"region"`
	Resources []ampPatchItem `json:"resources"`
}

type ampManagedPatch struct {
	Regions []ampRegionPatch `json:"regions"`
}

type ampEnvironmentResources struct {
	EnvironmentID string        `json:"environment_id"`
	Region        string        `json:"region"`
	Desired       []ampResource `json:"desired"`
	Observed      []ampObserved `json:"observed"`
}

// AMP spec fragments the provider writes.
type ampEndpointResources struct {
	CPU    string `json:"cpu,omitempty"`
	Memory string `json:"memory,omitempty"`
}

type ampEndpointConnectivity struct {
	Mode         string   `json:"mode,omitempty"`
	AllowedCidrs []string `json:"allowed_cidrs"`
}

type ampClusterSpec struct {
	PgVersion  int64    `json:"pg_version,omitempty"`
	Engine     string   `json:"engine,omitempty"`
	Extensions []string `json:"extensions,omitempty"`
}

type ampEndpointSpec struct {
	Role         string                   `json:"role,omitempty"`
	Password     string                   `json:"password,omitempty"`
	Resources    *ampEndpointResources    `json:"resources,omitempty"`
	Connectivity *ampEndpointConnectivity `json:"connectivity,omitempty"`
}

func ampClusterName(pool, cluster string) string {
	return pool + "/" + cluster
}

func ampEndpointName(pool, cluster string) string {
	return strings.Join([]string{pool, cluster, ampDefaultBranch, ampPrimaryEndpoint}, "/")
}

func findAmpDesired(resources []ampResource, kind, name string) (ampResource, bool) {
	for _, r := range resources {
		if r.Kind == kind && r.Name == name {
			return r, true
		}
	}
	return ampResource{}, false
}

func findAmpObserved(observed []ampObserved, kind, name string) (ampObserved, bool) {
	for _, o := range observed {
		if o.Kind == kind && o.ID == kind+"/"+name {
			return o, true
		}
	}
	return ampObserved{}, false
}

// ampEndpointProgress reports whether the cluster's primary endpoint has converged on its desired
// generation, or the error that stops it ever converging.
func ampEndpointProgress(desired []ampResource, observed []ampObserved, pool, cluster string) (bool, error) {
	endpointName := ampEndpointName(pool, cluster)
	for _, kn := range [][2]string{{ampKindCluster, ampClusterName(pool, cluster)}, {ampKindEndpoint, endpointName}} {
		if o, ok := findAmpObserved(observed, kn[0], kn[1]); ok && ampTerminalStates[o.State] {
			return false, fmt.Errorf("%s %s is %s: %s", kn[0], kn[1], o.State, o.Error)
		}
	}
	o, ok := findAmpObserved(observed, ampKindEndpoint, endpointName)
	if !ok || o.State != ampStateReady || o.Attrs["address"] == "" {
		return false, nil
	}
	// generation is omitted while 0, and applied_generation only appears once the controller has
	// echoed one back, so the comparison applies only when both are present.
	if d, ok := findAmpDesired(desired, ampKindEndpoint, endpointName); ok && d.Generation > 0 {
		if applied, ok := o.Attrs["applied_generation"]; ok && applied != fmt.Sprint(d.Generation) {
			return false, nil
		}
	}
	return true, nil
}

// splitHostPort splits an AMP endpoint address of the form host:port.
func splitHostPort(address string) (string, string) {
	i := strings.LastIndex(address, ":")
	if i < 0 {
		return address, ""
	}
	return address[:i], address[i+1:]
}

// Desired state for a BYOC environment is replaced as a whole, so concurrent writes from resources
// in the same apply must be serialized per environment or one would drop the other's clusters.
var byocEnvironmentLocks sync.Map

func lockByocEnvironment(environmentId string) func() {
	m, _ := byocEnvironmentLocks.LoadOrStore(environmentId, &sync.Mutex{})
	mu := m.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}
