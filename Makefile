GOOS     := $(shell go env GOOS)
GOARCH   := $(shell go env GOARCH)

ARCHIVE := harpoon-latest.$(GOOS)-$(GOARCH).tar.gz
DISTDIR := dist/$(GOOS)-$(GOARCH)

.PHONY: default
default:

.PHONY: clean
clean:
	git clean -dfx

.PHONY: archive
archive:
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build -o $(DISTDIR)/harpoon-agent ./harpoon-agent
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build -o $(DISTDIR)/harpoon-container ./harpoon-container
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build -o $(DISTDIR)/harpoon-scheduler ./harpoon-scheduler
	tar -C $(DISTDIR) -czvf dist/$(ARCHIVE) .
