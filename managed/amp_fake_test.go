/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package managed

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// fakeYcp is a stateful stand-in for the YBM /ycp routes. Every declared resource is reported
// as converged on its latest generation, endpoints with an address, and every cluster with its
// default main branch whether or not it is declared.
type fakeYcp struct {
	mu       sync.Mutex
	managed  map[string][]ampResource // region -> desired
	byoc     map[string]*fakeByocEnv
	requests []string
	bodies   []map[string]interface{}
}

type fakeByocEnv struct {
	state   string
	region  string
	desired []ampResource
}

func newFakeYcp() *fakeYcp {
	return &fakeYcp{managed: map[string][]ampResource{}, byoc: map[string]*fakeByocEnv{}}
}

func (f *fakeYcp) addByocEnv(id, state string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byoc[id] = &fakeByocEnv{state: state, region: "us-east-1"}
}

func (f *fakeYcp) setByocState(id, state string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byoc[id].state = state
}

// addByocPool declares a storage pool the way the console does.
func (f *fakeYcp) addByocPool(env, pool string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byoc[env].desired = append(f.byoc[env].desired, ampResource{Kind: ampKindStoragePool, Name: pool,
		Spec: mustMarshal(ampStoragePoolSpec{OwnerUserID: testAccountID}), Generation: 1})
}

func fakeID(name string) string {
	return "id-" + strings.ReplaceAll(name, "/", "-")
}

// bumpGenerations sets each resource's generation the way YCP does: 1 when it appears, +1 when
// its spec changes.
func bumpGenerations(before, after []ampResource) {
	for i := range after {
		prev, ok := findAmpDesired(before, after[i].Kind, after[i].Name)
		switch {
		case !ok:
			after[i].Generation = 1
		case !bytes.Equal(prev.Spec, after[i].Spec):
			after[i].Generation = prev.Generation + 1
		default:
			after[i].Generation = prev.Generation
		}
	}
}

func fakeObserved(desired []ampResource) []ampObserved {
	var observed []ampObserved
	for _, r := range desired {
		attrs := map[string]string{"id": fakeID(r.Name), "applied_generation": strconv.FormatInt(r.Generation, 10)}
		if r.Kind == ampKindEndpoint {
			leaf := r.Name[strings.LastIndex(r.Name, "/")+1:]
			attrs["address"] = leaf + ".amp.example.com:5432"
		}
		observed = append(observed, ampObserved{Kind: r.Kind, ID: r.Kind + "/" + r.Name, State: ampStateReady, Attrs: attrs})
		main := r.Name + "/" + ampDefaultBranch
		if _, declared := findAmpDesired(desired, ampKindBranch, main); r.Kind == ampKindCluster && !declared {
			observed = append(observed, ampObserved{Kind: ampKindBranch, ID: ampKindBranch + "/" + main, State: ampStateReady, Attrs: map[string]string{"id": fakeID(main)}})
		}
	}
	return observed
}

func copyResources(in []ampResource) []ampResource {
	out := make([]ampResource, len(in))
	copy(out, in)
	return out
}

func (f *fakeYcp) write(w http.ResponseWriter, status int, data interface{}) {
	w.WriteHeader(status)
	if data != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"data": data})
	}
}

func (f *fakeYcp) fail(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	if e, ok := err.(*ycpError); ok {
		status = e.StatusCode
	}
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{"error": map[string]interface{}{"status": status, "detail": err.Error()}})
}

func (f *fakeYcp) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	body, _ := io.ReadAll(r.Body)
	f.requests = append(f.requests, r.Method+" "+r.URL.Path)
	var parsed map[string]interface{}
	if len(body) > 0 {
		_ = json.Unmarshal(body, &parsed)
	}
	f.bodies = append(f.bodies, parsed)

	if r.URL.Path == "/api/public/v1/account" {
		f.write(w, http.StatusOK, map[string]interface{}{
			"spec": map[string]string{"name": "test", "account_type": "AMP_MANAGED"},
			"info": map[string]string{"id": testAccountID},
		})
		return
	}
	prefix := "/api/public/v1/accounts/" + testAccountID + "/ycp"
	route := strings.TrimPrefix(r.URL.Path, prefix)

	switch {
	case route == "/clusters":
		f.serveManaged(w, r, body)
	case strings.HasPrefix(route, "/environments/"):
		rest := strings.TrimPrefix(route, "/environments/")
		id, sub, _ := strings.Cut(rest, "/")
		id, _ = url.PathUnescape(id)
		env, ok := f.byoc[id]
		if !ok {
			f.fail(w, &ycpError{StatusCode: http.StatusNotFound, Message: "environment not found"})
			return
		}
		switch {
		case sub == "" && r.Method == http.MethodGet:
			f.write(w, http.StatusOK, map[string]interface{}{"id": id, "state": env.state, "region": env.region, "resources": fakeObserved(env.desired)})
		case sub == "desired" && r.Method == http.MethodGet:
			f.write(w, http.StatusOK, map[string]interface{}{"resources": env.desired})
		case sub == "desired" && r.Method == http.MethodPost:
			var req struct {
				Resources []ampResource `json:"resources"`
			}
			_ = json.Unmarshal(body, &req)
			bumpGenerations(env.desired, req.Resources)
			env.desired = req.Resources
			f.write(w, http.StatusAccepted, nil)
		default:
			f.fail(w, &ycpError{StatusCode: http.StatusNotFound, Message: "no route"})
		}
	default:
		f.fail(w, &ycpError{StatusCode: http.StatusNotFound, Message: "no route " + r.URL.Path})
	}
}

func (f *fakeYcp) serveManaged(w http.ResponseWriter, r *http.Request, body []byte) {
	if r.Method == http.MethodGet {
		regions := make([]string, 0, len(f.managed))
		for region := range f.managed {
			regions = append(regions, region)
		}
		sort.Strings(regions)
		envs := []ampEnvironmentResources{}
		for _, region := range regions {
			envs = append(envs, ampEnvironmentResources{EnvironmentID: "managed-" + region, Region: region,
				Desired: f.managed[region], Observed: fakeObserved(f.managed[region])})
		}
		f.write(w, http.StatusOK, map[string]interface{}{"environments": envs})
		return
	}
	intent := map[string]string{http.MethodPost: ampIntentCreate, http.MethodPut: ampIntentUpdate, http.MethodPatch: ampIntentDelete}[r.Method]
	var patch struct {
		Regions []struct {
			Region    string         `json:"region"`
			Resources []ampPatchItem `json:"resources"`
		} `json:"regions"`
	}
	if err := json.Unmarshal(body, &patch); err != nil || len(patch.Regions) == 0 {
		f.fail(w, &ycpError{StatusCode: http.StatusBadRequest, Message: "bad patch"})
		return
	}
	for _, region := range patch.Regions {
		before := copyResources(f.managed[region.Region])
		// The real route carries no storage pool row in the response; the pool is implied.
		after, err := applyAmpIntent(copyResources(before), intent, region.Resources)
		if err != nil {
			f.fail(w, err)
			return
		}
		bumpGenerations(before, after)
		f.managed[region.Region] = after
	}
	f.write(w, http.StatusOK, map[string]interface{}{"environments": []interface{}{}})
}

// --- A minimal Terraform core over the protocol server ---

type tfHarness struct {
	t    *testing.T
	srv  tfprotov6.ProviderServer
	fake *fakeYcp
}

func newTfHarness(t *testing.T) *tfHarness {
	t.Helper()
	t.Setenv("YBM_FF_AMP", "true")
	ampPollInterval = time.Millisecond
	t.Cleanup(func() { ampPollInterval = 10 * time.Second })

	fake := newFakeYcp()
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	u, _ := url.Parse(server.URL)

	srv := providerserver.NewProtocol6(New("test")())()
	ctx := context.Background()
	schemas, err := srv.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	providerType := schemas.Provider.ValueType()
	config := tfObject(t, providerType, map[string]interface{}{"auth_token": "token", "host": u.Host, "use_secure_host": false})
	resp, err := srv.ConfigureProvider(ctx, &tfprotov6.ConfigureProviderRequest{Config: dynamic(t, providerType, config)})
	if err != nil || hasErrors(resp.Diagnostics) {
		t.Fatalf("configure: %v %v", err, resp.Diagnostics)
	}
	return &tfHarness{t: t, srv: srv, fake: fake}
}

func dynamic(t *testing.T, typ tftypes.Type, v tftypes.Value) *tfprotov6.DynamicValue {
	dv, err := tfprotov6.NewDynamicValue(typ, v)
	if err != nil {
		t.Fatal(err)
	}
	return &dv
}

func hasErrors(diags []*tfprotov6.Diagnostic) bool {
	for _, d := range diags {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			return true
		}
	}
	return false
}

func diagText(diags []*tfprotov6.Diagnostic) string {
	var parts []string
	for _, d := range diags {
		parts = append(parts, d.Summary+": "+d.Detail)
	}
	return strings.Join(parts, "; ")
}

func tfValue(t *testing.T, typ tftypes.Type, v interface{}) tftypes.Value {
	if v == nil {
		return tftypes.NewValue(typ, nil)
	}
	switch {
	case typ.Is(tftypes.String), typ.Is(tftypes.Bool):
		return tftypes.NewValue(typ, v)
	case typ.Is(tftypes.Number):
		return tftypes.NewValue(typ, big.NewFloat(float64(v.(int))))
	case typ.Is(tftypes.Object{}):
		return tfObject(t, typ, v.(map[string]interface{}))
	case typ.Is(tftypes.List{}):
		elem := typ.(tftypes.List).ElementType
		var vals []tftypes.Value
		for _, s := range v.([]string) {
			vals = append(vals, tftypes.NewValue(elem, s))
		}
		return tftypes.NewValue(typ, vals)
	case typ.Is(tftypes.Map{}):
		elem := typ.(tftypes.Map).ElementType
		vals := map[string]tftypes.Value{}
		for k, s := range v.(map[string]string) {
			vals[k] = tftypes.NewValue(elem, s)
		}
		return tftypes.NewValue(typ, vals)
	}
	t.Fatalf("unsupported type %v", typ)
	return tftypes.Value{}
}

func tfObject(t *testing.T, typ tftypes.Type, attrs map[string]interface{}) tftypes.Value {
	obj := typ.(tftypes.Object)
	vals := map[string]tftypes.Value{}
	for name, at := range obj.AttributeTypes {
		vals[name] = tfValue(t, at, attrs[name])
	}
	for name := range attrs {
		if _, ok := obj.AttributeTypes[name]; !ok {
			t.Fatalf("unknown attribute %s", name)
		}
	}
	return tftypes.NewValue(typ, vals)
}

func (h *tfHarness) schema(name string) *tfprotov6.Schema {
	schemas, err := h.srv.GetProviderSchema(context.Background(), &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		h.t.Fatal(err)
	}
	s, ok := schemas.ResourceSchemas[name]
	if !ok {
		h.t.Fatalf("resource %s is not registered", name)
	}
	return s
}

func (h *tfHarness) resourceType(name string) tftypes.Type {
	return h.schema(name).ValueType()
}

type tfPlanResult struct {
	planned         tftypes.Value
	requiresReplace bool
	diags           []*tfprotov6.Diagnostic
}

// plan mirrors Terraform core: the proposed state is the config with each null computed attribute
// taken from the prior state.
func (h *tfHarness) plan(name string, prior tftypes.Value, config map[string]interface{}) tfPlanResult {
	ctx := context.Background()
	typ := h.resourceType(name)
	cfg := tfObject(h.t, typ, config)
	proposed := cfg
	if !prior.IsNull() {
		var priorAttrs, cfgAttrs map[string]tftypes.Value
		_ = prior.As(&priorAttrs)
		_ = cfg.As(&cfgAttrs)
		computed := map[string]bool{}
		for _, a := range h.schema(name).Block.Attributes {
			computed[a.Name] = a.Computed
		}
		merged := map[string]tftypes.Value{}
		for k, v := range cfgAttrs {
			if v.IsNull() && computed[k] {
				merged[k] = priorAttrs[k]
				continue
			}
			merged[k] = v
		}
		proposed = tftypes.NewValue(typ, merged)
	}
	resp, err := h.srv.PlanResourceChange(ctx, &tfprotov6.PlanResourceChangeRequest{
		TypeName: name, PriorState: dynamic(h.t, typ, prior), ProposedNewState: dynamic(h.t, typ, proposed), Config: dynamic(h.t, typ, cfg),
	})
	if err != nil {
		h.t.Fatal(err)
	}
	res := tfPlanResult{diags: resp.Diagnostics, requiresReplace: len(resp.RequiresReplace) > 0}
	if resp.PlannedState != nil {
		res.planned, _ = resp.PlannedState.Unmarshal(typ)
	}
	return res
}

// apply plans and applies a create or update and returns the new state.
func (h *tfHarness) apply(name string, prior tftypes.Value, config map[string]interface{}) (tftypes.Value, []*tfprotov6.Diagnostic) {
	h.t.Helper()
	typ := h.resourceType(name)
	if prior.Type() == nil {
		prior = tftypes.NewValue(typ, nil)
	}
	p := h.plan(name, prior, config)
	if hasErrors(p.diags) {
		return prior, p.diags
	}
	resp, err := h.srv.ApplyResourceChange(context.Background(), &tfprotov6.ApplyResourceChangeRequest{
		TypeName: name, PriorState: dynamic(h.t, typ, prior), PlannedState: dynamic(h.t, typ, p.planned),
		Config: dynamic(h.t, typ, tfObject(h.t, typ, config)),
	})
	if err != nil {
		h.t.Fatal(err)
	}
	if hasErrors(resp.Diagnostics) {
		return prior, resp.Diagnostics
	}
	state, _ := resp.NewState.Unmarshal(typ)
	h.requireConsistent(name, p.planned, state)
	return state, nil
}

// requireConsistent fails the test where Terraform would fail the apply with "Provider produced
// inconsistent result after apply": a value known in the plan that the new state does not match.
func (h *tfHarness) requireConsistent(name string, planned, state tftypes.Value) {
	h.t.Helper()
	var plannedAttrs, stateAttrs map[string]tftypes.Value
	_ = planned.As(&plannedAttrs)
	_ = state.As(&stateAttrs)
	for k, v := range plannedAttrs {
		if v.IsFullyKnown() && !v.Equal(stateAttrs[k]) {
			h.t.Errorf("%s: inconsistent result after apply: %s was planned as %v but is %v", name, k, v, stateAttrs[k])
		}
	}
}

func (h *tfHarness) destroy(name string, prior tftypes.Value) []*tfprotov6.Diagnostic {
	typ := h.resourceType(name)
	null := tftypes.NewValue(typ, nil)
	resp, err := h.srv.ApplyResourceChange(context.Background(), &tfprotov6.ApplyResourceChangeRequest{
		TypeName: name, PriorState: dynamic(h.t, typ, prior), PlannedState: dynamic(h.t, typ, null), Config: dynamic(h.t, typ, null),
	})
	if err != nil {
		h.t.Fatal(err)
	}
	return resp.Diagnostics
}

func (h *tfHarness) read(name string, prior tftypes.Value) tftypes.Value {
	typ := h.resourceType(name)
	resp, err := h.srv.ReadResource(context.Background(), &tfprotov6.ReadResourceRequest{TypeName: name, CurrentState: dynamic(h.t, typ, prior)})
	if err != nil || hasErrors(resp.Diagnostics) {
		h.t.Fatalf("read %s: %v %s", name, err, diagText(resp.Diagnostics))
	}
	state, _ := resp.NewState.Unmarshal(typ)
	return state
}

func attrString(t *testing.T, state tftypes.Value, name string) string {
	var attrs map[string]tftypes.Value
	if err := state.As(&attrs); err != nil {
		t.Fatal(err)
	}
	var s string
	if attrs[name].IsNull() {
		return ""
	}
	if err := attrs[name].As(&s); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return s
}

func attrStringMap(t *testing.T, state tftypes.Value, name string) map[string]string {
	var attrs map[string]tftypes.Value
	_ = state.As(&attrs)
	if attrs[name].IsNull() {
		return nil
	}
	var elems map[string]tftypes.Value
	_ = attrs[name].As(&elems)
	out := map[string]string{}
	for k, v := range elems {
		var s string
		_ = v.As(&s)
		out[k] = s
	}
	return out
}

func attrStrings(t *testing.T, state tftypes.Value, name string) []string {
	var attrs map[string]tftypes.Value
	_ = state.As(&attrs)
	if attrs[name].IsNull() {
		return nil
	}
	var elems []tftypes.Value
	_ = attrs[name].As(&elems)
	var out []string
	for _, v := range elems {
		var s string
		_ = v.As(&s)
		out = append(out, s)
	}
	return out
}
