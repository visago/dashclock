.PHONY: all

all:    lint build

lint:
	gofmt -w *.go

build:
	go build

run: lint
	go run main.go
