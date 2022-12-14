// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/yugabyte/yugabytedb-managed-go-client-internal (interfaces: NetworkApi)

// Package mock_yugabytedb_managed_go_client_internal is a generated GoMock package.
package mock_yugabytedb_managed_go_client_internal

import (
	context "context"
	http "net/http"
	reflect "reflect"

	gomock "github.com/golang/mock/gomock"
	openapi "github.com/yugabyte/yugabytedb-managed-go-client-internal"
)

// MockNetworkApi is a mock of NetworkApi interface.
type MockNetworkApi struct {
	ctrl     *gomock.Controller
	recorder *MockNetworkApiMockRecorder
}

// MockNetworkApiMockRecorder is the mock recorder for MockNetworkApi.
type MockNetworkApiMockRecorder struct {
	mock *MockNetworkApi
}

// NewMockNetworkApi creates a new mock instance.
func NewMockNetworkApi(ctrl *gomock.Controller) *MockNetworkApi {
	mock := &MockNetworkApi{ctrl: ctrl}
	mock.recorder = &MockNetworkApiMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockNetworkApi) EXPECT() *MockNetworkApiMockRecorder {
	return m.recorder
}

// CreateNetworkAllowList mocks base method.
func (m *MockNetworkApi) CreateNetworkAllowList(arg0 context.Context, arg1, arg2 string) openapi.ApiCreateNetworkAllowListRequest {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "CreateNetworkAllowList", arg0, arg1, arg2)
	ret0, _ := ret[0].(openapi.ApiCreateNetworkAllowListRequest)
	return ret0
}

// CreateNetworkAllowList indicates an expected call of CreateNetworkAllowList.
func (mr *MockNetworkApiMockRecorder) CreateNetworkAllowList(arg0, arg1, arg2 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CreateNetworkAllowList", reflect.TypeOf((*MockNetworkApi)(nil).CreateNetworkAllowList), arg0, arg1, arg2)
}

// CreateNetworkAllowListExecute mocks base method.
func (m *MockNetworkApi) CreateNetworkAllowListExecute(arg0 openapi.ApiCreateNetworkAllowListRequest) (openapi.NetworkAllowListResponse, *http.Response, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "CreateNetworkAllowListExecute", arg0)
	ret0, _ := ret[0].(openapi.NetworkAllowListResponse)
	ret1, _ := ret[1].(*http.Response)
	ret2, _ := ret[2].(error)
	return ret0, ret1, ret2
}

// CreateNetworkAllowListExecute indicates an expected call of CreateNetworkAllowListExecute.
func (mr *MockNetworkApiMockRecorder) CreateNetworkAllowListExecute(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CreateNetworkAllowListExecute", reflect.TypeOf((*MockNetworkApi)(nil).CreateNetworkAllowListExecute), arg0)
}

// CreateVpc mocks base method.
func (m *MockNetworkApi) CreateVpc(arg0 context.Context, arg1, arg2 string) openapi.ApiCreateVpcRequest {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "CreateVpc", arg0, arg1, arg2)
	ret0, _ := ret[0].(openapi.ApiCreateVpcRequest)
	return ret0
}

// CreateVpc indicates an expected call of CreateVpc.
func (mr *MockNetworkApiMockRecorder) CreateVpc(arg0, arg1, arg2 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CreateVpc", reflect.TypeOf((*MockNetworkApi)(nil).CreateVpc), arg0, arg1, arg2)
}

// CreateVpcExecute mocks base method.
func (m *MockNetworkApi) CreateVpcExecute(arg0 openapi.ApiCreateVpcRequest) (openapi.SingleTenantVpcResponse, *http.Response, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "CreateVpcExecute", arg0)
	ret0, _ := ret[0].(openapi.SingleTenantVpcResponse)
	ret1, _ := ret[1].(*http.Response)
	ret2, _ := ret[2].(error)
	return ret0, ret1, ret2
}

// CreateVpcExecute indicates an expected call of CreateVpcExecute.
func (mr *MockNetworkApiMockRecorder) CreateVpcExecute(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CreateVpcExecute", reflect.TypeOf((*MockNetworkApi)(nil).CreateVpcExecute), arg0)
}

// CreateVpcPeering mocks base method.
func (m *MockNetworkApi) CreateVpcPeering(arg0 context.Context, arg1, arg2 string) openapi.ApiCreateVpcPeeringRequest {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "CreateVpcPeering", arg0, arg1, arg2)
	ret0, _ := ret[0].(openapi.ApiCreateVpcPeeringRequest)
	return ret0
}

// CreateVpcPeering indicates an expected call of CreateVpcPeering.
func (mr *MockNetworkApiMockRecorder) CreateVpcPeering(arg0, arg1, arg2 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CreateVpcPeering", reflect.TypeOf((*MockNetworkApi)(nil).CreateVpcPeering), arg0, arg1, arg2)
}

// CreateVpcPeeringExecute mocks base method.
func (m *MockNetworkApi) CreateVpcPeeringExecute(arg0 openapi.ApiCreateVpcPeeringRequest) (openapi.VpcPeeringResponse, *http.Response, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "CreateVpcPeeringExecute", arg0)
	ret0, _ := ret[0].(openapi.VpcPeeringResponse)
	ret1, _ := ret[1].(*http.Response)
	ret2, _ := ret[2].(error)
	return ret0, ret1, ret2
}

// CreateVpcPeeringExecute indicates an expected call of CreateVpcPeeringExecute.
func (mr *MockNetworkApiMockRecorder) CreateVpcPeeringExecute(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CreateVpcPeeringExecute", reflect.TypeOf((*MockNetworkApi)(nil).CreateVpcPeeringExecute), arg0)
}

// DeleteNetworkAllowList mocks base method.
func (m *MockNetworkApi) DeleteNetworkAllowList(arg0 context.Context, arg1, arg2, arg3 string) openapi.ApiDeleteNetworkAllowListRequest {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "DeleteNetworkAllowList", arg0, arg1, arg2, arg3)
	ret0, _ := ret[0].(openapi.ApiDeleteNetworkAllowListRequest)
	return ret0
}

// DeleteNetworkAllowList indicates an expected call of DeleteNetworkAllowList.
func (mr *MockNetworkApiMockRecorder) DeleteNetworkAllowList(arg0, arg1, arg2, arg3 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "DeleteNetworkAllowList", reflect.TypeOf((*MockNetworkApi)(nil).DeleteNetworkAllowList), arg0, arg1, arg2, arg3)
}

// DeleteNetworkAllowListExecute mocks base method.
func (m *MockNetworkApi) DeleteNetworkAllowListExecute(arg0 openapi.ApiDeleteNetworkAllowListRequest) (*http.Response, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "DeleteNetworkAllowListExecute", arg0)
	ret0, _ := ret[0].(*http.Response)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// DeleteNetworkAllowListExecute indicates an expected call of DeleteNetworkAllowListExecute.
func (mr *MockNetworkApiMockRecorder) DeleteNetworkAllowListExecute(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "DeleteNetworkAllowListExecute", reflect.TypeOf((*MockNetworkApi)(nil).DeleteNetworkAllowListExecute), arg0)
}

// DeleteVpc mocks base method.
func (m *MockNetworkApi) DeleteVpc(arg0 context.Context, arg1, arg2, arg3 string) openapi.ApiDeleteVpcRequest {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "DeleteVpc", arg0, arg1, arg2, arg3)
	ret0, _ := ret[0].(openapi.ApiDeleteVpcRequest)
	return ret0
}

// DeleteVpc indicates an expected call of DeleteVpc.
func (mr *MockNetworkApiMockRecorder) DeleteVpc(arg0, arg1, arg2, arg3 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "DeleteVpc", reflect.TypeOf((*MockNetworkApi)(nil).DeleteVpc), arg0, arg1, arg2, arg3)
}

// DeleteVpcExecute mocks base method.
func (m *MockNetworkApi) DeleteVpcExecute(arg0 openapi.ApiDeleteVpcRequest) (*http.Response, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "DeleteVpcExecute", arg0)
	ret0, _ := ret[0].(*http.Response)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// DeleteVpcExecute indicates an expected call of DeleteVpcExecute.
func (mr *MockNetworkApiMockRecorder) DeleteVpcExecute(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "DeleteVpcExecute", reflect.TypeOf((*MockNetworkApi)(nil).DeleteVpcExecute), arg0)
}

// DeleteVpcPeering mocks base method.
func (m *MockNetworkApi) DeleteVpcPeering(arg0 context.Context, arg1, arg2, arg3 string) openapi.ApiDeleteVpcPeeringRequest {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "DeleteVpcPeering", arg0, arg1, arg2, arg3)
	ret0, _ := ret[0].(openapi.ApiDeleteVpcPeeringRequest)
	return ret0
}

// DeleteVpcPeering indicates an expected call of DeleteVpcPeering.
func (mr *MockNetworkApiMockRecorder) DeleteVpcPeering(arg0, arg1, arg2, arg3 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "DeleteVpcPeering", reflect.TypeOf((*MockNetworkApi)(nil).DeleteVpcPeering), arg0, arg1, arg2, arg3)
}

// DeleteVpcPeeringExecute mocks base method.
func (m *MockNetworkApi) DeleteVpcPeeringExecute(arg0 openapi.ApiDeleteVpcPeeringRequest) (*http.Response, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "DeleteVpcPeeringExecute", arg0)
	ret0, _ := ret[0].(*http.Response)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// DeleteVpcPeeringExecute indicates an expected call of DeleteVpcPeeringExecute.
func (mr *MockNetworkApiMockRecorder) DeleteVpcPeeringExecute(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "DeleteVpcPeeringExecute", reflect.TypeOf((*MockNetworkApi)(nil).DeleteVpcPeeringExecute), arg0)
}

// GetNetworkAllowList mocks base method.
func (m *MockNetworkApi) GetNetworkAllowList(arg0 context.Context, arg1, arg2, arg3 string) openapi.ApiGetNetworkAllowListRequest {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetNetworkAllowList", arg0, arg1, arg2, arg3)
	ret0, _ := ret[0].(openapi.ApiGetNetworkAllowListRequest)
	return ret0
}

// GetNetworkAllowList indicates an expected call of GetNetworkAllowList.
func (mr *MockNetworkApiMockRecorder) GetNetworkAllowList(arg0, arg1, arg2, arg3 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetNetworkAllowList", reflect.TypeOf((*MockNetworkApi)(nil).GetNetworkAllowList), arg0, arg1, arg2, arg3)
}

// GetNetworkAllowListExecute mocks base method.
func (m *MockNetworkApi) GetNetworkAllowListExecute(arg0 openapi.ApiGetNetworkAllowListRequest) (openapi.NetworkAllowListResponse, *http.Response, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetNetworkAllowListExecute", arg0)
	ret0, _ := ret[0].(openapi.NetworkAllowListResponse)
	ret1, _ := ret[1].(*http.Response)
	ret2, _ := ret[2].(error)
	return ret0, ret1, ret2
}

// GetNetworkAllowListExecute indicates an expected call of GetNetworkAllowListExecute.
func (mr *MockNetworkApiMockRecorder) GetNetworkAllowListExecute(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetNetworkAllowListExecute", reflect.TypeOf((*MockNetworkApi)(nil).GetNetworkAllowListExecute), arg0)
}

// GetSingleTenantVpc mocks base method.
func (m *MockNetworkApi) GetSingleTenantVpc(arg0 context.Context, arg1, arg2, arg3 string) openapi.ApiGetSingleTenantVpcRequest {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetSingleTenantVpc", arg0, arg1, arg2, arg3)
	ret0, _ := ret[0].(openapi.ApiGetSingleTenantVpcRequest)
	return ret0
}

// GetSingleTenantVpc indicates an expected call of GetSingleTenantVpc.
func (mr *MockNetworkApiMockRecorder) GetSingleTenantVpc(arg0, arg1, arg2, arg3 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetSingleTenantVpc", reflect.TypeOf((*MockNetworkApi)(nil).GetSingleTenantVpc), arg0, arg1, arg2, arg3)
}

// GetSingleTenantVpcExecute mocks base method.
func (m *MockNetworkApi) GetSingleTenantVpcExecute(arg0 openapi.ApiGetSingleTenantVpcRequest) (openapi.SingleTenantVpcResponse, *http.Response, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetSingleTenantVpcExecute", arg0)
	ret0, _ := ret[0].(openapi.SingleTenantVpcResponse)
	ret1, _ := ret[1].(*http.Response)
	ret2, _ := ret[2].(error)
	return ret0, ret1, ret2
}

// GetSingleTenantVpcExecute indicates an expected call of GetSingleTenantVpcExecute.
func (mr *MockNetworkApiMockRecorder) GetSingleTenantVpcExecute(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetSingleTenantVpcExecute", reflect.TypeOf((*MockNetworkApi)(nil).GetSingleTenantVpcExecute), arg0)
}

// GetVpcPeering mocks base method.
func (m *MockNetworkApi) GetVpcPeering(arg0 context.Context, arg1, arg2, arg3 string) openapi.ApiGetVpcPeeringRequest {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetVpcPeering", arg0, arg1, arg2, arg3)
	ret0, _ := ret[0].(openapi.ApiGetVpcPeeringRequest)
	return ret0
}

// GetVpcPeering indicates an expected call of GetVpcPeering.
func (mr *MockNetworkApiMockRecorder) GetVpcPeering(arg0, arg1, arg2, arg3 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetVpcPeering", reflect.TypeOf((*MockNetworkApi)(nil).GetVpcPeering), arg0, arg1, arg2, arg3)
}

// GetVpcPeeringExecute mocks base method.
func (m *MockNetworkApi) GetVpcPeeringExecute(arg0 openapi.ApiGetVpcPeeringRequest) (openapi.VpcPeeringResponse, *http.Response, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetVpcPeeringExecute", arg0)
	ret0, _ := ret[0].(openapi.VpcPeeringResponse)
	ret1, _ := ret[1].(*http.Response)
	ret2, _ := ret[2].(error)
	return ret0, ret1, ret2
}

// GetVpcPeeringExecute indicates an expected call of GetVpcPeeringExecute.
func (mr *MockNetworkApiMockRecorder) GetVpcPeeringExecute(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetVpcPeeringExecute", reflect.TypeOf((*MockNetworkApi)(nil).GetVpcPeeringExecute), arg0)
}

// ListNetworkAllowLists mocks base method.
func (m *MockNetworkApi) ListNetworkAllowLists(arg0 context.Context, arg1, arg2 string) openapi.ApiListNetworkAllowListsRequest {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ListNetworkAllowLists", arg0, arg1, arg2)
	ret0, _ := ret[0].(openapi.ApiListNetworkAllowListsRequest)
	return ret0
}

// ListNetworkAllowLists indicates an expected call of ListNetworkAllowLists.
func (mr *MockNetworkApiMockRecorder) ListNetworkAllowLists(arg0, arg1, arg2 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ListNetworkAllowLists", reflect.TypeOf((*MockNetworkApi)(nil).ListNetworkAllowLists), arg0, arg1, arg2)
}

// ListNetworkAllowListsExecute mocks base method.
func (m *MockNetworkApi) ListNetworkAllowListsExecute(arg0 openapi.ApiListNetworkAllowListsRequest) (openapi.NetworkAllowListListResponse, *http.Response, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ListNetworkAllowListsExecute", arg0)
	ret0, _ := ret[0].(openapi.NetworkAllowListListResponse)
	ret1, _ := ret[1].(*http.Response)
	ret2, _ := ret[2].(error)
	return ret0, ret1, ret2
}

// ListNetworkAllowListsExecute indicates an expected call of ListNetworkAllowListsExecute.
func (mr *MockNetworkApiMockRecorder) ListNetworkAllowListsExecute(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ListNetworkAllowListsExecute", reflect.TypeOf((*MockNetworkApi)(nil).ListNetworkAllowListsExecute), arg0)
}

// ListSingleTenantVpcs mocks base method.
func (m *MockNetworkApi) ListSingleTenantVpcs(arg0 context.Context, arg1, arg2 string) openapi.ApiListSingleTenantVpcsRequest {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ListSingleTenantVpcs", arg0, arg1, arg2)
	ret0, _ := ret[0].(openapi.ApiListSingleTenantVpcsRequest)
	return ret0
}

// ListSingleTenantVpcs indicates an expected call of ListSingleTenantVpcs.
func (mr *MockNetworkApiMockRecorder) ListSingleTenantVpcs(arg0, arg1, arg2 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ListSingleTenantVpcs", reflect.TypeOf((*MockNetworkApi)(nil).ListSingleTenantVpcs), arg0, arg1, arg2)
}

// ListSingleTenantVpcsExecute mocks base method.
func (m *MockNetworkApi) ListSingleTenantVpcsExecute(arg0 openapi.ApiListSingleTenantVpcsRequest) (openapi.SingleTenantVpcListResponse, *http.Response, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ListSingleTenantVpcsExecute", arg0)
	ret0, _ := ret[0].(openapi.SingleTenantVpcListResponse)
	ret1, _ := ret[1].(*http.Response)
	ret2, _ := ret[2].(error)
	return ret0, ret1, ret2
}

// ListSingleTenantVpcsExecute indicates an expected call of ListSingleTenantVpcsExecute.
func (mr *MockNetworkApiMockRecorder) ListSingleTenantVpcsExecute(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ListSingleTenantVpcsExecute", reflect.TypeOf((*MockNetworkApi)(nil).ListSingleTenantVpcsExecute), arg0)
}

// ListVpcPeerings mocks base method.
func (m *MockNetworkApi) ListVpcPeerings(arg0 context.Context, arg1, arg2 string) openapi.ApiListVpcPeeringsRequest {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ListVpcPeerings", arg0, arg1, arg2)
	ret0, _ := ret[0].(openapi.ApiListVpcPeeringsRequest)
	return ret0
}

// ListVpcPeerings indicates an expected call of ListVpcPeerings.
func (mr *MockNetworkApiMockRecorder) ListVpcPeerings(arg0, arg1, arg2 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ListVpcPeerings", reflect.TypeOf((*MockNetworkApi)(nil).ListVpcPeerings), arg0, arg1, arg2)
}

// ListVpcPeeringsExecute mocks base method.
func (m *MockNetworkApi) ListVpcPeeringsExecute(arg0 openapi.ApiListVpcPeeringsRequest) (openapi.VpcPeeringListResponse, *http.Response, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ListVpcPeeringsExecute", arg0)
	ret0, _ := ret[0].(openapi.VpcPeeringListResponse)
	ret1, _ := ret[1].(*http.Response)
	ret2, _ := ret[2].(error)
	return ret0, ret1, ret2
}

// ListVpcPeeringsExecute indicates an expected call of ListVpcPeeringsExecute.
func (mr *MockNetworkApiMockRecorder) ListVpcPeeringsExecute(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ListVpcPeeringsExecute", reflect.TypeOf((*MockNetworkApi)(nil).ListVpcPeeringsExecute), arg0)
}
