HOSTNAME=registry.terraform.io
NAMESPACE=yugabyte
NAME=ybm
VERSION=0.1.0-dev

OS := $(if $(GOOS),$(GOOS),$(shell go env GOOS))
ARCH := $(if $(GOARCH),$(GOARCH),$(shell go env GOARCH))
GO_BIN := $(if $(shell go env GOBIN),$(shell go env GOBIN),$(shell go env GOPATH)/bin)
MOCKGEN := $(GO_BIN)/mockgen

BINARY=terraform-provider-${NAME}
export GOPRIVATE := github.com/yugabyte

default: install

vet:
	go vet ./...

build:
	go build -ldflags="-X 'main.version=v${VERSION}'" -o ${BINARY}

release:
	GOOS=darwin GOARCH=amd64 go build -ldflags="-X 'main.version=v${VERSION}'"  -o ./bin/${BINARY}_${VERSION}_darwin_amd64
	GOOS=freebsd GOARCH=386 go build -ldflags="-X 'main.version=v${VERSION}'" -o ./bin/${BINARY}_${VERSION}_freebsd_386
	GOOS=freebsd GOARCH=amd64 go build -ldflags="-X 'main.version=v${VERSION}'" -o ./bin/${BINARY}_${VERSION}_freebsd_amd64
	GOOS=freebsd GOARCH=arm go build -ldflags="-X 'main.version=v${VERSION}'" -o ./bin/${BINARY}_${VERSION}_freebsd_arm
	GOOS=linux GOARCH=386 go build -ldflags="-X 'main.version=v${VERSION}'" -o ./bin/${BINARY}_${VERSION}_linux_386
	GOOS=linux GOARCH=amd64 go build -ldflags="-X 'main.version=v${VERSION}'" -o ./bin/${BINARY}_${VERSION}_linux_amd64
	GOOS=linux GOARCH=arm go build -ldflags="-X 'main.version=v${VERSION}'" -o ./bin/${BINARY}_${VERSION}_linux_arm
	GOOS=openbsd GOARCH=386 go build -ldflags="-X 'main.version=v${VERSION}'" -o ./bin/${BINARY}_${VERSION}_openbsd_386
	GOOS=openbsd GOARCH=amd64 go build -ldflags="-X 'main.version=v${VERSION}'" -o ./bin/${BINARY}_${VERSION}_openbsd_amd64
	GOOS=solaris GOARCH=amd64 go build -ldflags="-X 'main.version=v${VERSION}'" -o ./bin/${BINARY}_${VERSION}_solaris_amd64
	GOOS=windows GOARCH=386 go build -ldflags="-X 'main.version=v${VERSION}'" -o ./bin/${BINARY}_${VERSION}_windows_386
	GOOS=windows GOARCH=amd64 go build -ldflags="-X 'main.version=v${VERSION}'" -o ./bin/${BINARY}_${VERSION}_windows_amd64

install: build
	mkdir -p ~/.terraform.d/plugins/${HOSTNAME}/${NAMESPACE}/${NAME}/${VERSION}/$(OS)_$(ARCH)/
	mv ${BINARY} ~/.terraform.d/plugins/${HOSTNAME}/${NAMESPACE}/${NAME}/${VERSION}/$(OS)_$(ARCH)/

test:
	go test -v -cover ./... -timeout 120m

testacc: 
	TF_ACC=1 go test -v -cover ./... -timeout 120m   

doc:
	./scripts/install_tfplugindocs.sh $(OS)_$(ARCH)
	tfplugindocs generate --rendered-provider-name 'YugabyteDB Aeon' --provider-name ybm

update-client:
	go get github.com/yugabyte/yugabytedb-managed-go-client-internal
	go mod tidy

update-mock-apis:
	go install github.com/golang/mock/mockgen@v1.6.0
	$(MOCKGEN) -destination=mock_yugabytedb_managed_go_client_internal/mock_api_account.go -package=mock_yugabytedb_managed_go_client_internal github.com/yugabyte/yugabytedb-managed-go-client-internal AccountApi
	$(MOCKGEN) -destination=mock_yugabytedb_managed_go_client_internal/mock_api_network.go -package=mock_yugabytedb_managed_go_client_internal github.com/yugabyte/yugabytedb-managed-go-client-internal NetworkApi
	$(MOCKGEN) -destination=mock_yugabytedb_managed_go_client_internal/mock_api_project.go -package=mock_yugabytedb_managed_go_client_internal github.com/yugabyte/yugabytedb-managed-go-client-internal ProjectApi

clean:
	rm -rf terraform-provider-ybm

fmt:
	go fmt ./...
	terraform fmt --recursive
	
fmt-check:
	@echo "Verifying formatting, failures can be fixed with 'make fmt'"
	@!(gofmt -l -s -d . | grep '[a-z]')
	terraform fmt -check --recursive
