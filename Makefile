all:
	go build ./...

install: all
	go install github.com/google/seesaw/binaries/seesaw_cli
	go install github.com/google/seesaw/binaries/seesaw_ecu
	go install github.com/google/seesaw/binaries/seesaw_engine
	go install github.com/google/seesaw/binaries/seesaw_ha
	go install github.com/google/seesaw/binaries/seesaw_healthcheck
	go install github.com/google/seesaw/binaries/seesaw_ncc
	go install github.com/google/seesaw/binaries/seesaw_watchdog

install-test-tools: all
	go install github.com/google/seesaw/test_tools/healthcheck_test_tool
	go install github.com/google/seesaw/test_tools/ipvs_test_tool
	go install github.com/google/seesaw/test_tools/ncc_test_tool
	go install github.com/google/seesaw/test_tools/quagga_test_tool

proto:
	protoc --go_out=. pb/config/config.proto

test: all
	go test ./...
