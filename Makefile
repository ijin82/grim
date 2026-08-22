.PHONY: all build install test clean

BINARY_NAME=grim

all: build

build:
	go build -o $(BINARY_NAME) ./cmd/grim

install: build
	@if [ -w /usr/local/bin ]; then \
		install -m 755 $(BINARY_NAME) /usr/local/bin/$(BINARY_NAME); \
		echo "🎉 Installed $(BINARY_NAME) to /usr/local/bin/$(BINARY_NAME)"; \
	else \
		mkdir -p $(HOME)/.local/bin; \
		install -m 755 $(BINARY_NAME) $(HOME)/.local/bin/$(BINARY_NAME); \
		echo "🎉 Installed $(BINARY_NAME) to $(HOME)/.local/bin/$(BINARY_NAME)"; \
	fi

test:
	go test -v ./...

clean:
	rm -f $(BINARY_NAME) cryptovault
