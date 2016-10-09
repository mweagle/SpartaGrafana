.DEFAULT_GOAL=build
.PHONY: build test get run

clean:
	go clean .

generate:
	go generate -x
	@echo "Generate complete: `date`"

vet: generate
	go tool vet -composites=false *.go

get: clean
	godep restore

build: get generate vet
	go build .

test:
	go test ./test/...

delete:
	go run main.go delete

explore:
	go run main.go --level debug explore

provision: generate vet
	go run main.go provision --level info --s3Bucket $(S3_BUCKET)

provisionShort: generate vet
	go run main.go provision -s weagle --noop -l debug

describe: generate vet
	go run main.go --level info describe --out ./graph.html
