COREDNS_VERSION := v1.12.0
BUILD_DIR := $(CURDIR)/.build
COREDNS_DIR := $(BUILD_DIR)/coredns
PLUGIN_VER := "0.0.0"

.PHONY: all clean setup build

all: build

clean:
	rm -rf $(BUILD_DIR) coredns

setup: $(COREDNS_DIR)

$(COREDNS_DIR):
	@echo "Cloning CoreDNS $(COREDNS_VERSION)..."
	@mkdir -p $(BUILD_DIR)
	git clone --depth 1 --branch $(COREDNS_VERSION) https://github.com/coredns/coredns.git $(COREDNS_DIR)
	cp $(CURDIR)/plugin.cfg $(COREDNS_DIR)
	@echo "Configuring go.mod..."
	cd $(COREDNS_DIR) && go mod edit -require ztdns@v0.0.0
	cd $(COREDNS_DIR) && go mod edit -replace ztdns@v0.0.0=$(CURDIR)

build: setup
	@echo "Running go generate..."
	cd $(COREDNS_DIR) && go generate && go mod tidy
	@echo "Building CoreDNS..."
	cd $(COREDNS_DIR) && go build -o $(CURDIR)/coredns .
	@echo "Done!"
	./coredns -version

run: build
	./coredns -conf Corefile.example -dns.port=53
