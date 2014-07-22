.PHONY: default
default:

.PHONY: check
check:
	[ -z "$$(gofmt -l .)" ] || \
	  ( echo "These files are not properly gofmt'd:\n$$(gofmt -l .)" ; false )
	go vet ./...
	golint .
