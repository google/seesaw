all:
	go tool fix *.go
	go tool vet *.go
	gofmt -s -w *.go
	go build
	go install
test: all
	go test
