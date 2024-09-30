# tags
Use taglib C bindings in Go

## Usage
### Linux
#### Build Taglib
```sh
git clone --recurse-submodules https://github.com/raffleberry/tags.git

# ubuntu
sudo apt install gcc g++ cmake make libutfcpp-dev zlib1g-dev

# build & install(/usr/local/) taglib from source
./bake taglib
```
#### Install
```sh
go get github.com/raffleberry/tags
```

## Debugging
```sh
go clean -cache
```
clean before rebuilds

## Docs
[https://pkg.go.dev/github.com/raffleberry/tags](https://pkg.go.dev/github.com/raffleberry/tags)
