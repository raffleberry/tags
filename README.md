# tags
Use taglib C bindings in Go

## Usage/Setup

#### Windows(Msys2)
```sh
pacman -S git mingw-w64-x86_64-pkg-config mingw-w64-x86_64-taglib
git clone https://github.com/raffleberry/tags.git
./bake build
```
#### Linux
```sh
# ubuntu
sudo apt install gcc g++ cmake make libutfcpp-dev zlib1g-dev
git clone --recurse-submodules https://github.com/raffleberry/tags.git

./bake taglib
./bake build
```
test the cli program `build/tags` to double check if it worked.

use `go get` to add this to your projects:-
```sh
go get github.com/raffleberry/tags
```



## Debugging
```sh
./bake clean
```
clean before rebuilds

## Docs
[https://pkg.go.dev/github.com/raffleberry/tags](https://pkg.go.dev/github.com/raffleberry/tags)
