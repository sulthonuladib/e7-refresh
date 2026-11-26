.PHONY: build build-all dev install release clean

build:
	./bin/build

build-all:
	./bin/build-all

dev:
	go tool mygo dev

install:
	./bin/install

release:
	@test -n "$(TAG)" || (echo "usage: make release TAG=v0.1.0" && exit 2)
	./bin/release $(TAG)

clean:
	rm -rf build build-test
