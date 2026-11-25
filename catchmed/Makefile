.PHONY: test fuzz bench cover build vet

test:
	go test -race -count=1 ./...

vet:
	go vet ./...

fuzz:
	go test ./internal/download -run "^$$" -fuzz FuzzPlan -fuzztime 30s

bench:
	go test ./... -run "^$$" -bench . -benchmem

cover:
	go test -coverprofile=coverage.out ./... && go tool cover -html=coverage.out

build:
	go build -trimpath -o bin/catchmed ./cmd/catchmed
